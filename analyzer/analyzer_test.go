package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mateusz834/tgo/tgotest"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

func TestAnalyze(t *testing.T) {
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
			tgotest.Test(t, fileName, func(fset *token.FileSet, f *ast.File) []tgotest.Error {
				if err := Analyze(fset, f); err != nil {
					t := []tgotest.Error{}
					for _, v := range err.(AnalyzeErrors) {
						t = append(t, tgotest.Error{
							Msg:  fmt.Sprintf("col(%v): %v", v.StartPos.Column, v.Message),
							Line: v.StartPos.Line,
						})
					}
					return t
				}
				return nil
			})
		})
	}
}
