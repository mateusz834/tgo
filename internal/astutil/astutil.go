package astutil

import (
	"math"
	"strconv"

	"github.com/tgo-lang/lang/ast"
)

// FileUniqueIdent return an identifier that is not used throughout the entire
// file. Returns defaultIdent, if it is not used in the file, otherwise an identifier
// based on defaultIdent is generated.
func FileUniqueIdent(f *ast.File, defaultIdent string) string {
	used := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if n.Name == defaultIdent {
				used = true
				return false
			}
		}
		return true
	})
	if !used {
		return defaultIdent
	}

	usedIdents := make(map[string]struct{})
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			usedIdents[n.Name] = struct{}{}
		}
		return true
	})

	for i := range uint(math.MaxUint) {
		ident := defaultIdent + strconv.FormatUint(uint64(i), 10)
		if _, ok := usedIdents[ident]; !ok {
			return ident
		}
	}

	panic("unreachable")
}
