package analyzer

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	gotypes "go/types"

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
		f.Add(string(content))
	}
}

func FuzzContextAnalyzer(f *testing.F) {
	fuzzAddDir(f, "./testdata/context")
	fuzzAddDir(f, ".")

	f.Add(`package main

import "github.com/mateusz834/tgo"

func main() {
}

func a(tgo.Ctx) error {
	return nil
}
`)

	const tgoModuleSrc = `package tgo

type Ctx struct{}
`

	fset := gotoken.NewFileSet()
	tgoModuleFile, err := goparser.ParseFile(fset, "tgo.go", tgoModuleSrc, goparser.SkipObjectResolution)
	if err != nil {
		f.Fatal(err)
	}

	cfg := gotypes.Config{
		Importer: funcImporter(func(path string) (*gotypes.Package, error) {
			return nil, errors.New("imports not allowed")
		}),
	}
	tgoPkg, err := cfg.Check("github.com/mateusz834/tgo", fset, []*goast.File{tgoModuleFile}, nil)
	if err != nil {
		f.Fatal(err)
	}

	ctxType := tgoPkg.Scope().Lookup("Ctx").Type()
	errorType := gotypes.Universe.Lookup("error").Type()

	f.Fuzz(func(t *testing.T, src string) {
		gofset := gotoken.NewFileSet()
		gof, err := goparser.ParseFile(gofset, "test.go", src, goparser.SkipObjectResolution)
		if err != nil {
			return
		}

		tgofset := token.NewFileSet()
		tgof, err := parser.ParseFile(tgofset, "test.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err) // succesfully parsed by the Go parser, this should not happen.
		}

		for _, v := range tgof.Imports {
			if v.Name != nil && v.Name.Name == "." {
				t.Skip()
			}
		}

		ctx := &analyzerContext{fset: tgofset}
		tgoFuncs := checkContext(ctx, tgof)
		if len(ctx.errors) != 0 {
			t.Fatal("unexpected errors from checkContext")
		}

		cfg := gotypes.Config{
			Importer: funcImporter(func(path string) (*gotypes.Package, error) {
				if path == "github.com/mateusz834/tgo" {
					return tgoPkg, nil
				}
				// TODO: add a fake package that fuzz can fuzz :).
				return nil, errors.New("custom imports not allowed")
			}),
		}
		infos := gotypes.Info{Types: make(map[goast.Expr]gotypes.TypeAndValue)}
		if _, err := cfg.Check("pkgname", gofset, []*goast.File{gof}, &infos); err != nil {
			return
		}

		expectTgoFuncs := make(map[goast.Node]struct{})
		checkFuncType := func(ft *goast.FuncType) bool {
			if len(ft.Params.List) == 0 || ft.Results == nil || len(ft.Results.List) != 1 {
				return false
			}
			tv, ok := infos.Types[ft.Params.List[0].Type]
			if !ok {
				panic("unreachable")
			}
			tvRet, ok := infos.Types[ft.Results.List[0].Type]
			if !ok {
				panic("unreachable")
			}
			return gotypes.Identical(tv.Type, ctxType) && gotypes.Identical(tvRet.Type, errorType)
		}
		goast.Inspect(gof, func(n goast.Node) bool {
			switch n := n.(type) {
			case *goast.FuncDecl:
				if checkFuncType(n.Type) {
					expectTgoFuncs[n] = struct{}{}
				}
			case *goast.FuncLit:
				if checkFuncType(n.Type) {
					expectTgoFuncs[n] = struct{}{}
				}
			}
			return true
		})

		gotFuncs := make([]int, 0, len(tgoFuncs))
		for n := range tgoFuncs {
			gotFuncs = append(gotFuncs, tgofset.PositionFor(n.Pos(), false).Offset)
		}
		slices.Sort(gotFuncs)

		wantFuncs := make([]int, 0, len(expectTgoFuncs))
		for n := range expectTgoFuncs {
			wantFuncs = append(wantFuncs, gofset.PositionFor(n.Pos(), false).Offset)
		}
		slices.Sort(wantFuncs)

		if !slices.Equal(gotFuncs, wantFuncs) {
			t.Logf("source:\n%v", src)
			t.Logf("quoted source:\n%q", src)
			for n := range tgoFuncs {
				var b strings.Builder
				ast.Fprint(&b, tgofset, n, nil)
				t.Logf("got:\n%s", b.String())
			}
			for n := range expectTgoFuncs {
				var b strings.Builder
				goast.Fprint(&b, gofset, n, nil)
				t.Logf("want:\n%s", b.String())
			}
			t.Fatalf("unexpected tgo-funcs")
		}

	})
}

type funcImporter func(path string) (*gotypes.Package, error)

func (f funcImporter) Import(path string) (*gotypes.Package, error) {
	return f(path)
}
