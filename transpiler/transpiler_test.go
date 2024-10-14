package transpiler

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	goast "go/ast"
	"go/build/constraint"
	goformat "go/format"
	goparser "go/parser"
	goscanner "go/scanner"
	gotoken "go/token"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/format"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/scanner"
	"github.com/mateusz834/tgoast/token"
)

// TODO: transpilation of:
//func A(A){"\{a}\{""}l"}
//func a(a string) { "a" }

// TODO: write a one huge concatenated string and then introduce comments ?

func test() {
	// Transpile string writes to something like this,
	// this way we can also preserve comments, by adding them
	// betwen strings.
	io.WriteString(nil, ""+
		"<div"+
		" attr=\"value\""+
		" attr=\"valuedf\""+
		">"+
		"<span>"+
		"value between div"+
		"</span>"+
		"</div>",
	)
}

//const tgosrc = `
//package A
//
//func a(a string) {
//	<div>
//		/*test*/ a()
//	</div>
//}
//`

//const tgosrc = "// Code generated by tgo DO NOT EDIT.\n\n//line 0:1:1\npackage A\nimport()//0\nfunc()A(){switch{case 0:0//go:build\n0}}"

//const tgosrc = `package A
//
//func a(a string) {
//	<div>"test"</div>
//}
//`

// TODO: figure out this a printer (tgo).
// why this happend in template part, but not
// in *ast.ParenExpr.

const tgosrc = `package templates

func A(A) {
	<div>
	a:
	for _, v := range a {
		<div>
			for _, v := range 5 {
				<div>
				</div>
				continue
			}
		</div>
		switch a {
		case "lol":
			<div>
			</div>
			break
		case "test":
			break
		}
	}
	</div>
}
`

// TODO: issue with the assert, it should not allow three newliens in a row?

func TestTranspiler(t *testing.T) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "test.tgo", tgosrc, parser.SkipObjectResolution|parser.ParseComments)

	ast.Print(fs, f)

	if err != nil {
		if v, ok := err.(scanner.ErrorList); ok {
			for _, err := range v {
				t.Errorf("%v", err)
			}
		}
		t.Fatalf("%v", err)
	}

	err = analyzer.Analyze(fs, f)
	if v, ok := err.(analyzer.AnalyzeErrors); ok {
		for _, v := range v {
			t.Logf("%v", v)
		}
	}
	if err != nil {
		t.Fatalf("%v", err)
	}

	t.Log("\n" + string(tgosrc))
	fmted, err := format.Source([]byte(tgosrc))
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Log("\n" + string(fmted))

	var oo strings.Builder
	if err := format.Node(&oo, fs, f); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + oo.String())

	out := Transpile(f, fs, tgosrc)
	t.Log("\n" + out)
	t.Logf("\n%q", out)

	fs = token.NewFileSet()
	f, err = parser.ParseFile(fs, "test.go", out, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		if v, ok := err.(scanner.ErrorList); ok {
			for _, err := range v {
				t.Errorf("%v", err)
			}
		}
		t.Fatalf("%v", err)
	}

	ast.Print(fs, f)

	var o strings.Builder
	if err := format.Node(&o, fs, f); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + o.String())

	if o.String() != out {
		t.Log(gitDiff(t.TempDir(), o.String(), out))
		t.Fatal("difference found")
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

func TestTranspile(t *testing.T) {
	const testdata = "./testdata"
	files, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range files {
		ext := filepath.Ext(v.Name())
		if ext != ".tgo" {
			continue
		}

		testFile := filepath.Join(testdata, v.Name())
		expectFileName := filepath.Join(testdata, v.Name()[:len(v.Name())-len(".tgo")]+".go")
		t.Run(testFile, func(t *testing.T) {
			content, err := os.ReadFile(testFile)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%v:\n%s", testFile, content)

			fs := token.NewFileSet()
			f, err := parser.ParseFile(fs, testFile, content, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}

			out := Transpile(f, fs, string(content))
			t.Logf("transpiled %v:\n%s", testFile, out)

			expect, err := os.ReadFile(expectFileName)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if err := os.WriteFile(expectFileName, []byte(out), 06660); err != nil {
						t.Fatal(err)
					}
					return
				}
				t.Fatal(err)
			}

			if out != string(expect) {
				t.Log("make following changes to make this test pass:")
				t.Log(gitDiff(t.TempDir(), out, string(expect)))
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
				hasOnlyEmptyStrs := true
				for _, v := range n.List {
					isEmptyStr := false
					if v, ok := v.(*ast.ExprStmt); ok {
						if v, ok := v.X.(*ast.BasicLit); ok && v.Kind == token.STRING {
							str, err := strconv.Unquote(v.Value)
							if err != nil {
								panic(err) // unreachable, AST is valid
							}
							isEmptyStr = str == ""
						}
					}
					if !isEmptyStr {
						hasOnlyEmptyStrs = false
						break
					}
				}
				if hasOnlyEmptyStrs {
					expectedEmptyBlockStmtCount++
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
		if expectedEmptyBlockStmtCount != emptyBlockStmtCountGo {
			t.Error("transpiled output contains an unexpected, empty *ast.BlockStmt")
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
