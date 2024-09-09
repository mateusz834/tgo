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
	a = 3
}
`,
			want: []iterWhiteResult{
				{whiteIndent, 27, "\n\t"},
			},
		},
		{
			src: `package main
func main() {
	// test
}
`,
			want: []iterWhiteResult{
				{whiteIndent, 27, "\n\t"},
				{whiteComment, 29, "// test"},
				{whiteIndent, 36, "\n"},
			},
		},
		{
			src: `package main
func main() {
	/*test*/ /*test*/ //test
	// test
	// testing
	/*testing*/
}
`,
			want: []iterWhiteResult{
				{whiteIndent, 27, "\n\t"},
				{whiteIndent, 29, "/*test*/"},
				{whiteWhite, 37, " "},
				{whiteIndent, 38, "/*test*/"},
				{whiteWhite, 44, " "},
				{whiteIndent, 45, "//test"},
				{whiteIndent, 51, "\n\t"},
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

		fd := f.Decls[0].(*ast.FuncDecl)
		start := fd.Body.Lbrace + 1
		end := fd.Body.Rbrace
		if len(fd.Body.List) > 0 {
			end = fd.Body.List[0].Pos()
		}

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
