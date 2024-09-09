package transpiler

import (
	"slices"
	"testing"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/token"
)

func TestIterWhite(t *testing.T) {
	cases := []struct {
		src  string
		want []iterWhiteResult
	}{
		{
			src: `package main
func main() {
}
`,
			want: []iterWhiteResult{
				{whiteWhite, 1, " "},
			},
		},
	}

	for _, tt := range cases {
		fset := token.NewFileSet()
		fset.AddFile("test", fset.Base(), 100) // increase fset.Base()

		for i := range tt.want {
			tt.want[i].pos += 101
		}

		f, err := parser.ParseFile(fset, "test.go", tt.src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}

		start := f.Decls[0].(*ast.FuncDecl).Body.Lbrace + 1
		end := f.Decls[0].(*ast.FuncDecl).Body.Rbrace - 1

		tr := transpiler{f: f, fs: fset, src: tt.src}
		got := slices.Collect(tr.iterWhite(start, end))
		if !slices.Equal(got, tt.want) {
			t.Errorf(
				"\nsource: %v\ngot:%#v\nwant:%#v",
				tt.src, got, tt.want,
			)
		}

	}
}
