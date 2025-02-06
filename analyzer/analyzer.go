package analyzer

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mateusz834/tgo/tgofuncs"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/token"
)

func Analyze(fset *token.FileSet, f *ast.File) (tgofuncs.Info, error) {
	ctx := &analyzerContext{
		fset: fset,
	}
	info := checkContext(ctx, f)
	if len(ctx.errors) == 0 {
		ast.Walk(&branchAnalyzer{
			ctx: &branchAnalyzerContext{
				ctx:         ctx,
				labelScopes: labelScopes(f),
			},
		}, f)
	}
	checkDirectives(ctx, f)
	if len(ctx.errors) != 0 {
		return tgofuncs.Info{}, ctx.errors
	}
	return info, nil
}

type AnalyzeError struct {
	Message string
	// TODO: remove EndPos, no need for it.
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

func unlabel(v ast.Node) ast.Node {
	for {
		if n, ok := v.(*ast.LabeledStmt); ok {
			v = n.Stmt
			continue
		}
		return v
	}
}

func checkContext(ctx *analyzerContext, f *ast.File) tgofuncs.Info {
	// TODO: we are only "type-checking" one file, describe
	// why it is safe to do, and why we went this way,
	// not depending on go/types, go/packages, perf
	// document that we do not support type aliases.
	// But we can fuzz agaisnt go/types :).

	info, err := tgofuncs.Check(f)
	if err != nil {
		for _, v := range err.(tgofuncs.Errors) {
			ctx.errors = append(ctx.errors, AnalyzeError{
				Message:  v.Msg,
				StartPos: ctx.fset.Position(v.Pos),
			})
		}
	}
	c := &contextAnalyzer{
		ctx: &contextAnalyzerContext{
			ctx:      ctx,
			tgoFuncs: info.TgoFuncs,
		},
		context: contextNotTgo,
	}
	ast.Walk(c, f)

	return info
}

type contextAnalyzerContext struct {
	ctx      *analyzerContext
	tgoFuncs map[*ast.FuncType]struct{}
}

type context uint8

const (
	contextNotTgo context = iota
	contextTgoBody
	contextTgoTag
)

type contextAnalyzer struct {
	ctx     *contextAnalyzerContext
	context context
}

func (f *contextAnalyzer) Visit(list ast.Node) ast.Visitor {
	switch n := list.(type) {
	case *ast.FuncDecl:
		c := &contextAnalyzer{
			ctx:     f.ctx,
			context: contextNotTgo,
		}
		if _, ok := f.ctx.tgoFuncs[n.Type]; ok {
			c.context = contextTgoBody
		}
		return c
	case *ast.FuncLit:
		c := &contextAnalyzer{
			ctx:     f.ctx,
			context: contextNotTgo,
		}
		if _, ok := f.ctx.tgoFuncs[n.Type]; ok {
			c.context = contextTgoBody
		}
		return c
	case *ast.IfStmt,
		*ast.SwitchStmt, *ast.CaseClause,
		*ast.ForStmt, *ast.SelectStmt,
		*ast.CommClause, *ast.RangeStmt,
		*ast.TypeSwitchStmt, *ast.LabeledStmt,
		*ast.BlockStmt:
		return f
	case *ast.ExprStmt:
		if x, ok := n.X.(*ast.BasicLit); ok && x.Kind == token.STRING {
			if f.context != contextTgoBody {
				f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
					Message:  "string basic literal is not allowed in this context",
					StartPos: f.ctx.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.ctx.fset.Position(n.End()),
				})
			}
		}
		return f
	case *ast.TemplateLiteralExpr:
		if f.context != contextTgoBody {
			f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
				Message:  "template literal is not allowed in this context",
				StartPos: f.ctx.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.ctx.fset.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextNotTgo, ctx: f.ctx}
	case *ast.ElementBlockStmt:
		return f
	case *ast.OpenTag:
		if f.context != contextTgoBody {
			f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
				Message:  "open tag is not allowed in this context",
				StartPos: f.ctx.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.ctx.fset.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextTgoTag, ctx: f.ctx}
	case *ast.EndTag:
		if f.context != contextTgoBody {
			f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
				Message:  "end tag is not allowed in this context",
				StartPos: f.ctx.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.ctx.fset.Position(n.End()),
			})
		}
		return nil
	case *ast.AttributeStmt:
		if f.context != contextTgoTag {
			f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
				Message:  "attribute is not allowed in this context",
				StartPos: f.ctx.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.ctx.fset.Position(n.End()),
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
		return &contextAnalyzer{
			ctx:     f.ctx,
			context: contextNotTgo,
		}
	}
}

type scope struct {
	f   ast.Node // *ast.FuncDecl or *ast.FuncLit
	tag *ast.ElementBlockStmt
}

type labelScopeAnalyzer struct {
	out map[scope][]string
	cur scope
}

func (f *labelScopeAnalyzer) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.FuncDecl, *ast.FuncLit:
		return &labelScopeAnalyzer{out: f.out, cur: scope{f: n}}
	case *ast.ElementBlockStmt:
		return &labelScopeAnalyzer{out: f.out, cur: scope{f: f.cur.f, tag: n}}
	case *ast.LabeledStmt:
		f.out[f.cur] = append(f.out[f.cur], n.Label.Name)
	}
	return f
}

func labelScopes(f *ast.File) map[scope][]string {
	out := make(map[scope][]string)
	ast.Walk(&labelScopeAnalyzer{out: out}, f)
	return out
}

type branchAnalyzerContext struct {
	ctx         *analyzerContext
	labelScopes map[scope][]string
}

type branchAnalyzer struct {
	ctx             *branchAnalyzerContext
	breakDepth      int
	continueDepth   int
	labeledDepth    map[string]int
	curElementBlock *ast.ElementBlockStmt
	f               ast.Node // *ast.FuncDecl or *ast.FuncLit
}

// TODO: labels inside of open tag.

func (f *branchAnalyzer) Visit(node ast.Node) ast.Visitor {
	//switch unlabeled := unlabel(node); unlabeled.(type) {
	//case *ast.OpenTagStmt, *ast.EndTagStmt:
	//	if unlabeled != node {
	//		ast.Walk(f, unlabeled)
	//		return nil
	//	}
	//}

	switch n := node.(type) {
	case *ast.FuncDecl, *ast.FuncLit:
		return &branchAnalyzer{ctx: f.ctx, f: n} // reset depths
	case *ast.ForStmt, *ast.RangeStmt:
		return &branchAnalyzer{
			ctx:             f.ctx,
			breakDepth:      0,
			continueDepth:   0,
			labeledDepth:    maps.Clone(f.labeledDepth),
			curElementBlock: f.curElementBlock,
			f:               f.f,
		}
	case *ast.SwitchStmt, *ast.SelectStmt, *ast.TypeSwitchStmt:
		return &branchAnalyzer{
			ctx:             f.ctx,
			breakDepth:      0,
			continueDepth:   f.continueDepth,
			labeledDepth:    maps.Clone(f.labeledDepth),
			curElementBlock: f.curElementBlock,
			f:               f.f,
		}
	case *ast.LabeledStmt:
		b := &branchAnalyzer{
			ctx:             f.ctx,
			breakDepth:      0,
			continueDepth:   f.continueDepth,
			labeledDepth:    maps.Clone(f.labeledDepth),
			curElementBlock: f.curElementBlock,
			f:               f.f,
		}
		if b.labeledDepth == nil {
			b.labeledDepth = make(map[string]int)
		}
		b.labeledDepth[n.Label.Name] = 0
		return b
	case *ast.ElementBlockStmt:
		return &branchAnalyzer{
			ctx:             f.ctx,
			breakDepth:      0,
			continueDepth:   f.continueDepth,
			labeledDepth:    maps.Clone(f.labeledDepth),
			curElementBlock: n,
			f:               f.f,
		}
	case *ast.OpenTag:
		f.continueDepth++
		f.breakDepth++
		for k := range f.labeledDepth {
			f.labeledDepth[k]++
		}
	case *ast.EndTag:
		if f.continueDepth == 0 || f.breakDepth == 0 {
			panic("unreachable")
		}
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
				f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
					Message:  "unexpected break statement in the middle of a tag body, ensure that all open tags are closed",
					StartPos: f.ctx.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.ctx.fset.Position(n.End() - 1),
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
				f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
					Message:  "unexpected continue statement in the middle of a tag body, ensure that all open tags are closed",
					StartPos: f.ctx.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.ctx.fset.Position(n.End() - 1),
				})
			}
		case token.GOTO:
			s := scope{f: f.f, tag: f.curElementBlock}
			if n.Label != nil && !slices.Contains(f.ctx.labelScopes[s], n.Label.Name) {
				f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
					Message:  "unexpected goto statement, ensure that all tags are closed at the goto and the jump locaton",
					StartPos: f.ctx.ctx.fset.Position(n.Pos()),
					EndPos:   f.ctx.ctx.fset.Position(n.End() - 1),
				})
			}
		case token.FALLTHROUGH:
			// ignore, fallthrough, as it can only be as the last statement.
		default:
			panic("unreachable")
		}
	case *ast.ReturnStmt:
		if f.curElementBlock != nil {
			f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
				Message:  "unexpected return statement in the middle of a tag body, ensure that all open tags are closed",
				StartPos: f.ctx.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.ctx.fset.Position(n.End() - 1),
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
