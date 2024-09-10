package transpiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/token"
)

func fuzzAddDir2(f *testing.F, testdata string) {
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

func FuzzIterWhite(f *testing.F) {
	fuzzAddDir2(f, "../../tgoast/printer/testdata/tgo")
	fuzzAddDir2(f, "../../tgoast/parser/testdata/tgo")
	fuzzAddDir2(f, "../../tgoast/printer")
	fuzzAddDir2(f, "../../tgoast/printer/testdata")
	fuzzAddDir2(f, "../../tgoast/parser")
	fuzzAddDir2(f, "../../tgoast/parser/testdata")
	fuzzAddDir2(f, "../../tgoast/ast")
	f.Fuzz(func(t *testing.T, src string) {
		fset := token.NewFileSet()
		fset.AddFile("test.go", fset.Base(), 99)
		f, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return
		}

		var (
			startNode ast.Node
			endNode   ast.Node
		)
		ast.Inspect(f, func(n ast.Node) bool {
			if n, ok := n.(*ast.BlockStmt); ok {
				if len(n.List) >= 2 {
					startNode = n.List[0]
					endNode = n.List[1]
				}

			}
			return true
		})

		if startNode != nil && endNode != nil {
			tr := transpiler{f: f, fs: fset, src: src}
			for v := range tr.iterWhite(startNode.End(), endNode.Pos()) {
				if !strings.HasPrefix(src[fset.PositionFor(v.pos, false).Offset:], v.text) {
					t.Fatalf("source: %q, invalid pos for: %v", src, v)
				}
			}
		}
	})
}
