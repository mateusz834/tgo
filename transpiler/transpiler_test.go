package transpiler

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
