package tgofuncs

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

type Info struct {
	TgoFuncs []ast.Node // *ast.FuncDecl or *ast.FuncLit.
}

func Check(f *ast.File) Info {
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

	c := &contextAnalyzer{
		ctx: &contextAnalyzerContext{
			tgoImports: tgoImports,
		},
	}
	ast.Walk(c, f)
	return Info{
		TgoFuncs: c.ctx.tgoFuncs,
	}
}

type contextAnalyzerContext struct {
	tgoImports []string
	tgoFuncs   []ast.Node
}

type contextAnalyzer struct {
	ctx             *contextAnalyzerContext
	shadowedImports bitField
}

func (f *contextAnalyzer) simpleStmt(v ast.Stmt) (s bitField) {
	switch v := v.(type) {
	case *ast.AssignStmt:
		for _, v := range v.Lhs {
			if v, ok := v.(*ast.Ident); ok {
				s.setShadowed(f, v.Name)
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
						shadowed.setShadowed(f, v.Name)
					}
				case *ast.TypeSpec:
					shadowed.setShadowed(f, v.Name.Name)
				default:
					panic("unreachable")
				}
			}
		case *ast.AssignStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: shadowed.clone(),
			}, v)
			shadowed = orBitField(shadowed, f.simpleStmt(v))
		case *ast.IfStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.SwitchStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.TypeSwitchStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.CommClause:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Comm)),
			}, v)
		case *ast.ForStmt:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: orBitField(shadowed, f.simpleStmt(v.Init)),
			}, v)
		case *ast.RangeStmt:
			expr := func(x ast.Expr) (s bitField) {
				switch x := x.(type) {
				case *ast.Ident:
					s.setShadowed(f, x.Name)
				}
				return
			}
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: orBitField(shadowed, expr(v.Key), expr(v.Value)),
			}, v)
		case *ast.LabeledStmt:
			panic("unreachable")
		default:
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: shadowed.clone(),
			}, v)
		}
	}
}

func (f *contextAnalyzer) checkFieldList(fl *ast.FieldList) (s bitField) {
	if fl != nil {
		for _, v := range fl.List {
			for _, v := range v.Names {
				s.setShadowed(f, v.Name)
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
	case *ast.CaseClause:
		f.analyzeStmts(n.Body)
		return nil
	case *ast.CommClause:
		f.analyzeStmts(n.Body)
		return nil
	case *ast.OpenTagStmt:
		f.analyzeStmts(n.Body)
		return nil
	case *ast.FuncDecl:
		tgo, shadowed := f.checkFuncType(orBitField(f.shadowedImports, f.checkFieldList(n.Recv)), n.Type)
		if tgo {
			f.ctx.tgoFuncs = append(f.ctx.tgoFuncs, n)
		}
		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: shadowed,
		}
	case *ast.FuncLit:
		tgo, shadowed := f.checkFuncType(f.shadowedImports, n.Type)
		if tgo {
			f.ctx.tgoFuncs = append(f.ctx.tgoFuncs, n)
		}
		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: shadowed,
		}
	default:
		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: f.shadowedImports.clone(),
		}
	}
}

type bitField struct {
	other    map[int]struct{}
	bitField uint64
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

func (b bitField) clone() bitField {
	return bitField{
		other:    maps.Clone(b.other),
		bitField: b.bitField,
	}
}

func (b bitField) isSet(n int) bool {
	if n < 63 {
		return b.bitField&1<<n != 0
	}
	_, ok := b.other[n]
	return ok
}

func (b *bitField) set(n int) {
	if n < 63 {
		b.bitField |= 1 << n
		return
	}
	if b.other == nil {
		b.other = map[int]struct{}{}
	}
	b.other[n] = struct{}{}
}

func (b *bitField) setShadowed(c *contextAnalyzer, n string) {
	for i, v := range c.ctx.tgoImports {
		if v == n {
			b.set(i)
		}
	}
}
