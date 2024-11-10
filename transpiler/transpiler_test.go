package transpiler

import (
	"cmp"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	goast "go/ast"
	"go/build/constraint"
	goformat "go/format"
	goparser "go/parser"
	goprinter "go/printer"
	goscanner "go/scanner"
	gotoken "go/token"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/format"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/printer"
	"github.com/mateusz834/tgoast/token"
)

// TODO: transpilation of:
//func A(A){"\{a}\{""}l"}
//func a(a string) { "a" }

//const testSrc = `
//package A//
//import("github.com/mateusz834/tgo")
//func A(tgo.Ctx)error{"\{""}"//
//}
//func A(tgo.Ctx)error{"\{""}"
//func(tgo.Ctx)error{//
//type tgo A
//"\{""}"  }}`

//const testSrc = "package A//\nimport(\"github.com/mateusz834/tgo\") \nfunc A(tgo.Ctx)error{\"\\{\"\"}\"//\n}\nfunc A(tgo.Ctx)error{\"\\{\"\"}\"\nfunc(tgo.Ctx)error{//\ntype tgo A\n\"\\{\"\"}\"  }}"

//const testSrc = "package A\nimport()\nfunc()A()(...A)"

const testSrc = `package A
import"github.com/mateusz834/tgo"
func A(tgo.Ctx)error{
	<div>
	A:
	</div>
}
`

func TestTest(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "0", testSrc, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	if err := analyzer.Analyze(fset, f); err != nil {
		t.Fatal(err)
	}

	out := Transpile(f, fset, testSrc)
	t.Logf("transpiled:\n%s", out)
	t.Logf("transpiled:\n%q", out)

	fsetgo := gotoken.NewFileSet()
	fgo, err := goparser.ParseFile(fsetgo, "transpiled.go", out, goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		if v, ok := err.(goscanner.ErrorList); ok {
			for _, v := range v {
				file := fsetgo.File(fgo.FileStart)
				t.Logf("%v: %v", file.PositionFor(file.Pos(v.Pos.Offset), false), v)
			}
		}
		t.Fatalf("goparser.ParseFile(Transpile(src)) = %v; want = <nil>", err)
	}

	var s strings.Builder
	goPrinterConfig.Fprint(&s, fsetgo, fgo)
	t.Logf("formatted:\n%v", s.String())
	t.Logf("quoted formatted:\n%s", s.String())
}

var (
	update          = flag.Bool("update", false, "")
	printerConfig   = printer.Config{Tabwidth: 8, Mode: printer.UseSpaces | printer.TabIndent}
	goPrinterConfig = goprinter.Config{Tabwidth: 8, Mode: goprinter.UseSpaces | goprinter.TabIndent}
)

func TestTranspile(t *testing.T) {
	const testdata = "./testdata"
	files, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range files {
		if v.IsDir() {
			continue
		}
		t.Run(v.Name(), func(t *testing.T) {
			file := filepath.Join(testdata, v.Name())
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			// TODO: support multiple file inputs (go and tgo) and outputs.

			tgo, transpiled, _ := strings.Cut(string(content), "======\n")

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.tgo", tgo, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}

			fmt := true
			for _, v := range f.Comments {
				for _, v := range v.List {
					if v.Text == "//nofmt" {
						fmt = false
						break
					}
				}
			}

			if err := analyzer.Analyze(fset, f); err != nil {
				for _, v := range err.(analyzer.AnalyzeErrors) {
					t.Log(v)
				}
				t.Fatal(err)
			}

			var s strings.Builder
			printerConfig.Fprint(&s, fset, f)

			if *update {
				if fmt {
					tgo = s.String()
					fset = token.NewFileSet()
					f, err = parser.ParseFile(fset, "test.tgo", tgo, parser.ParseComments|parser.SkipObjectResolution)
					if err != nil {
						t.Fatal(err)
					}
				}
				transpiled = Transpile(f, fset, tgo)
				if err := os.WriteFile(file, []byte(tgo+"======\n"+transpiled), 0660); err != nil {
					t.Fatal(err)
				}
			}

			out := Transpile(f, fset, tgo)

			if fmt && s.String() != tgo {
				t.Fatal("file not formatted (format with -update)")
			}

			gofset := gotoken.NewFileSet()
			gof, err := goparser.ParseFile(gofset, "test.go", out, goparser.ParseComments|goparser.SkipObjectResolution)
			if err != nil {
				t.Logf("transpiled:\n%s", out)
				t.Fatalf("failed to parse transpiled source: %v", err)
			}

			var goFmted strings.Builder
			goPrinterConfig.Fprint(&goFmted, gofset, gof)
			if fmt && goFmted.String() != out {
				t.Fatalf("transpiled output not formatted:\n%s\nwant:\n%s", out, goFmted.String())
			}

			// TODO: type-check output (also listing the errors in the file?)

			if out != transpiled {
				t.Log("make following changes to make this test pass:")
				t.Log(gitDiff(t.TempDir(), out, transpiled))
				t.Fatal("difference found")
			}
		})
	}
}

func fuzzAddDir(f *testing.F, testdata string) {
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
		f.Add(testFile, string(content))
	}
}

func FuzzFormattedTgoProducesFormattedGoSource(f *testing.F) {
	fuzzAddDir(f, "../../tgoast/printer/testdata/tgo")
	fuzzAddDir(f, "../../tgoast/parser/testdata/tgo")
	fuzzAddDir(f, "../../tgoast/printer")
	fuzzAddDir(f, "../../tgoast/printer/testdata")
	fuzzAddDir(f, "../../tgoast/parser")
	fuzzAddDir(f, "../../tgoast/parser/testdata")
	fuzzAddDir(f, "../../tgoast/ast")
	fuzzAddDir(f, "../analyzer/testdata")
	fuzzAddDir(f, "../tgofuncs/testdata")

	f.Add("a", `package main

func a() {
	"000"
	//
}
`)

	f.Add("a", `

// test
package main
`)

	f.Fuzz(func(t *testing.T, name string, src string) {
		if testing.Verbose() {
			t.Logf("file name: %q", name)
			t.Logf("source:\n%v", src)
			t.Logf("quoted input:\n%q", src)
		}

		if !utf8.ValidString(name) {
			return
		}

		for _, v := range []string{"\r", "\f", "\n", "\v", "\000", "\ufeff"} {
			if strings.Contains(name, v) {
				return
			}
		}

		fset := token.NewFileSet()

		// Add an unused file to FileSet, so that fset.Base()
		// is incrased before parsing the file. This way we also
		// make sure that we are converting token.Pos into source offset
		// correctly in the transpiler.
		fset.AddFile("t", -1, 99)

		f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return
		}

		if analyzer.Analyze(fset, f) != nil {
			return
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

		want := make(map[nodeInfo]struct{})
		ignore := make(map[*ast.Ident]bool)
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AttributeStmt, *ast.TemplateLiteralExpr,
				*ast.TemplateLiteralPart, *ast.File:
				return true
			case *ast.OpenTagStmt:
				ignore[n.Name] = true
				return true
			case *ast.EndTagStmt:
				ignore[n.Name] = true
				return true
			case *ast.CommentGroup, *ast.Comment:
				return true
			case *ast.ExprStmt:
				switch n := n.X.(type) {
				case *ast.TemplateLiteralExpr:
					return true
				case *ast.BasicLit:
					// TODO: bad
					if n.Kind == token.STRING {
						return true
					}
				}
			case *ast.BasicLit:
				// TODO: bad
				if n.Kind == token.STRING {
					return true
				}
			case *ast.Ident:
				if ignore[n] {
					return true
				}
			case *ast.CommClause:
				return true
			case *ast.CaseClause:
				return true
			case nil:
				return true
			}

			info := genNodeInfo[token.Token](n, func(p token.Pos) (line int, column int) {
				pos := fset.Position(p)
				return pos.Line, pos.Column
			})

			if _, ok := want[info]; ok {
				for k := range want {
					t.Log(k)
				}
				panic("unreachable")
			}

			if testing.Verbose() {
				t.Logf("tgo key: %v", info)
			}
			want[info] = struct{}{}

			return true
		})

		missing := maps.Clone(want)
		goast.Inspect(fgo, func(n goast.Node) bool {
			if n == nil {
				return true
			}

			info := genNodeInfo[gotoken.Token](n, func(p gotoken.Pos) (line int, column int) {
				pos := fsetgo.Position(p)
				return pos.Line, pos.Column
			})

			if testing.Verbose() {
				t.Logf("go key: %v", info)
			}

			if _, ok := want[info]; ok {
				delete(missing, info)
			}

			return true
		})

		if len(f.Comments) == 0 && len(missing) != 0 {
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

		// The Go formatter moves comments around, bacause it treats every comment
		// at Column == 1 as doc comment, and it moves directives to the end of a comment.
		// Line directive should not be moved in any way (https://go.dev/cl/609077).
		// We are not able to keep that formatted.
		for i, v := range fgo.Comments {
			for _, c := range v.List {
				p := fsetgo.PositionFor(c.Pos(), false)
				if (p.Column == 1 && strings.HasPrefix(c.Text, "//line")) || strings.HasPrefix(c.Text, "/*line") {
					if len(v.List) != 1 {
						return
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
								return
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
						if f.Func.Name() == "github.com/mateusz834/tgoast/ast.sortSpecs" &&
							strings.Contains(v, "invalid line number") {
							return
						}

						// https://go.dev/cl/610115 https://go.dev/issue/69206
						if f.Func.Name() == "github.com/mateusz834/tgoast/printer.combinesWithName" &&
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
				t.Fatalf("format.Node() = %v; want <nil>", err)
			}
		}()

		if tgoFmt.String() != src {
			return // input src not formatted
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
	})

}

type pos struct {
	line, column int
}

type nodeInfo struct {
	nodeName  string
	nodeStart pos
	nodeEnd   pos
	other     string
}

func genNodeInfo[TOK fmt.Stringer, POS interface{ IsValid() bool }](
	n interface {
		Pos() POS
		End() POS
	},
	posToLineCol func(pos POS) (line int, column int),
) nodeInfo {
	v := reflect.ValueOf(n).Elem()

	var info strings.Builder
	info.Grow(32)

	appendPos := func(name string, pos POS) {
		if info.Len() != 0 {
			info.WriteString(";")
		}
		line, column := posToLineCol(pos)
		info.WriteString(name)
		info.WriteString(":")
		info.WriteString(strconv.FormatInt(int64(line), 10))
		info.WriteString(":")
		info.WriteString(strconv.FormatInt(int64(column), 10))
	}

	for i := range v.NumField() {
		fv := v.Field(i)
		fieldName := v.Type().Field(i).Name
		if fv.Type() == reflect.TypeFor[POS]() {
			appendPos(fieldName, fv.Interface().(POS))
		} else if fv.Type() == reflect.TypeFor[string]() {
			if info.Len() != 0 {
				info.WriteString(";")
			}
			info.WriteString(fieldName)
			info.WriteString(":")
			info.WriteString(strconv.Quote(fv.String()))
		} else if fv.Type() == reflect.TypeFor[TOK]() {
			if info.Len() != 0 {
				info.WriteString(";")
			}
			info.WriteString(fieldName)
			info.WriteString(":")
			info.WriteString(fv.Interface().(TOK).String())
		}
	}

	startLine, startCol := posToLineCol(n.Pos())
	endLine, endCol := posToLineCol(n.End())
	switch any(n).(type) {
	case *ast.LabeledStmt, *goast.LabeledStmt:
		endLine, endCol = 0, 0
	}

	return nodeInfo{
		nodeName:  v.Type().Name(),
		nodeStart: pos{line: startLine, column: startCol},
		nodeEnd:   pos{line: endLine, column: endCol},
		other:     info.String(),
	}
}

func gitDiff(tmpDir string, got, expect string) (string, error) {
	gotPath := filepath.Join(tmpDir, "got")
	gotFile, err := os.Create(gotPath)
	if err != nil {
		return "", err
	}
	defer gotFile.Close()
	if _, err := gotFile.WriteString(got); err != nil {
		return "", err
	}

	expectPath := filepath.Join(tmpDir, "expect")
	expectFile, err := os.Create(expectPath)
	if err != nil {
		return "", err
	}
	defer expectFile.Close()
	if _, err := expectFile.WriteString(expect); err != nil {
		return "", err
	}

	var out strings.Builder
	cmd := exec.Command("git", "diff", "-U 100000", "--no-index", "--color=always", "--ws-error-highlight=all", gotPath, expectPath)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && cmd.ProcessState.ExitCode() != 1 {
		return "", err
	}
	return out.String(), nil
}
