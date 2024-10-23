package analyzer

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/token"
)

var update = flag.Bool("update", false, "")

func TestAnalyze(t *testing.T) {
	const testdata = "./testdata"
	files, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range files {
		dirFiles := []string{v.Name()}
		if v.IsDir() {
			files, err := os.ReadDir(filepath.Join(testdata, v.Name()))
			if err != nil {
				t.Fatal(err)
			}
			dirFiles = []string{}
			for _, f := range files {
				dirFiles = append(dirFiles, filepath.Join(v.Name(), f.Name()))
			}
		}
		for _, fileName := range dirFiles {
			t.Run(fileName, func(t *testing.T) {
				fileName := filepath.Join(testdata, fileName)
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

				ast.Print(fset, f)

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
}
