package analyzer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mateusz834/tgo/tgotest"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/token"
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
							Msg:    v.Message,
							Line:   v.StartPos.Line,
							Column: v.StartPos.Column,
						})
					}
					return t
				}
				return nil
			})
		})
	}
}
