package tgoimporter

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"runtime"
)

type goImporter struct {
	I    types.ImporterFrom
	Fset *token.FileSet
	pkg  *types.Package
}

func NewGoImporter(fset *token.FileSet) types.Importer {
	return &goImporter{
		Fset: fset,
		I:    importer.ForCompiler(fset, runtime.Compiler, nil).(types.ImporterFrom),
	}
}

func (f *goImporter) Import(path string) (*types.Package, error) {
	if path == "github.com/mateusz834/tgo" {
		return f.tgoPkg()
	}
	return f.I.Import(path)
}

func (f *goImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "github.com/mateusz834/tgo" {
		return f.tgoPkg()
	}
	return f.I.ImportFrom(path, dir, mode)
}

func (f *goImporter) tgoPkg() (*types.Package, error) {
	pkg, err := parseTgoPackageAsGo(f.Fset)
	if err != nil {
		return nil, err
	}
	f.pkg = pkg
	return pkg, nil
}

// TODO: test that proves (only for CI) that this is the same as in the tgo repo.
func parseTgoPackageAsGo(fset *token.FileSet) (*types.Package, error) {
	tgoModuleFile, err := parser.ParseFile(fset, "tgo.go", tgoModuleSrc, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	cfg := types.Config{
		Importer: importer.Default(),
	}
	tgoPkg, err := cfg.Check("github.com/mateusz834/tgo", fset, []*ast.File{tgoModuleFile}, nil)
	if err != nil {
		return nil, err
	}

	return tgoPkg, nil
}
