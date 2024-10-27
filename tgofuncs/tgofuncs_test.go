package tgofuncs

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

func TestTgoFuncs(t *testing.T) {
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

			ast.Print(fset, f)

			var got strings.Builder
			info := Check(f)
			for _, v := range info.TgoFuncs {
				got.WriteString(fset.Position(v.Pos()).String())
				got.WriteString("\n")
			}

			if *update {
				out := tgo
				if got.String() != "" {
					out += "======\n" + got.String()
				}
				if err := os.WriteFile(fileName, []byte(out), 0666); err != nil {
					t.Fatal(err)
				}
				return
			}

			if !separatorFound {
				if got.String() != "" {
					t.Logf("source:\n%v", tgo)
					t.Fatalf("unexpected tgo funcs, got:\n%v\nwant: <empty>", got.String())
				}
				return
			}

			if got.String() != errors {
				t.Logf("source:\n%v", tgo)
				t.Fatalf("unexpected tgo funcs, got:\n%v\nwant:\n%v", got.String(), errors)
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
		f.Add(string(content), "", "")
	}
}

func FuzzTgoFuncs(f *testing.F) {
	fuzzAddDir(f, "./testdata")
	fuzzAddDir(f, ".")
	fuzzAddDir(f, "..")
	fuzzAddDir(f, "../transpiler")
	fuzzAddDir(f, "../analyzer")

	f.Add(`package templates

import "github.com/mateusz834/tgo"

func a(tgo.Ctx) error {
	return nil
}
`, "", "")

	const tgoModuleSrc = "package tgo\ntype Ctx struct{}\ntype Error = error"

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

	f.Fuzz(func(t *testing.T, src, additionalPath, additionalSrc string) {
		gofset := gotoken.NewFileSet()
		gof, err := goparser.ParseFile(gofset, "test.go", src, goparser.SkipObjectResolution)
		if err != nil {
			return
		}

		additionalFile, err := goparser.ParseFile(gofset, "test.go", src, goparser.SkipObjectResolution)
		if err != nil {
			return
		}

		additionalPackageCfg := gotypes.Config{
			Importer: funcImporter(func(path string) (*gotypes.Package, error) {
				return nil, errors.New("imports not allowed")
			}),
		}
		additionalPkg, _ := additionalPackageCfg.Check(additionalPath, gofset, []*goast.File{additionalFile}, nil)

		tgofset := token.NewFileSet()
		tgof, err := parser.ParseFile(tgofset, "test.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err) // succesfully parsed by the Go parser, this should not happen.
		}

		got := Check(tgof)

		cfg := gotypes.Config{
			Importer: funcImporter(func(path string) (*gotypes.Package, error) {
				if path == "github.com/mateusz834/tgo" {
					return tgoPkg, nil
				} else if additionalPkg != nil && path == additionalPath {
					return additionalPkg, nil
				}
				return nil, errors.New("custom imports not allowed")
			}),
		}
		infos := gotypes.Info{Types: make(map[goast.Expr]gotypes.TypeAndValue)}
		if _, err := cfg.Check("pkgname", gofset, []*goast.File{gof}, &infos); err != nil {
			return
		}

		want := []goast.Node{}
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
			// TODO: Identical does Unalias, the checher does not follow them.
			return gotypes.Identical(tv.Type, ctxType) && gotypes.Identical(tvRet.Type, errorType)
		}
		goast.Inspect(gof, func(n goast.Node) bool {
			switch n := n.(type) {
			case *goast.FuncDecl:
				if checkFuncType(n.Type) {
					want = append(want, n)
				}
			case *goast.FuncLit:
				if checkFuncType(n.Type) {
					want = append(want, n)
				}
			}
			return true
		})

		if !slices.EqualFunc(got.TgoFuncs, want, func(x ast.Node, y goast.Node) bool {
			return tgofset.PositionFor(x.Pos(), false).Offset == gofset.PositionFor(y.Pos(), false).Offset
		}) {
			t.Logf("source:\n%v", src)
			t.Logf("quoted source:\n%q", src)
			for _, n := range got.TgoFuncs {
				var b strings.Builder
				ast.Fprint(&b, tgofset, n, nil)
				t.Logf("got:\n%s", b.String())
			}
			for _, n := range want {
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
