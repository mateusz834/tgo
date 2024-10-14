package analyzer

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/format"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/scanner"
	"github.com/mateusz834/tgoast/token"
)

const tgosrc = `package main

import "github.com/mateusz834/tgo"

func test(a string) error {
	<
		div
		/*test*/ @attr
	>
	</div>
	<div> // lol
	<span> // lol
	</span> // lol
	</div> // lol
}
`

func TestTgo(t *testing.T) {
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

	err = Analyze(fs, f)
	if v, ok := err.(AnalyzeErrors); ok {
		for _, v := range v {
			t.Logf("%v", v)
		}
	}
	t.Logf("%v", err)

	err = format.Node(os.Stdout, fs, f)
	if err != nil {
		t.Fatal(err)
	}
}

var update = flag.Bool("update", false, "")

func TestAnalyze(t *testing.T) {
	const testdata = "./testdata"
	files, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range files {
		t.Run(v.Name(), func(t *testing.T) {
			fileName := filepath.Join(testdata, v.Name())
			c, err := os.ReadFile(fileName)
			if err != nil {
				t.Fatal(err)
			}

			tgo, errors, separatorFound := strings.Cut(string(c), "======\n")

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.tgo", tgo, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			var gotErrors strings.Builder
			if err := Analyze(fset, f); err != nil {
				for _, v := range err.(AnalyzeErrors) {
					gotErrors.WriteString(v.Error())
					gotErrors.WriteString("\n")
				}
				gotErrors.WriteString(err.Error())
				gotErrors.WriteString("\n")
			}

			if *update {
				out := tgo
				if gotErrors.String() != "" {
					out += "======\n" + gotErrors.String()
				}
				if err := os.WriteFile(fileName, []byte(out), 0666); err != nil {
					t.Fatal(err)
				}
				return
			}

			if !separatorFound {
				if gotErrors.String() != "" {
					t.Logf("source:\n%v", tgo)
					t.Fatalf("unexpected errors, got:\n%v\nwant: <empty>", gotErrors.String())
				}
				return
			}

			if gotErrors.String() != errors {
				t.Logf("source:\n%v", tgo)
				t.Fatalf("unexpected errors, got:\n%v\nwant:\n%v", gotErrors.String(), errors)
			}
		})
	}
}
