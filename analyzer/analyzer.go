package analyzer

import (
	"fmt"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

// TODO: matching tags <div> has corresponding close + void tags

func Analyze(fs *token.FileSet, f *ast.File) error {
	a := &analyzer{
		context: contextNotTgo,
		ctx: &analyzerContext{
			fs: fs,
		},
	}
	ast.Walk(a, f)
	if len(a.ctx.errors) != 0 {
		return a.ctx.errors
	}
	return nil
}

type AnalyzeError struct {
	Message          string
	StartPos, EndPos token.Position
}

func (a AnalyzeError) Error() string {
	return fmt.Sprintf("%v: %v", a.StartPos, a.Message)
}

type AnalyzeErrors []AnalyzeError

func (a AnalyzeErrors) Error() string {
	switch len(a) {
	case 0:
		return "no errors"
	case 1:
		return a[0].Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", a[0], len(a)-1)
}

type analyzerContext struct {
	errors AnalyzeErrors
	fs     *token.FileSet
}

type context uint8

const (
	contextNotTgo context = iota
	contextTgoBody
	contextTgoTag
)

type analyzer struct {
	context context
	ctx     *analyzerContext
}

func (f *analyzer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.FuncDecl:
		if len(n.Type.Params.List) == 0 {
			return &analyzer{context: contextNotTgo, ctx: f.ctx}
		}
		return &analyzer{context: contextTgoBody, ctx: f.ctx}
	case *ast.FuncLit:
		if len(n.Type.Params.List) == 0 {
			return &analyzer{context: contextNotTgo, ctx: f.ctx}
		}
		return &analyzer{context: contextTgoBody, ctx: f.ctx}
	case *ast.BlockStmt, *ast.IfStmt,
		*ast.SwitchStmt, *ast.CaseClause,
		*ast.ForStmt, *ast.SelectStmt,
		*ast.CommClause, *ast.RangeStmt,
		*ast.TypeSwitchStmt, *ast.ExprStmt:
		return f
	case *ast.TemplateLiteralExpr:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "Template literal is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		return &analyzer{context: contextNotTgo, ctx: f.ctx}
	case *ast.OpenTagStmt:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "Open Tag is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		return &analyzer{context: contextTgoTag, ctx: f.ctx}
	case *ast.EndTagStmt:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "End Tag is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		return nil
	case *ast.AttributeStmt:
		if f.context != contextTgoTag {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "Attribute is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		if v, ok := n.Value.(*ast.TemplateLiteralExpr); ok {
			a := &analyzer{context: contextNotTgo, ctx: f.ctx}
			for _, v := range v.Parts {
				ast.Walk(a, v)
			}
		}
		return nil
	default:
		return &analyzer{context: contextNotTgo, ctx: f.ctx}
	}
}
