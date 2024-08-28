package transpiler

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	goast "go/ast"
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

const tgosrc = `
package main

func a() {
	"000"
	/*line a:1:1*/ //
}
`

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
	err = nil
	if v, ok := err.(analyzer.AnalyzeErrors); ok {
		for _, v := range v {
			t.Logf("%v", v)
		}
	}
	if err != nil {
		t.Fatalf("%v", err)
	}

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
	f.Fuzz(func(t *testing.T, name string, src string) {
		t.Logf("file name: %q", name)

		if _, err := parser.ParseFile(
			token.NewFileSet(),
			name,
			"//line "+name+":1:1\n/*line "+name+":1:1*/\npackage main",
			parser.ParseComments|parser.SkipObjectResolution,
		); err != nil ||
			strings.ContainsRune(name, '\r') || strings.ContainsRune(src, '\r') ||
			strings.ContainsRune(name, '\f') || strings.ContainsRune(name, '\n') ||
			strings.ContainsRune(name, '`') || strings.ContainsRune(name, '\'') ||
			strings.Contains(name, "*/") {
			return
		}

		t.Logf("source:\n%v", src)
		t.Logf("quoted input:\n%q", src)

		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, name, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return
		}

		if analyzer.Analyze(fs, f) != nil {
			return
		}

		// TODO: remove this and fix the formatter :)
		// probably a upstream fix to go also
		//if len(f.Comments) > 0 {
		//	for _, v := range f.Comments {
		//		if v.End() < f.Package {
		//			t.Skip()
		//		}
		//	}
		//}

		// Because of the go doc formatting rules it is currently impossible
		// with the curreent golang formatter to make sure that the comments
		// before the package token do not cause different formatting when
		// the line directive is prepended.
		for _, v := range f.Comments {
			for _, v := range v.List {
				if v.Pos() > f.Package {
					break
				}
				return
			}
		}

		emptyBlockStmtCount := 0
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BlockStmt:
				if len(n.List) == 0 {
					emptyBlockStmtCount++
				}
			case *ast.BasicLit:
				if strings.ContainsRune(n.Value, '`') {
					t.Skip()
				}
			case *ast.TemplateLiteralExpr:
				for _, v := range n.Strings {
					if strings.ContainsRune(v, '`') {
						t.Skip()
					}
				}
			}
			return true
		})

		out := Transpile(f, fs, src)
		t.Logf("transpiled output:\n%v", out)

		fsgo := gotoken.NewFileSet()
		fgo, err := goparser.ParseFile(fsgo, name, out, goparser.ParseComments|goparser.SkipObjectResolution)
		if err != nil {
			if v, ok := err.(goscanner.ErrorList); ok {
				for _, v := range v {
					t.Logf("%v", v)
				}
			}
			t.Logf("quoted transpiled output:\n%q", out)
			t.Fatalf("goparser.ParseFile() = %v; want <nil>", err)
		}

		var tgoFmt strings.Builder
		if err := format.Node(&tgoFmt, fs, f); err != nil {
			if strings.Contains(err.Error(), "format.Node internal error (") {
				return
			}
			t.Fatalf("format.Node() = %v; want <nil>", err)
		}

		if tgoFmt.String() != src {
			return // input src not formatted
		}

		var outFmt strings.Builder
		if err := goformat.Node(&outFmt, fsgo, fgo); err != nil {
			t.Fatalf("goformat.Node() = %v; want <nil>", err)
		}
		t.Logf("formatted transpiled output:\n%v", outFmt.String())

		t.Logf("quoted transpiled output:\n%q", out)
		t.Logf("quoted formatted transpiled output:\n%q", outFmt.String())

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
		if emptyBlockStmtCount != emptyBlockStmtCountGo {
			t.Fatalf("transpiled output contains unexpected empty *ast.BlockStmt")
		}
	})
}
