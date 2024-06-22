package transpiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/scanner"
	"github.com/mateusz834/tgoast/token"
)

const tgosrc = `package templates

import "sth"

var a = "lol"

var a, b = "lol", "dkf"

var (
	a string = "test"
	a = "test"
)

var a = sth[0][1](  s,  sdjf  )

var a = a {a: 3}

var a = a {
	a: (3),
	a: 3,
	a: 3,
	a: 3,
}

var xd = x[:]
var xd = x[:dkj]
var xd = x[aa:]

var xd = x[ : ]
var xd = x[ : dkj ]
var xd = x[  aa : ]

var xd = x[:dkj:df]
var xd = x[a:dkj:df]
var xd = x[ a : dkj : df ]

var a = a.(type)
var a = a.(*lol)

var c = + a

var c = kdj + a

func a() { }

func a() {
	func() {
		b(a)
	}
	a(a)

	if a:=3;a<3 {
	} else {
		dfj
	}

	switch a {
	case 1:
		"lol"
	case 2:
	case 2:
	case 2:
	}

	select {
		case <-a:
	}

	for i:=0;true;i<2 {
		a(d)
	}

	for true {
		a(d)
	}

	for range a {
		a(d)
	}

	for a := range a {
		a(d)
	}

	for b, a := range a {
		a(d)
	}

	a = [55]int{}
	a := map[string]int{}
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

	out := Transpile(fs, f, tgosrc)
	t.Logf("\n%v", out)
}

func TestGoSourceUnchanged(t *testing.T) {
	files, err := filepath.Glob("../../tgoast/parser/*.go")
	if err != nil {
		t.Error(err)
	}

	for _, file := range files {
		c, err := os.ReadFile(file)
		if err != nil {
			t.Error(err)
		}
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, file, c, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			t.Errorf("failed while parsing %q: %v", file, err)
		}
		out := Transpile(fs, f, string(c))
		if out != string(c) {
			t.Errorf("unexpected tranform of  %v", files)
			d, err := gitDiff(t.TempDir(), string(c), out)
			if err == nil {
				t.Logf("\n%v", d)
			}
		}
		t.Logf("\n%v", out)

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
