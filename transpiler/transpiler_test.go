package transpiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/format"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/scanner"
	"github.com/mateusz834/tgoast/token"
)

const tgosrc = `package templates

func test(sth string) {
	<div
		@class="test \{sth}"
		@class2="test"
		@class3="\{"lol"}"
	>
		"test"
		"testing"
		<div>"sth\{test}"</div>
	</div>

	//<div>
	//	"test"
	//	{
	//		<span><div>"test \{sth}"</div></span>
	//	}
	//</div>

	//<div
	//	testi() //;
	//	kdfj
	//	@kdjf="lol \{sth} kdfjd"
	//	@test="test \{func(a strin) {
	//		<div>
	//			"hello world :)"
	//		</div>
	//	}()}"
	//>
	//	test = 2
	//	"hello \{sth}test"
	//</div>

	//<span>
	//	<div><div>"hello world"</div></div>
	//</span>

	//<div><span>"test\{sth}aa\{sth2}bb\{sth3}"</span></div>

	//<div@class="test"@class="test"><div>"lol"</div></div>

	//// TODO: ignore spaces between div name and attr and between attr and attr.
	//// TODO: comments?
	//<div
	//	@class="test"
	//	@class="testingg"
	//	@class="test"
	//	@class="testingg"
	//	a = 3
	//>
	//	"test"
	//</div>
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

	fmted, err := format.Source([]byte(tgosrc))
	if err == nil {
		t.Log("\n" + string(fmted))
	}

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

func FuzzFormattedTgoProducesFormattedGoSource(t *testing.F) {
	t.Fuzz(func(t *testing.T, src string) {
		t.Logf("source:\n%v", src)

		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, "file.tgo", src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return
		}

		out := Transpile(f, fs, src)
		t.Logf("transpiled output:\n%v", out)

		fsgo := gotoken.NewFileSet()
		fgo, err := goparser.ParseFile(fsgo, "file.go", src, goparser.ParseComments|goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("goparser.ParseFile() = %v; want <nil>", err)
		}

		var outFmt strings.Builder
		if err := goformat.Node(&outFmt, fsgo, fgo); err != nil {
			t.Fatalf("goformat.Node() = %v; want <nil>", err)
		}
		t.Logf("formatted output:\n%v", outFmt)

		if outFmt.String() != out {
			t.Log(gitDiff(t.TempDir(), outFmt.String(), out))
			t.Fatal("difference found")
		}
	})
}
