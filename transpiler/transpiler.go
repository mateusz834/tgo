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
		t.inspect(v)
	}
	t.fromSource(prevDeclEnd, t.f.FileEnd)
}

func inspectNodes[T ast.Node](t *transpiler, prevEndPos token.Pos, nodes []T) {
	for _, v := range nodes {
		t.fromSource(prevEndPos, v.Pos())
		t.inspect(v)
		prevEndPos = v.End()
	}
}

func (t *transpiler) inspect(n ast.Node) {
	switch n := n.(type) {
	case *ast.Ident:
		t.fromSource(n.Pos(), n.End())
	case *ast.Ellipsis:
		panic("here")
	case *ast.BasicLit:
		t.fromSource(n.Pos(), n.End())
	case *ast.FuncLit:
		t.fromSource(n.Pos(), n.Body.Lbrace)
		t.inspect(n.Body)
	case *ast.CompositeLit:
		start := n.Pos()
		if n.Type != nil {
			t.inspect(n.Type)
			t.fromSource(n.Type.End(), n.Lbrace+1)
			start = n.Lbrace + 1
		}
		if len(n.Elts) == 0 {
			t.fromSource(start, n.Rbrace+1)
			return
		}
		inspectNodes(t, start, n.Elts)
		t.fromSource(n.Elts[len(n.Elts)-1].End(), n.Rbrace+1)
	case *ast.ParenExpr:
		t.fromSource(n.Lparen, n.X.Pos())
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.Rparen+1)
	case *ast.SelectorExpr:
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.End())
	case *ast.IndexExpr:
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.Index.Pos())
		t.inspect(n.Index)
		t.fromSource(n.Index.End(), n.Rbrack+1)
	case *ast.IndexListExpr:
		panic("todo")
	case *ast.SliceExpr:
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.Lbrack+1)
		lastEnd := n.Lbrack + 1
		if n.Low != nil {
			t.fromSource(lastEnd, n.Low.Pos())
			t.inspect(n.Low)
			lastEnd = n.Low.End()
		}
		if n.High != nil {
			t.fromSource(lastEnd, n.High.Pos())
			t.inspect(n.High)
			lastEnd = n.High.End()
		}
		if n.Max != nil {
			t.fromSource(lastEnd, n.Max.Pos())
			t.inspect(n.Max)
			lastEnd = n.Max.End()
		}
		t.fromSource(lastEnd, n.Rbrack+1)
	case *ast.TypeAssertExpr:
		t.inspect(n.X)
		if n.Type == nil {
			t.fromSource(n.X.End(), n.Rparen+1)
			return
		}
		t.fromSource(n.X.End(), n.Type.Pos())
		t.inspect(n.Type)
		t.fromSource(n.Type.End(), n.Rparen+1)
	case *ast.CallExpr:
		t.inspect(n.Fun)
		if len(n.Args) != 0 {
			t.fromSource(n.Fun.End(), n.Lparen+1)
			inspectNodes(t, n.Lparen+1, n.Args)
			t.fromSource(n.Args[len(n.Args)-1].End(), n.Rparen+1)
		} else {
			t.fromSource(n.Fun.End(), n.End())
		}
	case *ast.StarExpr:
		t.fromSource(n.Star, n.X.Pos())
		t.inspect(n.X)
	case *ast.UnaryExpr:
		t.fromSource(n.OpPos, n.X.Pos())
		t.inspect(n.X)
	case *ast.BinaryExpr:
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.Y.Pos())
		t.inspect(n.Y)
	case *ast.KeyValueExpr:
		t.inspect(n.Key)
		t.fromSource(n.Key.End(), n.Value.Pos())
		t.inspect(n.Value)

	case *ast.GenDecl:
		if n.Tok != token.VAR {
			t.fromSource(n.Pos(), n.End())
			return
		}
		t.fromSource(n.Pos(), n.Specs[0].Pos())
		inspectNodes(t, n.Specs[0].Pos(), n.Specs)
		t.fromSource(n.Specs[len(n.Specs)-1].End(), n.End())
	case *ast.ValueSpec:
		t.fromSource(n.Pos(), n.Names[len(n.Names)-1].End())
		start := n.Names[len(n.Names)-1].End()
		if n.Type != nil {
			t.fromSource(start, n.Type.Pos())
			t.inspect(n.Type)
			start = n.Type.End()
		}
		inspectNodes(t, start, n.Values)
	case *ast.FuncDecl:
		t.fromSource(n.Pos(), n.Body.Lbrace)
		t.inspect(n.Body)

	case *ast.DeclStmt:
		t.inspect(n.Decl)
	case *ast.EmptyStmt:
		t.fromSource(n.Pos(), n.End())
	case *ast.LabeledStmt:
		t.inspect(n.Label)
		t.fromSource(n.Label.End(), n.Stmt.Pos())
		t.inspect(n.Stmt)
	case *ast.ExprStmt:
		t.inspect(n.X)
	case *ast.SendStmt:
		t.inspect(n.Chan)
		t.fromSource(n.Chan.End(), n.Value.Pos())
		t.inspect(n.Value)
	case *ast.IncDecStmt:
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.End())
	case *ast.AssignStmt:
		inspectNodes(t, n.Pos(), n.Lhs)
		t.fromSource(n.Lhs[len(n.Lhs)-1].End(), n.Rhs[0].Pos())
		inspectNodes(t, n.Rhs[0].Pos(), n.Rhs)
	case *ast.GoStmt:
		t.fromSource(n.Pos(), n.Call.Pos())
		t.inspect(n.Call)
	case *ast.DeferStmt:
		t.fromSource(n.Pos(), n.Call.Pos())
		t.inspect(n.Call)
	case *ast.ReturnStmt:
		if len(n.Results) != 0 {
			t.fromSource(n.Pos(), n.Results[0].Pos())
			inspectNodes(t, n.Results[0].Pos(), n.Results)
			return
		}
		t.fromSource(n.Pos(), n.End())
	case *ast.BranchStmt:
		t.fromSource(n.Pos(), n.End())
	case *ast.BlockStmt:
		if len(n.List) != 0 {
			t.fromSource(n.Pos(), n.List[0].Pos())
			inspectNodes(t, n.List[0].Pos(), n.List)
			t.fromSource(n.List[len(n.List)-1].End(), n.End())
			return
		}
		t.fromSource(n.Pos(), n.End())
	case *ast.IfStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(n.Pos(), n.Init.Pos())
			t.inspect(n.Init)
			start = n.Init.End()
		}
		t.fromSource(start, n.Cond.Pos())
		t.inspect(n.Cond)
		t.fromSource(n.Cond.End(), n.Body.Pos())
		t.inspect(n.Body)
		if n.Else != nil {
			t.fromSource(n.Body.End(), n.Else.Pos())
			t.inspect(n.Else)
		}
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
	case *ast.SwitchStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(n.Pos(), n.Init.Pos())
			t.inspect(n.Init)
			start = n.Init.End()
		}
		if n.Tag != nil {
			t.fromSource(start, n.Tag.Pos())
			t.inspect(n.Tag)
			start = n.Tag.End()
		}
		t.fromSource(start, n.Body.Pos())
		t.inspect(n.Body)
	case *ast.TypeSwitchStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(n.Pos(), n.Init.Pos())
			t.inspect(n.Init)
			start = n.Init.End()
		}
		t.fromSource(start, n.Assign.Pos())
		t.inspect(n.Assign)
		t.fromSource(n.Assign.End(), n.Body.Pos())
		t.inspect(n.Body)
	case *ast.CommClause:
		if n.Comm != nil {
			t.fromSource(n.Pos(), n.Comm.Pos())
			t.inspect(n.Comm)
			t.fromSource(n.Comm.End(), n.Colon+1)
		} else {
			t.fromSource(n.Pos(), n.Colon+1)
		}
		if len(n.Body) != 0 {
			t.fromSource(n.Colon+1, n.Body[0].Pos())
			inspectNodes(t, n.Body[0].Pos(), n.Body)
		}
	case *ast.SelectStmt:
		t.fromSource(n.Pos(), n.Body.Pos())
		t.inspect(n.Body)
	case *ast.ForStmt:
		start := n.Pos()
		if n.Init != nil {
			t.fromSource(start, n.Init.Pos())
			t.inspect(n.Init)
			start = n.Init.End()
		}
		if n.Cond != nil {
			t.fromSource(start, n.Cond.Pos())
			t.inspect(n.Cond)
			start = n.Cond.End()
		}
		if n.Post != nil {
			t.fromSource(start, n.Post.Pos())
			t.inspect(n.Post)
			start = n.Post.End()
		}
		t.fromSource(start, n.Body.Pos())
		t.inspect(n.Body)
	case *ast.RangeStmt:
		start := n.Pos()
		if n.Key != nil {
			t.fromSource(start, n.Key.Pos())
			t.inspect(n.Key)
			start = n.Key.End()
		}
		if n.Value != nil {
			t.fromSource(start, n.Value.Pos())
			t.inspect(n.Value)
			start = n.Value.End()
		}
		t.fromSource(start, n.X.Pos())
		t.inspect(n.X)
		t.fromSource(n.X.End(), n.Body.Pos())
		t.inspect(n.Body)

	case *ast.ArrayType, *ast.StructType,
		*ast.FuncType, *ast.InterfaceType,
		*ast.MapType, *ast.ChanType:
		t.fromSource(n.Pos(), n.End())
	case nil:
	default:
		panic("unexpected type: " + fmt.Sprintf("%T", n))
	}
}
