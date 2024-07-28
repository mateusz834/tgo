package transpiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/format"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/scanner"
	"github.com/mateusz834/tgoast/token"
)

const tgosrc = `package templates

func test(sth string) {
	test = 2
	<div
		test = 2
		@kdjf="lol \{sth} kdfjd"
		@test="test \{func(a strin) {
			<div>
				"hello world :)"
			</div>
		}()}"
	>
		test = 2
		"hello \{sth}test"
	</div>

	<div>"hello world"</div>
}
`

var _ = `
	//<div @xd="lol"></div>
	//<span>
	//a:=3;</span>
	//<span>
	//	a := 2
	//</span>
	//<span>
	//	lol
	//</span>
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
	if v, ok := err.(analyzer.AnalyzeErrors); ok {
		for _, v := range v {
			t.Logf("%v", v)
		}
	}
	if err != nil {
		t.Fatalf("%v", err)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.OpenTagStmt:
			t.Logf("%q", tgosrc[n.Pos()-1:n.End()-1])
		}
		return true
	})

	out := Transpile(f, fs, tgosrc)
	t.Log("\n" + out)

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

	var o strings.Builder
	format.Node(&o, fs, f)
	t.Log("\n" + o.String())

	if o.String() != out {
		t.Log(gitDiff(t.TempDir(), o.String(), out))
		t.Fatal("difference found")
	}

	// ff := fileTranspiler{f: f}
	// ff.transpile()
	// ast.Print(fs, f)
}

//func TestGoSourceUnchanged(t *testing.T) {
//	files, err := filepath.Glob("../../tgoast/parser/*.go")
//	if err != nil {
//		t.Error(err)
//	}
//
//	for _, file := range files {
//		c, err := os.ReadFile(file)
//		if err != nil {
//			t.Error(err)
//		}
//		fs := token.NewFileSet()
//		f, err := parser.ParseFile(fs, file, c, parser.SkipObjectResolution|parser.ParseComments)
//		if err != nil {
//			t.Errorf("failed while parsing %q: %v", file, err)
//		}
//		out := Transpile(fs, f, string(c))
//		if out != string(c) {
//			t.Errorf("unexpected tranform of  %v", files)
//			d, err := gitDiff(t.TempDir(), string(c), out)
//			if err == nil {
//				t.Logf("\n%v", d)
//			}
//		}
//		t.Logf("\n%v", out)
//
//	}
//}

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

//func TestFileToIr(t *testing.T) {
//	cases := []struct {
//		src string
//		out ir.File
//	}{
//		{
//			src: `package main
//func test() {
//	<div> /* lol
//	*/
//	</div>
//}
//`,
//			out: ir.File{
//				&ir.Source{Source: "package main\nfunc test() {\n\t"},
//				&ir.StaticWrite{Strings: []string{"<", "div", ">", "</", "div", ">"}},
//				&ir.Source{Source: "\n}"},
//			},
//		},
//		{
//			src: `package main
//func test(sth string) { <div></div>
//	<div>
//		sth = "test"
//	</div>
//}
//`,
//			out: ir.File{
//				&ir.Source{Source: "package main\nfunc test(sth string) {\n\t"},
//				&ir.StaticWrite{Strings: []string{"<", "div", ">"}},
//				&ir.Source{Source: "\n\t\tsth = \"test\"}\n\t"},
//				&ir.StaticWrite{Strings: []string{"</", "div", ">"}},
//				&ir.Source{Source: "\n}"},
//			},
//		},
//	}
//}
