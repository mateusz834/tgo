package tgoimporter

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
)

type TgoDefaultImporter2 struct {
	I    types.ImporterFrom
	Fset *token.FileSet
	pkg  *types.Package
}

func (f *TgoDefaultImporter2) Import(path string) (*types.Package, error) {
	if path == "github.com/mateusz834/tgo" {
		return f.tgoPkg()
	}
	return f.I.Import(path)
}

func (f *TgoDefaultImporter2) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "github.com/mateusz834/tgo" {
		return f.tgoPkg()
	}
	return f.I.ImportFrom(path, dir, mode)
}

func (f *TgoDefaultImporter2) tgoPkg() (*types.Package, error) {
	pkg, err := parseTgo(f.Fset)
	if err != nil {
		return nil, err
	}
	f.pkg = pkg
	return pkg, nil
}

// TODO: test that proves (only for CI) that this is the same as in the tgo repo.
func parseTgo(fset *token.FileSet) (*types.Package, error) {
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
