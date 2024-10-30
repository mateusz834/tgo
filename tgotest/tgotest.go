package tgotest

import (
	"cmp"
	"flag"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/printer"
	"github.com/mateusz834/tgoast/token"
)

type Error struct {
	Msg  string
	Line int
}

var update = flag.Bool("update", false, "")

const (
	prefix    = "// ERROR: "
	errConcat = " ERROR: "
)

func Test(t *testing.T, path string, testFunc func(fset *token.FileSet, f *ast.File) []Error) {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// TODO: error when file is not formatted
	// TODO: when -update then format then print and parse (so that postion info is the same after another print)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.tgo", contents, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	got := testFunc(fset, f)

	want := []Error{}
	newComments := []*ast.CommentGroup{}
	for _, cg := range f.Comments {
		newCommentGroup := &ast.CommentGroup{}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, prefix) {
				for _, v := range strings.Split(c.Text[len(prefix):], errConcat) {
					want = append(want, Error{Msg: v, Line: fset.PositionFor(c.Pos(), false).Line})
				}
				continue
			}
			newCommentGroup.List = append(newCommentGroup.List, c)
		}
		if len(newCommentGroup.List) != 0 {
			newComments = append(newComments, newCommentGroup)
		}
	}

	if *update {
		g := make(map[int][]string)
		for _, v := range got {
			g[v.Line] = append(g[v.Line], v.Msg)
		}
		for line, errs := range g {
			newComments = append(newComments, &ast.CommentGroup{
				List: []*ast.Comment{{
					Slash: fset.File(f.FileStart).LineStart(line+1) - 1,
					Text:  prefix + strings.Join(errs, errConcat),
				}},
			})
		}
		slices.SortFunc(newComments, func(x, y *ast.CommentGroup) int {
			return cmp.Compare(x.Pos(), y.Pos())
		})

		f.Comments = newComments
		c := printer.Config{Tabwidth: 8, Mode: printer.UseSpaces | printer.TabIndent}
		var out strings.Builder
		if err := c.Fprint(&out, fset, f); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(out.String()), 0666); err != nil {
			t.Fatal(err)
		}
		return
	}

	if !slices.Equal(got, want) {
		t.Fatal("unexpected errors")
	}
}
