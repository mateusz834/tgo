package analyzer

import (
	"fmt"
	"strings"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

func Analyze(fs *token.FileSet, f *ast.File) error {
	ctx := &analyzerContext{
		fs: fs,
	}
	ast.Walk(&contextAnalyzer{context: contextNotTgo, ctx: ctx}, f)
	ast.Walk(&tagPairsAnalyzer{ctx: ctx}, f)
	if len(ctx.errors) != 0 {
		return ctx.errors
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

type contextAnalyzer struct {
	context context
	ctx     *analyzerContext
}

func (f *contextAnalyzer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.FuncDecl:
		if len(n.Type.Params.List) == 0 {
			return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
		}
		return &contextAnalyzer{context: contextTgoBody, ctx: f.ctx}
	case *ast.FuncLit:
		if len(n.Type.Params.List) == 0 {
			return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
		}
		return &contextAnalyzer{context: contextTgoBody, ctx: f.ctx}
	case *ast.BlockStmt, *ast.IfStmt,
		*ast.SwitchStmt, *ast.CaseClause,
		*ast.ForStmt, *ast.SelectStmt,
		*ast.CommClause, *ast.RangeStmt,
		*ast.TypeSwitchStmt, *ast.ExprStmt:
		return f
	case *ast.TemplateLiteralExpr:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "template literal is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
	case *ast.OpenTagStmt:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "open tag is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextTgoTag, ctx: f.ctx}
	case *ast.EndTagStmt:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "end tag is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		return nil
	case *ast.AttributeStmt:
		if f.context != contextTgoTag {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "attribute is not allowed in this context",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End()),
			})
		}
		if v, ok := n.Value.(*ast.TemplateLiteralExpr); ok {
			a := &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
			for _, v := range v.Parts {
				ast.Walk(a, v)
			}
		}
		return nil
	default:
		return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
	}
}

type tagPairsAnalyzer struct {
	ctx *analyzerContext
}

func (f *tagPairsAnalyzer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.BlockStmt:
		f.checkTagPairs(n.List)
	case *ast.OpenTagStmt:
		f.checkTagPairs(n.Body)
	case *ast.CaseClause:
		f.checkTagPairs(n.Body)
	case *ast.CommClause:
		f.checkTagPairs(n.Body)
	}
	return f
}

func (f *tagPairsAnalyzer) checkTagPairs(stmt []ast.Stmt) {
	type namePos struct {
		name       string
		start, end token.Pos
	}
	deep := make([]namePos, 0, 16)

	for _, v := range stmt {
		switch n := v.(type) {
		case *ast.OpenTagStmt:
			// TODO(mateusz834): void elements
			deep = append(deep, namePos{
				name:  n.Name.Name,
				start: v.Pos(),
				end:   v.End() - 1,
			})
		case *ast.EndTagStmt:
			if len(deep) == 0 {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  "missing open tag",
					StartPos: f.ctx.fs.Position(n.OpenPos),
					EndPos:   f.ctx.fs.Position(n.ClosePos),
				})
				continue
			}
			last := deep[len(deep)-1]
			deep = deep[:len(deep)-1]
			if !strings.EqualFold(last.name, n.Name.Name) {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  fmt.Sprintf("unexpected close tag: %q, want: %q", n.Name.Name, last.name),
					StartPos: f.ctx.fs.Position(n.OpenPos),
					EndPos:   f.ctx.fs.Position(n.ClosePos),
				})
				continue
			}
		}
	}

	for _, v := range deep {
		f.ctx.errors = append(f.ctx.errors, AnalyzeError{
			Message:  "unclosed tag",
			StartPos: f.ctx.fs.Position(v.start),
			EndPos:   f.ctx.fs.Position(v.end),
		})
	}
}
