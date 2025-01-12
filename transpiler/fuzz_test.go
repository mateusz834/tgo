package transpiler

import (
	"cmp"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	goast "go/ast"
	"go/build/constraint"
	goformat "go/format"
	goimporter "go/importer"
	goparser "go/parser"
	goscanner "go/scanner"
	gotoken "go/token"
	gotypes "go/types"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/mateusz834/tgo/internal/tgoimporter"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/format"
	"github.com/tgo-lang/lang/importer"
	"github.com/tgo-lang/lang/parser"
	"github.com/tgo-lang/lang/token"
	"github.com/tgo-lang/lang/types"
)

func fuzzAddDir(f *testing.F, testdata string, transform func(string) string) {
	if transform == nil {
		transform = func(s string) string { return s }
	}

	files, err := os.ReadDir(testdata)
	if err != nil {
		f.Fatal(err)
	}
	for _, v := range files {
		if v.IsDir() {
			continue
		}

		testFile := filepath.Join(testdata, v.Name())
		content, err := os.ReadFile(testFile)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(testFile, transform(string(content)))
	}
}

func FuzzFormattedTgoProducesFormattedGoSource(f *testing.F) {
	fuzzAddDir(f, "../../tgoast/printer/testdata/tgo", nil)
	fuzzAddDir(f, "../../tgoast/parser/testdata/tgo", nil)
	fuzzAddDir(f, "../../tgoast/printer", nil)
	fuzzAddDir(f, "../../tgoast/printer/testdata", nil)
	fuzzAddDir(f, "../../tgoast/parser", nil)
	fuzzAddDir(f, "../../tgoast/parser/testdata", nil)
	fuzzAddDir(f, "../../tgoast/ast", nil)
	fuzzAddDir(f, "../analyzer/testdata", nil)
	fuzzAddDir(f, "../tgofuncs/testdata", nil)
	fuzzAddDir(f, "./testdata", func(s string) string {
		return strings.Split(s, "======\n")[0]
	})

	f.Fuzz(func(t *testing.T, name string, src string) {
		fmted := fuzzSource(t, name, src)
		if fmted != "" {
			t.Run("fmted", func(t *testing.T) {
				fuzzSource(t, name, fmted)
			})
		}
	})
}

// fuzzSource runs a single iteration of a fuzz test.
// Returns a formatted (tgo) source in case the input src was not formatted.
func fuzzSource(t *testing.T, name, src string) string {
	if !utf8.ValidString(name) {
		return ""
	}

	for _, v := range []string{"\r", "\f", "\n", "\v", "\000", "\ufeff"} {
		if strings.Contains(name, v) {
			return ""
		}
	}

	if strings.Contains(src, "\f") {
		return ""
	}

	if testing.Verbose() {
		t.Logf("file name: %q", name)
		t.Logf("source:\n%v", src)
		t.Logf("quoted input:\n%q", src)
	}

	fset := token.NewFileSet()

	// Add an unused file to FileSet, so that fset.Base()
	// is incrased before parsing the file. This way we also
	// make sure that we are converting token.Pos into source offset
	// correctly in the transpiler.
	fset.AddFile("t", -1, 99)

	f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return ""
	}

	if analyzer.Analyze(fset, f) != nil {
		return ""
	}

	//if len(f.Comments) != 0 {
	//	for _, n := range tgofuncs.Check(f).TgoFuncs {
	//		switch n := n.(type) {
	//		case *ast.FuncDecl:
	//			names := n.Type.Params.List[0].Names
	//			if names == nil || names[0].Name == "_" {
	//				t.Skip()
	//			}
	//		case *ast.FuncLit:
	//			names := n.Type.Params.List[0].Names
	//			if names == nil || names[0].Name == "_" {
	//				t.Skip()
	//			}
	//		}
	//	}
	//}

	prevLine := math.MinInt
	for _, v := range f.Comments {
		for _, v := range v.List {
			pos := fset.Position(v.Pos())
			if prevLine+1 == pos.Line {
				t.Skip()
			}
			prevLine = pos.Line
		}
	}

	// See https://go.dev/issue/69861
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BasicLit:
			if n.Kind == token.STRING && n.Value[0] == '`' {
				for _, v := range src[fset.File(f.FileStart).Offset(n.Pos())+1:] {
					if v == '`' {
						return true
					}
					if v == '\r' {
						t.Skip()
					}
				}
			}
		}
		return true
	})

	// See https://go.dev/issue/41197
	for _, v := range f.Comments {
		for _, v := range v.List {
			if v.Text[1] == '/' {
				for _, v := range src[fset.File(f.FileStart).Offset(v.Pos())+2:] {
					if v == '\r' {
						t.Skip()
					}
					if v == '\n' {
						break
					}
				}
			} else if v.Text[1] == '*' {
				var prev rune
				for _, v := range src[fset.File(f.FileStart).Offset(v.Pos())+2:] {
					if v == '\r' {
						t.Skip()
					}
					if prev == '*' && v == '/' {
						break
					}
					prev = v
				}
			}
		}
	}

	out := Transpile(f, fset, src)

	if testing.Verbose() {
		t.Logf("transpiled output:\n%v", out)
		t.Logf("quoted transpiled output:\n%q", out)
	}

	fsetgo := gotoken.NewFileSet()
	fgo, err := goparser.ParseFile(fsetgo, name, out, goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		if v, ok := err.(goscanner.ErrorList); ok {
			for _, v := range v {
				t.Logf("%v", v)
			}
		}
		t.Fatalf("goparser.ParseFile(Transpile(src)) = %v; want = <nil>", err)
	}

	expectedEmptyBlockStmtCount := 0
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			//hasOnlyEmptyStrs := true
			//for _, v := range n.List {
			//	isEmptyStr := false
			//	if v, ok := v.(*ast.ExprStmt); ok {
			//		if v, ok := v.X.(*ast.BasicLit); ok && v.Kind == token.STRING {
			//			str, err := strconv.Unquote(v.Value)
			//			if err != nil {
			//				panic(err) // unreachable, AST is valid
			//			}
			//			isEmptyStr = str == ""
			//		}
			//	}
			//	if !isEmptyStr {
			//		hasOnlyEmptyStrs = false
			//		break
			//	}
			//}
			//if hasOnlyEmptyStrs {
			//	expectedEmptyBlockStmtCount++
			//}
			if len(n.List) == 0 {
				expectedEmptyBlockStmtCount++
				_ = n
			}
		}
		return true
	})

	emptyBlockStmtCountGo := 0
	goast.Inspect(fgo, func(n goast.Node) bool {
		switch n := n.(type) {
		case *goast.BlockStmt:
			if len(n.List) == 0 {
				emptyBlockStmtCountGo++
			}
		}
		return true
	})

	// Transpiler should not produce empty block stmts for empty tags (<div>)
	// and for empty tag bodies (<div></div>).
	if emptyBlockStmtCountGo != expectedEmptyBlockStmtCount {
		var transpiled, input strings.Builder
		ast.Fprint(&input, fset, f, ast.NotNilFilter)
		goast.Fprint(&transpiled, fsetgo, fgo, goast.NotNilFilter)
		t.Logf("input AST:\n%v", input.String())
		t.Logf("transpiled AST:\n%v", transpiled.String())
		t.Errorf(
			"transpiled output contains an unexpected amount of *ast.BlockStmt: %v; want: %v",
			emptyBlockStmtCountGo, expectedEmptyBlockStmtCount,
		)
	}

	want := tgoExpectedNodeInfos(f, fset)
	missing := maps.Clone(want)
	goast.Inspect(fgo, func(n goast.Node) bool {
		if n == nil {
			return true
		}
		info := genNodeInfo[gotoken.Token](n, fsetgo.Position)
		if testing.Verbose() {
			t.Logf("go key: %v", info)
		}
		delete(missing, info)
		return true
	})

	if len(missing) != 0 {
		//var transpiled, input strings.Builder
		//ast.Fprint(&input, fset, f, ast.NotNilFilter)
		//goast.Fprint(&transpiled, fsetgo, fgo, goast.NotNilFilter)
		//t.Logf("input AST:\n%v", input.String())
		//t.Logf("transpiled AST:\n%v", transpiled.String())
		for _, v := range slices.SortedFunc(maps.Keys(missing), func(x, y nodeInfo) int {
			return cmp.Or(
				cmp.Compare(x.nodeStart.line, y.nodeStart.line),
				cmp.Compare(x.nodeStart.column, y.nodeStart.column),
			)
		}) {
			t.Logf("missing key: %+v", v)
		}
		t.Fatal("invalid line directives")
	}

	// Make sure that every comment we transpiled is the same and has the same position
	// information as in the original source.
	// We currently only check comments that are present
	// in the transpiled output, whether they match comments in the tgo input.
	// TODO: We should do this the other way round, extract coments from tgo, that we
	// expect to be in the transpiled output.
	validComments := make(map[nodeInfo]struct{})
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			info := genNodeInfo[token.Token](c, fset.Position)
			if testing.Verbose() {
				t.Logf("go comment key: %v", info)
			}
			validComments[info] = struct{}{}
		}
	}
	for _, cg := range fgo.Comments {
		for _, c := range cg.List {
			p := fsetgo.Position(c.Pos())
			if (p.Column == 1 && strings.HasPrefix(c.Text, "//line ")) || strings.HasPrefix(c.Text, "/*line ") {
				continue
			}
			if c.Text == "// Code generated by tgo - DO NOT EDIT." {
				continue
			}
			if c.Text == "// Assert that no other file in this package overrides the error builtin interface." {
				continue
			}
			ni := genNodeInfo[gotoken.Token](c, fsetgo.Position)
			if _, ok := validComments[ni]; !ok {
				t.Errorf("invalid comment found: %v", ni)
			}
			delete(validComments, ni)
		}
	}

	//	func A(tgo. //
	//			Ctx) error {
	//		<div></div>
	//	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncType:
			ast.Inspect(n.Params, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.SelectorExpr:
					from, to := n.X.End(), n.Sel.Pos()
					for _, cg := range f.Comments {
						for _, c := range cg.List {
							if c.Pos() > from && c.End() < to {
								t.Skip()
							}
						}
					}
				}
				return true
			})
		}
		return true
	})

	// The Go formatter moves comments around, bacause it treats every comment
	// at Column == 1 as doc comment, and it moves directives to the end of a comment.
	// Line directive should not be moved in any way (https://go.dev/cl/609077).
	// We are not able to keep that formatted.
	for i, v := range fgo.Comments {
		for _, c := range v.List {
			p := fsetgo.PositionFor(c.Pos(), false)
			if (p.Column == 1 && strings.HasPrefix(c.Text, "//line")) || strings.HasPrefix(c.Text, "/*line") {
				if len(v.List) != 1 {
					return ""
				}

				// Comments with line directives are not properly combined
				// into comment groups, because of line directives (https://go.dev/cl/609515)
				if i+1 != len(fgo.Comments) {
					end := fsetgo.PositionFor(v.End(), false)
					nextStart := fsetgo.PositionFor(fgo.Comments[i+1].Pos(), false)
					if end.Line+1 == nextStart.Line || end.Line == nextStart.Line {
						onlyWhite := true
						for _, v := range out[end.Offset:nextStart.Offset] {
							switch v {
							case ' ', '\t', '\n':
							default:
								onlyWhite = false
							}
						}
						if onlyWhite {
							return ""
						}
					}
				}
			}
		}
	}

	var tgoFmt strings.Builder
	func() {
		defer func() {
			if p := recover(); p != nil {
				b := make([]uintptr, 128)
				n := runtime.Callers(0, b)
				cf := runtime.CallersFrames(b[:n])
				for f, ok := cf.Next(); ok; f, ok = cf.Next() {
					v, _ := p.(string)

					// Upstream bugs:

					// https://go.dev/cl/610035
					if f.Func.Name() == "github.com/tgo-lang/lang/ast.sortSpecs" &&
						strings.Contains(v, "invalid line number") {
						return
					}

					// https://go.dev/cl/610115 https://go.dev/issue/69206
					if f.Func.Name() == "github.com/tgo-lang/lang/printer.combinesWithName" &&
						strings.Contains(v, "unexpected parenthesized expression") {
						return
					}
				}
				panic(p)
			}
		}()
		if err := format.Node(&tgoFmt, fset, f); err != nil {
			if strings.Contains(err.Error(), "format.Node internal error (") {
				// See https://go.dev/issue/69089
				for _, v := range f.Comments {
					for _, v := range v.List {
						if fset.PositionFor(v.Pos(), false).Column != 1 &&
							(constraint.IsGoBuild(v.Text) || constraint.IsPlusBuild(v.Text)) {
							return
						}
					}
				}

				// See https://go.dev/issue/69858
				var lastEndImportPos token.Pos
				for _, v := range f.Decls {
					if v, ok := v.(*ast.GenDecl); ok && v.Tok == token.IMPORT {
						lastEndImportPos = v.End()
					}
				}
				for _, v := range f.Comments {
					for _, v := range v.List {
						if v.Pos() > lastEndImportPos {
							break
						}
						if v.Text[1] == '*' && strings.ContainsRune(v.Text, '\f') {
							return
						}
					}
				}
				for _, v := range f.Imports {
					if strings.ContainsRune(v.Path.Value, '\f') {
						return
					}
				}

				// See https://go.dev/cl/626758
				hasEllipsis := false
				ast.Inspect(f, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.FuncType:
						if n.Results != nil {
							ast.Inspect(n.Results, func(n ast.Node) bool {
								switch n.(type) {
								case *ast.Ellipsis:
									hasEllipsis = true
								}
								return true
							})
						}
					}
					return true
				})
				if hasEllipsis {
					return
				}
			}

			//const testSrc = `package A
			//import()
			//func()A()(
			///**/A)`

			//package A
			//import()
			//func()A(A(
			///**/A))
			if len(f.Comments) != 0 {
				hasMultiLineReturn := false
				ast.Inspect(f, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.FuncType:
						if fset.Position(n.Params.Opening).Line != fset.Position(n.Params.Closing).Line {
							hasMultiLineReturn = true
							return false
						}
						if n.Results != nil {
							if fset.Position(n.Results.Opening).Line != fset.Position(n.Results.Closing).Line {
								hasMultiLineReturn = true
								return false
							}
						}
					}
					return true
				})
				if hasMultiLineReturn {
					return
				}
			}

			t.Fatalf("format.Node() = %v; want <nil>", err)
		}
	}()

	if tgoFmt.String() != src {
		return tgoFmt.String()
	}

	var outFmt strings.Builder
	if err := goformat.Node(&outFmt, fsetgo, fgo); err != nil {
		t.Fatalf("goformat.Node() = %v; want <nil>", err)
	}

	if testing.Verbose() {
		t.Logf("formatted transpiled output:\n%v", outFmt.String())
		t.Logf("quoted formatted transpiled output:\n%q", outFmt.String())
	}

	if outFmt.String() != out {
		diff, err := gitDiff(t.TempDir(), out, outFmt.String())
		if err != nil {
			t.Fatalf("difference found")
		}
		t.Fatalf(
			"difference found, apply following changes to make this test pass:\n%v",
			diff,
		)
	}

	//fuzzTypes(t, fset, f, fsetgo, fgo)

	return ""
}

func fuzzTypes(t *testing.T, fset *token.FileSet, f *ast.File, gofset *gotoken.FileSet, gof *goast.File) {
	type typeError struct {
		Line int
		Col  int
		Msg  string
		Soft bool
	}

	tgoErrs := []typeError{}
	cfg := types.Config{
		Importer: &tgoimporter.TgoDefaultImporter{I: importer.Default().(types.ImporterFrom)},
		Error: func(err error) {
			e := err.(types.Error)
			pos := fset.Position(e.Pos)
			tgoErrs = append(tgoErrs, typeError{
				Line: pos.Line,
				Col:  pos.Column,
				Msg:  e.Msg,
				Soft: e.Soft,
			})
		},
	}
	cfg.Check("test", fset, []*ast.File{f}, nil)

	goErrs := make(map[typeError]struct{})
	gocfg := gotypes.Config{
		Importer: &tgoimporter.TgoDefaultImporter2{I: goimporter.Default().(gotypes.ImporterFrom)},
		Error: func(err error) {
			e := err.(gotypes.Error)
			pos := gofset.Position(e.Pos)
			goErrs[typeError{
				Line: pos.Line,
				Col:  pos.Column,
				Msg:  e.Msg,
				Soft: e.Soft,
			}] = struct{}{}
		},
	}
	gocfg.Check("test", gofset, []*goast.File{gof}, nil)

	// TODO: strings.Replace errors

	unreported := maps.Clone(goErrs)
	for _, v := range tgoErrs {
		if _, ok := goErrs[v]; !ok {
			t.Errorf("missing error: %v", v)
		} else if testing.Verbose() {
			t.Logf("ok error: %v", v)
		}
		delete(unreported, v)
	}

	for v := range unreported {
		t.Errorf("additional unexpected error: %v", v)
	}
}

// TODO: fuzz test (if tgo type checking succedes, the transpiled with go/types should also).
// and the error position (of go/types and tgo-lang/lang/types) and the error messages should be similar (and the same positions).
