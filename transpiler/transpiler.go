package transpiler

import (
	"fmt"
	"strings"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

const debug = false

func Transpile(fs *token.FileSet, f *ast.File, source string) string {
	t := transpiler{
		fs:     fs,
		f:      f,
		source: source,
	}
	t.out.Grow(len(source))
	t.transpile()
	return t.out.String()
}

type transpiler struct {
	fs     *token.FileSet
	f      *ast.File
	source string

	out strings.Builder

	lastSourcePos token.Pos
}

func (t *transpiler) fromSource(i, j token.Pos) {
	if debug {
		fmt.Printf(
			"appending original source (%v-%v): %q\n",
			t.fs.Position(i), t.fs.Position(j), t.source[i-1:j-1],
		)
	}
	if i < t.lastSourcePos {
		panic("unreachable")
	}
	t.lastSourcePos = i
	t.out.WriteString(t.source[i-1 : j-1])
}

func (t *transpiler) transpile() {
	prevDeclEnd := t.f.FileStart
	for _, v := range t.f.Decls {
		t.fromSource(prevDeclEnd, v.Pos())
		prevDeclEnd = v.End()
		t.transpileDecl(v)
	}
	t.fromSource(prevDeclEnd, t.f.FileEnd)
}

func (t *transpiler) transpileDecl(d ast.Decl) {
	ast.Inspect(d, t.inspect)
}

func inspectNodes[T ast.Node](t *transpiler, prevEndPos token.Pos, nodes []T) {
	for _, v := range nodes {
		t.fromSource(prevEndPos, v.Pos())
		ast.Inspect(v, t.inspect)
		prevEndPos = v.End()
	}
}

func (t *transpiler) inspect(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Ident:
		t.fromSource(n.Pos(), n.End())
		return false
	case *ast.Ellipsis:
		panic("here")
	case *ast.BasicLit:
		t.fromSource(n.Pos(), n.End())
		return false
	case *ast.FuncLit:
		t.fromSource(n.Pos(), n.Body.Lbrace)
		ast.Inspect(n.Body, t.inspect)
		return false
	case *ast.CompositeLit:
		start := n.Pos()
		if n.Type != nil {
			ast.Inspect(n.Type, t.inspect)
			t.fromSource(n.Type.End(), n.Lbrace+1)
			start = n.Lbrace + 1
		}
		if len(n.Elts) == 0 {
			t.fromSource(start, n.Rbrace+1)
			return false
		}
		inspectNodes(t, start, n.Elts)
		t.fromSource(n.Elts[len(n.Elts)-1].End(), n.Rbrace+1)
		return false
	case *ast.ParenExpr:
		t.fromSource(n.Lparen, n.X.Pos())
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.Rparen+1)
		return false
	case *ast.SelectorExpr:
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.End())
		return false
	case *ast.IndexExpr:
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.Index.Pos())
		ast.Inspect(n.Index, t.inspect)
		t.fromSource(n.Index.End(), n.Rbrack+1)
		return false
	case *ast.IndexListExpr:
		panic("todo")
	case *ast.SliceExpr:
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.Lbrack+1)
		lastEnd := n.Lbrack + 1
		if n.Low != nil {
			t.fromSource(lastEnd, n.Low.Pos())
			ast.Inspect(n.Low, t.inspect)
			lastEnd = n.Low.End()
		}
		if n.High != nil {
			t.fromSource(lastEnd, n.High.Pos())
			ast.Inspect(n.High, t.inspect)
			lastEnd = n.High.End()
		}
		if n.Max != nil {
			t.fromSource(lastEnd, n.Max.Pos())
			ast.Inspect(n.Max, t.inspect)
			lastEnd = n.Max.End()
		}
		t.fromSource(lastEnd, n.Rbrack+1)
		return false
	case *ast.TypeAssertExpr:
		ast.Inspect(n.X, t.inspect)
		if n.Type == nil {
			t.fromSource(n.X.End(), n.Rparen+1)
			return false
		}
		t.fromSource(n.X.End(), n.Type.Pos())
		ast.Inspect(n.Type, t.inspect)
		t.fromSource(n.Type.End(), n.Rparen+1)
		return false
	case *ast.CallExpr:
		ast.Inspect(n.Fun, t.inspect)
		if len(n.Args) != 0 {
			t.fromSource(n.Fun.End(), n.Lparen+1)
			inspectNodes(t, n.Lparen+1, n.Args)
			t.fromSource(n.Args[len(n.Args)-1].End(), n.Rparen+1)
		} else {
			t.fromSource(n.Fun.End(), n.End())
		}
		return false
	case *ast.StarExpr:
		t.fromSource(n.Star, n.X.Pos())
		ast.Inspect(n.X, t.inspect)
		return false
	case *ast.UnaryExpr:
		t.fromSource(n.OpPos, n.X.Pos())
		ast.Inspect(n.X, t.inspect)
		return false
	case *ast.BinaryExpr:
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.Y.Pos())
		ast.Inspect(n.Y, t.inspect)
		return false
	case *ast.KeyValueExpr:
		ast.Inspect(n.Key, t.inspect)
		t.fromSource(n.Key.End(), n.Value.Pos())
		ast.Inspect(n.Value, t.inspect)
		return false

	case *ast.GenDecl:
		if n.Tok != token.VAR {
			t.fromSource(n.Pos(), n.End())
			return false
		}
		t.fromSource(n.Pos(), n.Specs[0].Pos())
		for i, v := range n.Specs {
			ast.Inspect(v, t.inspect)
			if i+1 != len(n.Specs) {
				t.fromSource(v.End(), n.Specs[i+1].Pos())
			}
		}
		t.fromSource(n.Specs[len(n.Specs)-1].End(), n.End())
		return false
	case *ast.ValueSpec:
		t.fromSource(n.Pos(), n.Names[len(n.Names)-1].End())
		start := n.Names[len(n.Names)-1].End()
		if n.Type != nil {
			t.fromSource(start, n.Type.Pos())
			ast.Inspect(n.Type, t.inspect)
			start = n.Type.End()
		}
		inspectNodes(t, start, n.Values)
		return false
	case *ast.FuncDecl:
		t.fromSource(n.Pos(), n.Body.Lbrace)
		ast.Inspect(n.Body, t.inspect)
		return false

	case *ast.DeclStmt:
		ast.Inspect(n.Decl, t.inspect)
		return false
	case *ast.EmptyStmt:
		t.fromSource(n.Pos(), n.End())
		return false
	case *ast.LabeledStmt:
		ast.Inspect(n.Label, t.inspect)
		t.fromSource(n.Label.End(), n.Stmt.Pos())
		ast.Inspect(n.Stmt, t.inspect)
		return false
	case *ast.ExprStmt:
		ast.Inspect(n.X, t.inspect)
		return false
	case *ast.SendStmt:
		ast.Inspect(n.Chan, t.inspect)
		t.fromSource(n.Chan.End(), n.Value.Pos())
		ast.Inspect(n.Value, t.inspect)
		return false
	case *ast.IncDecStmt:
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.End())
		return false
	case *ast.AssignStmt:
		inspectNodes(t, n.Pos(), n.Lhs)
		t.fromSource(n.Lhs[len(n.Lhs)-1].End(), n.Rhs[0].Pos())
		inspectNodes(t, n.Rhs[0].Pos(), n.Rhs)
		return false
	case *ast.GoStmt:
		t.fromSource(n.Pos(), n.Call.Pos())
		ast.Inspect(n.Call, t.inspect)
		return false
	case *ast.DeferStmt:
		t.fromSource(n.Pos(), n.Call.Pos())
		ast.Inspect(n.Call, t.inspect)
		return false
	case *ast.ReturnStmt:
		if len(n.Results) != 0 {
			t.fromSource(n.Pos(), n.Results[0].Pos())
			inspectNodes(t, n.Results[0].Pos(), n.Results)
			return false
		}
		t.fromSource(n.Pos(), n.End())
		return false
	case *ast.BranchStmt:
		t.fromSource(n.Pos(), n.End())
		return false
	case *ast.BlockStmt:
		if len(n.List) != 0 {
			t.fromSource(n.Pos(), n.List[0].Pos())
			inspectNodes(t, n.List[0].Pos(), n.List)
			t.fromSource(n.List[len(n.List)-1].End(), n.End())
			return false
		}
		t.fromSource(n.Pos(), n.End())
		return false
	case *ast.IfStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(n.Pos(), n.Init.Pos())
			ast.Inspect(n.Init, t.inspect)
			start = n.Init.End()
		}
		t.fromSource(start, n.Cond.Pos())
		ast.Inspect(n.Cond, t.inspect)
		t.fromSource(n.Cond.End(), n.Body.Pos())
		ast.Inspect(n.Body, t.inspect)
		if n.Else != nil {
			t.fromSource(n.Body.End(), n.Else.Pos())
			ast.Inspect(n.Else, t.inspect)
		}
		return false
	case *ast.CaseClause:
		if len(n.List) != 0 {
			t.fromSource(n.Pos(), n.List[0].Pos())
			inspectNodes(t, n.List[0].Pos(), n.List)
			t.fromSource(n.List[len(n.List)-1].End(), n.Colon+1)
		} else {
			t.fromSource(n.Pos(), n.Colon+1)
		}
		if len(n.Body) != 0 {
			t.fromSource(n.Colon+1, n.Body[0].Pos())
			inspectNodes(t, n.Body[0].Pos(), n.Body)
		}
		return false
	case *ast.SwitchStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(n.Pos(), n.Init.Pos())
			ast.Inspect(n.Init, t.inspect)
			start = n.Init.End()
		}
		if n.Tag != nil {
			t.fromSource(start, n.Tag.Pos())
			ast.Inspect(n.Tag, t.inspect)
			start = n.Tag.End()
		}
		t.fromSource(start, n.Body.Pos())
		ast.Inspect(n.Body, t.inspect)
		return false
	case *ast.TypeSwitchStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(n.Pos(), n.Init.Pos())
			ast.Inspect(n.Init, t.inspect)
			start = n.Init.End()
		}
		t.fromSource(start, n.Assign.Pos())
		ast.Inspect(n.Assign, t.inspect)
		t.fromSource(n.Assign.End(), n.Body.Pos())
		ast.Inspect(n.Body, t.inspect)
		return false
	case *ast.CommClause:
		if n.Comm != nil {
			t.fromSource(n.Pos(), n.Comm.Pos())
			ast.Inspect(n.Comm, t.inspect)
			t.fromSource(n.Comm.End(), n.Colon+1)
		} else {
			t.fromSource(n.Pos(), n.Colon+1)
		}
		if len(n.Body) != 0 {
			t.fromSource(n.Colon+1, n.Body[0].Pos())
			inspectNodes(t, n.Body[0].Pos(), n.Body)
		}
		return false
	case *ast.SelectStmt:
		t.fromSource(n.Pos(), n.Body.Pos())
		ast.Inspect(n.Body, t.inspect)
		return false
	case *ast.ForStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(start, n.Init.Pos())
			ast.Inspect(n.Init, t.inspect)
			start = n.Init.End()
		}
		if n.Cond != nil {
			t.fromSource(start, n.Cond.Pos())
			ast.Inspect(n.Cond, t.inspect)
			start = n.Cond.End()
		}
		if n.Post != nil {
			t.fromSource(start, n.Post.Pos())
			ast.Inspect(n.Post, t.inspect)
			start = n.Post.End()
		}
		t.fromSource(start, n.Body.Pos())
		ast.Inspect(n.Body, t.inspect)
		return false
	case *ast.RangeStmt:
		start := n.Pos()
		if n.Key != nil {
			t.fromSource(start, n.Key.Pos())
			ast.Inspect(n.Key, t.inspect)
			start = n.Key.End()
		}
		if n.Value != nil {
			t.fromSource(start, n.Value.Pos())
			ast.Inspect(n.Value, t.inspect)
			start = n.Value.End()
		}
		t.fromSource(start, n.X.Pos())
		ast.Inspect(n.X, t.inspect)
		t.fromSource(n.X.End(), n.Body.Pos())
		ast.Inspect(n.Body, t.inspect)
		return false

	case *ast.ArrayType, *ast.StructType,
		*ast.FuncType, *ast.InterfaceType,
		*ast.MapType, *ast.ChanType:
		t.fromSource(n.Pos(), n.End())
		return false
	case nil:
		return false
	default:
		panic("unexpected type: " + fmt.Sprintf("%T", n))
	}
}
