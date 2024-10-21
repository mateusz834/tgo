package analyzer

import (
	"fmt"
	"maps"
	"strconv"

	"github.com/mateusz834/tgoast/ast"
)

const (
	tgoModule      = "github.com/mateusz834/tgo"
	tgoPackageName = "tgo"
)

func checkContext(ctx *analyzerContext, f *ast.File) {
	tgoImports := []string{}
	for _, v := range f.Imports {
		path, err := strconv.Unquote(v.Path.Value)
		if err != nil {
			panic(err)
		}
		if path == tgoModule {
			ident := tgoPackageName
			if v.Name != nil {
				ident = v.Name.Name
			}
			if ident == "." {
				panic("oho, figure this out then :)")
			}
			tgoImports = append(tgoImports, ident)
		}
	}

	// TODO: we are only "type-checking" one file, describe
	// why it is safe to do, and why we went this way,
	// not depending on go/types, go/packages, perf
	// document that we do not support type aliases.
	// But we can fuzz agaisnt go/types :).

	ast.Walk(&contextAnalyzer{
		ctx: &contextAnalyzerContext{
			ctx:        ctx,
			tgoImports: tgoImports,
		},
		context: contextNotTgo,
	}, f)
}

type contextAnalyzerContext struct {
	ctx        *analyzerContext
	tgoImports []string
}

type context uint8

const (
	contextNotTgo context = iota
	contextTgoBody
	contextTgoTag
)

type contextAnalyzer struct {
	ctx             *contextAnalyzerContext
	shadowedImports bitField
	context         context
}

func (f *contextAnalyzer) simpleStmt(v ast.Stmt) (s bitField) {
	switch v := v.(type) {
	case *ast.AssignStmt:
		for _, v := range v.Lhs {
			if v, ok := v.(*ast.Ident); ok {
				f.setShadowed(&s, v.Name)
			}
		}
	case nil, *ast.IncDecStmt, *ast.ExprStmt, *ast.SendStmt:
	default:
		panic(fmt.Sprintf("unreachable %T", v))
	}
	return
}

func (f *contextAnalyzer) analyzeStmts(list []ast.Stmt) {
	shadowed := f.shadowedImports.clone()
	for _, v := range list {
		for {
			if l, ok := v.(*ast.LabeledStmt); ok {
				v = l.Stmt
				continue
			}
			break
		}

		switch v := v.(type) {
		case *ast.DeclStmt:
			d := v.Decl.(*ast.GenDecl)
			for _, v := range d.Specs {
				switch v := v.(type) {
				case *ast.ValueSpec:
					for _, v := range v.Names {
						f.setShadowed(&shadowed, v.Name)
					}
				case *ast.TypeSpec:
					f.setShadowed(&shadowed, v.Name.Name)
				default:
					panic("unreachable")
				}
			}
		case *ast.AssignStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: shadowed.clone(),
			}, v)
			shadowed = orBitField(shadowed, f.simpleStmt(v))
		case *ast.IfStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.SwitchStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.TypeSwitchStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.CommClause:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Comm)),
			}, v)
		case *ast.ForStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.RangeStmt:
			expr := func(x ast.Expr) (s bitField) {
				switch x := x.(type) {
				case *ast.Ident:
					f.setShadowed(&s, x.Name)
				}
				return
			}
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: orBitField(shadowed, expr(v.Key), expr(v.Value)),
			}, v)
		case *ast.LabeledStmt:
			panic("unreachable")
		default:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				context:         f.context,
				shadowedImports: shadowed.clone(),
			}, v)
		}
	}
}

func (f *contextAnalyzer) checkFieldList(fl *ast.FieldList) (s bitField) {
	if fl != nil {
		for _, v := range fl.List {
			for _, v := range v.Names {
				f.setShadowed(&s, v.Name)
			}
		}
	}
	return
}

func (f *contextAnalyzer) checkFuncType(shadowedImports bitField, ft *ast.FuncType) (tgoFunc bool, shadowedByFunc bitField) {
	shadowedBefore := orBitField(shadowedImports, f.checkFieldList(ft.TypeParams))
	shadowedByFunc = orBitField(shadowedBefore, f.checkFieldList(ft.Params), f.checkFieldList(ft.Results))
	if len(ft.Params.List) == 0 || ft.Results == nil || len(ft.Results.List) != 1 {
		return
	}

	okReturn := false
	switch v := ast.Unparen(ft.Results.List[0].Type).(type) {
	case *ast.Ident:
		if v.Name == "error" {
			okReturn = true
		}
	}

	switch v := ast.Unparen(ft.Params.List[0].Type).(type) {
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			for i, importName := range f.ctx.tgoImports {
				if ident.Name == importName && !shadowedBefore.isSet(i) {
					tgoFunc = okReturn && v.Sel.Name == "Ctx"
					return
				}
			}
		}
	}

	return
}

func (f *contextAnalyzer) Visit(list ast.Node) ast.Visitor {
	switch n := list.(type) {
	case *ast.BlockStmt:
		f.analyzeStmts(n.List)
		return nil
	case *ast.FuncDecl:
		tgo, shadowed := f.checkFuncType(orBitField(f.shadowedImports, f.checkFieldList(n.Recv)), n.Type)
		if tgo {
			return &contextAnalyzer{
				ctx:             f.ctx,
				context:         contextTgoBody,
				shadowedImports: shadowed,
			}
		}
		return &contextAnalyzer{
			ctx:             f.ctx,
			context:         contextNotTgo,
			shadowedImports: shadowed,
		}
	case *ast.FuncLit:
		tgo, shadowed := f.checkFuncType(f.shadowedImports, n.Type)
		if tgo {
			return &contextAnalyzer{
				ctx:             f.ctx,
				context:         contextTgoBody,
				shadowedImports: shadowed,
			}
		}
		return &contextAnalyzer{
			ctx:             f.ctx,
			context:         contextNotTgo,
			shadowedImports: shadowed,
		}
	case *ast.IfStmt,
		*ast.SwitchStmt, *ast.CaseClause,
		*ast.ForStmt, *ast.SelectStmt,
		*ast.CommClause, *ast.RangeStmt,
		*ast.TypeSwitchStmt, *ast.ExprStmt,
		*ast.LabeledStmt:
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
	case *ast.OpenTagStmt:
		if f.context != contextTgoBody {
			f.ctx.ctx.errors = append(f.ctx.ctx.errors, AnalyzeError{
				Message:  "open tag is not allowed in this context",
				StartPos: f.ctx.ctx.fset.Position(n.Pos()),
				EndPos:   f.ctx.ctx.fset.Position(n.End()),
			})
		}
		return &contextAnalyzer{context: contextTgoTag, ctx: f.ctx}
	case *ast.EndTagStmt:
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
			ctx:             f.ctx,
			context:         contextNotTgo,
			shadowedImports: orBitField(f.shadowedImports),
		}
	}
}

type bitField struct {
	other    map[int]struct{}
	bitField uint64
}

func (s bitField) clone() bitField {
	return bitField{
		other:    maps.Clone(s.other),
		bitField: s.bitField,
	}
}

func (s *bitField) set(n int) {
	if n < 63 {
		s.bitField |= 1 << n
		return
	}
	if s.other == nil {
		s.other = map[int]struct{}{}
	}
	s.other[n] = struct{}{}
}

func (s bitField) isSet(n int) bool {
	if n < 63 {
		return s.bitField&1<<n != 0
	}
	_, ok := s.other[n]
	return ok
}

func orBitField(o ...bitField) (out bitField) {
	for _, v := range o {
		out.bitField |= v.bitField
		for k := range v.other {
			if out.other == nil {
				out.other = make(map[int]struct{})
			}
			out.other[k] = struct{}{}
		}
	}
	return out
}

func (f *contextAnalyzer) setShadowed(s *bitField, n string) {
	for i, v := range f.ctx.tgoImports {
		if v == n {
			s.set(i)
		}
	}
}
