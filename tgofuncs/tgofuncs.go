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
	var (
		tgoImports   []string
		hasDotImport bool
	)
	for _, v := range f.Imports {
		path, err := strconv.Unquote(v.Path.Value)
		if err != nil {
			panic(err)
		}
		if path == tgoModule {
			ident := tgoPackageName
			if v.Name != nil {
				if v.Name.Name == "." {
					hasDotImport = true
					continue
				} else if v.Name.Name == "_" {
					continue
				}
				ident = v.Name.Name
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
			tgoImports:   tgoImports,
			hasDotImport: hasDotImport,
		},
	}
	ast.Walk(c, f)
	return Info{
		TgoFuncs: c.ctx.tgoFuncs,
	}
}

type contextAnalyzerContext struct {
	tgoFuncs     []ast.Node
	tgoImports   []string
	hasDotImport bool
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
			if v.Init != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.Init)
			}
			s := orBitField(shadowed, f.simpleStmt(v.Init))
			if v.Cond != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Cond)
			}
			if v.Body != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Body)
			}
			if v.Else != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Else)
			}
		case *ast.SwitchStmt:
			if v.Init != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.Init)
			}
			s := orBitField(shadowed, f.simpleStmt(v.Init))
			ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Body)
			if v.Tag != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Tag)
			}
		case *ast.TypeSwitchStmt:
			if v.Init != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.Init)
			}
			ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.Assign)
			s := orBitField(shadowed, f.simpleStmt(v.Init), f.simpleStmt(v.Assign))
			if v.Body != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Body)
			}
		case *ast.CommClause:
			if v.Comm != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.Comm)
			}
			c := &contextAnalyzer{ctx: f.ctx, shadowedImports: orBitField(shadowed, f.simpleStmt(v.Comm))}
			c.analyzeStmts(v.Body)
		case *ast.ForStmt:
			if v.Init != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.Init)
			}
			s := orBitField(shadowed, f.simpleStmt(v.Init))
			if v.Cond != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Cond)
			}
			if v.Post != nil {
				ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Post)
			}
			ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Body)
		case *ast.RangeStmt:
			ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: shadowed.clone()}, v.X)
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
			}, v.Body)
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
		if v.Name == "error" && !shadowedBefore.isSetError() {
			okReturn = true
		} else if f.ctx.hasDotImport && v.Name == "Error" && !shadowedBefore.isSetTgoError() {
			okReturn = true
		}
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			for i, importName := range f.ctx.tgoImports {
				if ident.Name == importName && !shadowedBefore.isSet(i) {
					okReturn = v.Sel.Name == "Error"
				}
			}
		}
	}

	if !okReturn {
		return
	}

	switch v := ast.Unparen(ft.Params.List[0].Type).(type) {
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			for i, importName := range f.ctx.tgoImports {
				if ident.Name == importName && !shadowedBefore.isSet(i) {
					tgoFunc = v.Sel.Name == "Ctx"
					return
				}
			}
		}
	case *ast.Ident:
		if f.ctx.hasDotImport && v.Name == "Ctx" && !shadowedBefore.isSetTgoCtx() {
			tgoFunc = true
			return
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
		for _, v := range n.List {
			ast.Walk(&contextAnalyzer{
				ctx:             f.ctx,
				shadowedImports: f.shadowedImports.clone(),
			}, v)
		}
		f.analyzeStmts(n.Body)
		return nil
	case *ast.OpenTagStmt:
		f.analyzeStmts(n.Body)
		return nil
	case *ast.CommClause:
		// SelectStmt contains a BlockStmt, which contains CommClauses only,
		// they are already handled by analyzeStmts.
		panic("unreachable")
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

const (
	bitTgoCtx   = 63
	bitTgoError = 62
	bitError    = 61

	bitsForImports = 60
)

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
	if n < bitsForImports {
		return b.bitField&1<<n != 0
	}
	_, ok := b.other[n]
	return ok
}

func (b *bitField) set(n int) {
	if n < bitsForImports {
		b.bitField |= 1 << n
		return
	}
	if b.other == nil {
		b.other = map[int]struct{}{}
	}
	b.other[n] = struct{}{}
}

func (b bitField) isSetTgoCtx() bool {
	return b.bitField&bitTgoCtx != 0
}

func (b bitField) isSetTgoError() bool {
	return b.bitField&bitTgoError != 0
}

func (b bitField) isSetError() bool {
	return b.bitField&bitError != 0
}

func (b bitField) setTgoCtx() {
	b.bitField |= bitTgoCtx
}

func (b *bitField) setTgoError() {
	b.bitField |= bitTgoError
}

func (b *bitField) setError() {
	b.bitField |= bitError
}

func (b *bitField) setShadowed(c *contextAnalyzer, n string) {
	for i, v := range c.ctx.tgoImports {
		if v == n {
			b.set(i)
		}
	}
	switch n {
	case "Error":
		b.setTgoError()
	case "Ctx":
		b.setTgoCtx()
	case "error":
		b.setError()
	}
}
