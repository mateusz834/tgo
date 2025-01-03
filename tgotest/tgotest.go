package tgotest

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/parser"
	"github.com/tgo-lang/lang/printer"
	"github.com/tgo-lang/lang/token"
)

type Error struct {
	Msg          string
	Line, Column int
}

var update = flag.Bool("update", false, "")

const (
	prefix    = "// ERROR: "
	errConcat = " ERROR: "
)

var printerConfig = printer.Config{Tabwidth: 8, Mode: printer.UseSpaces | printer.TabIndent}

func Test(t *testing.T, path string, testFunc func(fset *token.FileSet, f *ast.File) []Error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.tgo", contents, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var s strings.Builder
	if err := printerConfig.Fprint(&s, fset, f); err != nil {
		t.Fatal(err)
	}

	if *update {
		// When -update then print and parse, so that postion info (column) is the same after another print.
		fset = token.NewFileSet()
		f, err = parser.ParseFile(fset, "test.tgo", s.String(), parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
	} else if s.String() != string(contents) {
		// Printing might change the source, so require formatted code, otherwise
		// the Column (from token.Position) might change, making error message in comments invalid.
		t.Fatalf("%v is not formatted (run tests with -update)", path)
	}

	got := testFunc(fset, f)

	slices.SortFunc(got, func(x, y Error) int {
		return cmp.Or(
			cmp.Compare(x.Line, y.Line),
			cmp.Compare(x.Column, y.Column),
			cmp.Compare(x.Msg, y.Msg),
		)
	})

	want := []Error{}
	newComments := []*ast.CommentGroup{}
	for _, cg := range f.Comments {
		newCommentGroup := &ast.CommentGroup{}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, prefix) {
				for _, v := range strings.Split(c.Text[len(prefix):], errConcat) {
					var column int
					var msg string
					if _, err := fmt.Sscanf(v, "col(%v): %q", &column, &msg); err != nil {
						t.Fatal(err)
					} else if column == 0 || msg == "" {
						t.Fatal("invalid comment")
					}
					want = append(want, Error{
						Msg:    msg,
						Line:   fset.PositionFor(c.Pos(), false).Line,
						Column: column,
					})
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
			g[v.Line] = append(g[v.Line], fmt.Sprintf("col(%v): %q", v.Column, v.Msg))
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
		var out strings.Builder
		if err := printerConfig.Fprint(&out, fset, f); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(out.String()), 0666); err != nil {
			t.Fatal(err)
		}
		return
	}

	if !slices.Equal(got, want) {
		var gotStr, wantStr strings.Builder
		for _, v := range got {
			gotStr.WriteString(fmt.Sprintf("line: %v, column: %v: %v\n", v.Line, v.Column, v.Msg))
		}
		for _, v := range want {
			wantStr.WriteString(fmt.Sprintf("line: %v, column: %v: %v\n", v.Line, v.Column, v.Msg))
		}
		t.Fatalf("unexpected errors\ngot:\n%v\nwant:\n%v", gotStr.String(), wantStr.String())
	}
}
