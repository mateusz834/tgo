package analyzer

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

func Analyze(fset *token.FileSet, f *ast.File) error {
	ctx := &analyzerContext{
		fset: fset,
	}
	ast.Walk(&tagPairsAnalyzer{ctx: ctx}, f)
	checkContext(ctx, f)
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
	fset   *token.FileSet
}

type context uint8

const (
	contextNotTgo context = iota
	contextTgoBody
	contextTgoTag
)

func checkContext(ctx *analyzerContext, f *ast.File) {
	ident := "tgo"
	tgoImported := false
	for _, v := range f.Imports {
		path, err := strconv.Unquote(v.Path.Value)
		if err != nil {
			panic(err)
		}
		if path == "github.com/mateusz834/tgo" {
			tgoImported = true
			if v.Name != nil {
				ident = v.Name.Name
			}
		}
	}

	if ident == "." {
		panic("oho, figure this out then :)")
	}

	ast.Walk(&contextAnalyzer{
		ctx:         ctx,
		context:     contextNotTgo,
		ident:       ident,
		tgoImported: tgoImported,
	}, f)
}

type contextAnalyzer struct {
	ctx *analyzerContext

	context     context
	ident       string
	tgoImported bool
	exists      bool
}

func (f *contextAnalyzer) simpleStmt(v ast.Stmt) bool {
	if v, ok := v.(*ast.AssignStmt); ok {
		for _, v := range v.Lhs {
			if v, ok := v.(*ast.Ident); ok {
				if v.Name == f.ident {
					return true
				}
			}
		}
	}
	return false
}

func (f *contextAnalyzer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.BlockStmt:
		exists := false
		for _, v := range n.List {
			if l, ok := v.(*ast.LabeledStmt); ok {
				v = l.Stmt
			}

			switch v := v.(type) {
			case *ast.DeclStmt:
				d := v.Decl.(*ast.GenDecl)
				for _, v := range d.Specs {
					switch v := v.(type) {
					case *ast.ValueSpec:
						for _, v := range v.Names {
							if v.Name == f.ident {
								exists = true
								break
							}
						}
					case *ast.TypeSpec:
						if v.Name.Name == f.ident {
							exists = true
						}
					default:
						panic("unreachable")
					}
				}
			case *ast.AssignStmt:
				for _, v := range v.Lhs {
					if v, ok := v.(*ast.Ident); ok {
						if v.Name == f.ident {
							exists = true
							break
						}
					}
				}
			case *ast.IfStmt:
				ast.Walk(&contextAnalyzer{
					ctx:         f.ctx,
					context:     f.context,
					ident:       f.ident,
					tgoImported: f.tgoImported,
					exists:      exists || f.exists || f.simpleStmt(v.Init),
				}, v)
				return nil
			case *ast.SwitchStmt:
				ast.Walk(&contextAnalyzer{
					ctx:         f.ctx,
					context:     f.context,
					ident:       f.ident,
					tgoImported: f.tgoImported,
					exists:      exists || f.exists || f.simpleStmt(v.Init),
				}, v)
				return nil
			case *ast.TypeSwitchStmt:
				ast.Walk(&contextAnalyzer{
					ctx:         f.ctx,
					context:     f.context,
					ident:       f.ident,
					tgoImported: f.tgoImported,
					exists:      exists || f.exists || f.simpleStmt(v.Init),
				}, v)
				return nil
			case *ast.CommClause:
				ast.Walk(&contextAnalyzer{
					ctx:         f.ctx,
					context:     f.context,
					ident:       f.ident,
					tgoImported: f.tgoImported,
					exists:      exists || f.exists || f.simpleStmt(v.Comm),
				}, v)
				return nil
			case *ast.ForStmt:
				ast.Walk(&contextAnalyzer{
					ctx:         f.ctx,
					context:     f.context,
					ident:       f.ident,
					tgoImported: f.tgoImported,
					exists:      exists || f.exists || f.simpleStmt(v.Init),
				}, v)
				return nil
			case *ast.RangeStmt:
				panic("TODO")
			case *ast.LabeledStmt:
				panic("unreachable")
			default:
				ast.Walk(&contextAnalyzer{
					ctx:         f.ctx,
					context:     f.context,
					ident:       f.ident,
					tgoImported: f.tgoImported,
					exists:      exists || f.exists,
				}, v)
			}

		}
		return nil
	case *ast.FuncDecl:
		if f.exists {
			return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
		}
		return &contextAnalyzer{context: contextTgoBody, ctx: f.ctx}
	case *ast.FuncLit:
		if f.exists {
			return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
		}
		return &contextAnalyzer{context: contextTgoBody, ctx: f.ctx}
	case *ast.IfStmt,
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
				StartPos: f.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.fset.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
	case *ast.OpenTagStmt:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "open tag is not allowed in this context",
				StartPos: f.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.fset.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextTgoTag, ctx: f.ctx}
	case *ast.EndTagStmt:
		if f.context != contextTgoBody {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "end tag is not allowed in this context",
				StartPos: f.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.fset.Position(n.End()),
			})
		}
		return nil
	case *ast.AttributeStmt:
		if f.context != contextTgoTag {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "attribute is not allowed in this context",
				StartPos: f.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.fset.Position(n.End()),
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
					StartPos: f.ctx.fset.Position(n.OpenPos),
					EndPos:   f.ctx.fset.Position(n.ClosePos),
				})
				continue
			}
			last := deep[len(deep)-1]
			deep = deep[:len(deep)-1]
			if !strings.EqualFold(last.name, n.Name.Name) {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  fmt.Sprintf("unexpected close tag: %q, want: %q", n.Name.Name, last.name),
					StartPos: f.ctx.fset.Position(n.OpenPos),
					EndPos:   f.ctx.fset.Position(n.ClosePos),
				})
			}
		}
	}

	for _, v := range deep {
		f.ctx.errors = append(f.ctx.errors, AnalyzeError{
			Message:  "unclosed tag",
			StartPos: f.ctx.fset.Position(v.start),
			EndPos:   f.ctx.fset.Position(v.end),
		})
	}
}

type branchAnalyzer struct {
	ctx           *analyzerContext
	depth         int
	breakDepth    int
	continueDepth int
	labeledDepth  map[string]int
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
			labeledDepth:  maps.Clone(f.labeledDepth),
		}
	case *ast.SwitchStmt, *ast.SelectStmt, *ast.TypeSwitchStmt:
		return &branchAnalyzer{
			ctx:           f.ctx,
			depth:         f.depth,
			breakDepth:    0,
			continueDepth: f.continueDepth,
			labeledDepth:  maps.Clone(f.labeledDepth),
		}
	case *ast.LabeledStmt:
		b := &branchAnalyzer{
			ctx:           f.ctx,
			depth:         f.depth,
			breakDepth:    0,
			continueDepth: f.continueDepth,
			labeledDepth:  maps.Clone(f.labeledDepth),
		}
		if b.labeledDepth == nil {
			b.labeledDepth = make(map[string]int)
		}
		b.labeledDepth[n.Label.Name] = 0
		return b
	case *ast.OpenTagStmt:
		// TODO(mateusz834): void elements
		f.depth++
		f.continueDepth++
		f.breakDepth++
		for k := range f.labeledDepth {
			f.labeledDepth[k]++
		}
	case *ast.EndTagStmt:
		if f.depth == 0 || f.continueDepth == 0 || f.breakDepth == 0 {
			panic("unreachable")
		}
		// TODO(mateusz834): void elements
		f.depth--
		f.continueDepth--
		f.breakDepth--
		for k, v := range f.labeledDepth {
			if v == 0 {
				panic("unreachable")
			}
			f.labeledDepth[k]--
		}
	case *ast.BranchStmt:
		switch n.Tok {
		case token.BREAK:
			depth := f.breakDepth
			if n.Label != nil {
				if d, ok := f.labeledDepth[n.Label.Name]; ok {
					depth = d
				}
			}
			if depth != 0 {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  "unexpected break statement in the middle of a tag body, ensure that all open tags are closed",
					StartPos: f.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.fset.Position(n.End() - 1),
				})
			}
		case token.CONTINUE:
			depth := f.continueDepth
			if n.Label != nil {
				if d, ok := f.labeledDepth[n.Label.Name]; ok {
					depth = d
				}
			}
			if depth != 0 {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  "unexpected continue statement in the middle of a tag body, ensure that all open tags are closed",
					StartPos: f.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.fset.Position(n.End() - 1),
				})
			}
		case token.GOTO:
			// TODO: can we make it better? Who even uses gotos.
			// TODO: we can jump to already openned div :) FIX
			if f.depth != 0 {
				f.ctx.errors = append(f.ctx.errors, AnalyzeError{
					Message:  "unexpected goto statement in the middle of a tag body, ensure that all open tags are closed",
					StartPos: f.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.fset.Position(n.End() - 1),
				})
			}
		case token.FALLTHROUGH:
			// ignore, fallthrough, as it can only be as the last statement.
		default:
			panic("unreachable")
		}
	case *ast.ReturnStmt:
		if f.depth != 0 {
			f.ctx.errors = append(f.ctx.errors, AnalyzeError{
				Message:  "unexpected return statement in the middle of a tag body, ensure that all open tags are closed",
				StartPos: f.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.fset.Position(n.End() - 1),
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
					StartPos: ctx.fset.Position(v.Pos()),
					EndPos:   ctx.fset.Position(v.End()),
				})
			}
		}
	}
}
