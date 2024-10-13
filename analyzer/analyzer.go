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
	if len(ctx.errors) == 0 {
		ast.Walk(&branchAnalyzer{ctx: ctx}, f)
	}
	checkDirectives(ctx, f)
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
		*ast.TypeSwitchStmt, *ast.ExprStmt,
		*ast.LabeledStmt:
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

type branchAnalyzer struct {
	ctx           *analyzerContext
	depth         int
	breakDepth    int
	continueDepth int
}

func (f *branchAnalyzer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.FuncDecl, *ast.FuncLit:
		return &branchAnalyzer{ctx: f.ctx} // reset depths
	case *ast.ForStmt, *ast.RangeStmt:
		return &branchAnalyzer{
			ctx:           f.ctx,
			depth:         f.depth,
			breakDepth:    0,
			continueDepth: 0,
		}
	case *ast.SwitchStmt, *ast.SelectStmt, *ast.TypeSwitchStmt:
		return &branchAnalyzer{
			ctx:           f.ctx,
			depth:         f.depth,
			breakDepth:    0,
			continueDepth: f.continueDepth,
		}
	case *ast.OpenTagStmt:
		// TODO(mateusz834): void elements
		f.depth++
		f.continueDepth++
		f.breakDepth++
	case *ast.EndTagStmt:
		if f.depth == 0 || f.continueDepth == 0 || f.breakDepth == 0 {
			panic("unreachable")
		}
		f.depth--
		f.continueDepth--
		f.breakDepth--
	case *ast.BranchStmt:
		if n.Label != nil {
			panic("this is not going to be easy")
		}
		switch n.Tok {
		case token.BREAK:
			if f.breakDepth != 0 {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  "unexpected break statement",
					StartPos: f.ctx.fs.Position(n.Pos()),
					EndPos:   f.ctx.fs.Position(n.End() - 1),
				})
			}
		case token.CONTINUE:
			if f.continueDepth != 0 {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  "unexpected continue statement",
					StartPos: f.ctx.fs.Position(n.Pos()),
					EndPos:   f.ctx.fs.Position(n.End() - 1),
				})
			}
		case token.GOTO:
			panic("figure out")
		case token.FALLTHROUGH:
			panic("figure out")
		default:
			panic("unreachable")
		}
	case *ast.ReturnStmt:
		if f.depth != 0 {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "unexpected return statement",
				StartPos: f.ctx.fs.Position(n.Pos()),
				EndPos:   f.ctx.fs.Position(n.End() - 1),
			})
		}
	}
	return f
}

func checkDirectives(ctx *analyzerContext, f *ast.File) {
	for _, v := range f.Comments {
		for _, v := range v.List {
			// TODO: better detection (it has to start directly after newline and require a file)?
			if strings.HasPrefix(v.Text[2:], "line") {
				ctx.errors = append(ctx.errors, AnalyzeError{
					Message:  "line directive is not allowed inside of the tgo file",
					StartPos: ctx.fs.Position(v.Pos()),
					EndPos:   ctx.fs.Position(v.End()),
				})
			}
		}
	}
}
