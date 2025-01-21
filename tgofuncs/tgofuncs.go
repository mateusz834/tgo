// tgofuncs package implements a really basic and primitive way of detecting tgo-funcs.
// We don't use the types package directly in the transpiler for few reasons:
//
//   - Performance, we want to be fast, using the types package would also most likely mean
//     that we would have to do some sort of caching to speed up the transpiler.
//
//   - We are a transpiler, meaning that we only really control changes in a single file (the transpiled one),
//     users can transpile a .tgo file modify (other, non-transpiled) .go files, thus possibly invalidaing (or changing the behaviour of
//     our transpiled file), nothing enforces them to run the transpiler again, after modifying .go files.
//
// Fortunetaly, each function declaration/literal always has to list all the types, that it uses, a tgo-func is a func that accepts
// a [tgo.Ctx] as a first argument (can have more than one argument, but the first must be a [tgo.Ctx]) and returns
// an [error]. To detect such functions we only need to check wheterh tgo package is imported and the "tgo" identifier is not shadowed,
// (e.g. by variables, ...) thus [tgo.Ctx] is then the thing that we were looking for.
//
// Obviously, because we are not doing a full type-checking, this has some drawbacks:
//
//   - No support for type aliases, such type:
//
//     type AliasedCtx = tgo.Ctx
//
//     is not going to be allowed as a first parameter in a tgo-func. Function containing AliasedCtx, is not going to be treated
//     as a tgo-func, even though in the type checker these two types are identical.
//     But also keeping in mind the second point (of the list, above), we can't really support aliases, alias can be in a different
//     file and users can freely change .go files, without running the transpiler.
package tgofuncs

import (
	"fmt"
	"maps"
	"strconv"

	"github.com/mateusz834/tgo/internal/astutil"
	"github.com/tgo-lang/lang/ast"
)

// TODO: describe issues and why they are fine:
// - we don't know the ident of a import stmt.

const (
	tgoModule      = "github.com/mateusz834/tgo"
	tgoPackageName = "tgo"
)

type ImportDetails struct {
	ImportIdent string
	DotImport   bool
}

type Info struct {
	TgoFuncs map[*ast.FuncType]struct{} // all tgo funcs

	// When != "", then the transpiler must add an additional import
	// with following identifier.
	SpecialTgoImportIdent string

	// UsableImportForTemplate contains every TemplateLiteralExpr found in a file with
	// a corresponding (non-shadowed) import to use.
	UsableImportForTemplate map[*ast.TemplateLiteralExpr]ImportDetails

	// UsableImportForTemplate contains every tgo-node found in a file, that
	// has the nil builtin shadowed, thus the transpiled code needs to use different
	// form of err != nil check.
	NeedsSpecialNilErrorCheck map[ast.Node]ImportDetails

	// UsableGlobalImport is a identifier of a tgo import that can be used at the global scope.
	// TODO: can it be a "shadowed"? Check it with go/types. This might be an issue with type asserts.
	UsableGlobalImport string
}

func Check(f *ast.File) Info {
	var (
		tgoImports   []string
		hasDotImport bool
	)

	for _, v := range f.Imports {
		path, err := strconv.Unquote(v.Path.Value)
		if err != nil {
			panic(err) // unreachable, AST is valid
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

	c := &contextAnalyzer{
		ctx: &contextAnalyzerContext{
			f:                         f,
			tgoImports:                tgoImports,
			tgoFuncs:                  map[*ast.FuncType]struct{}{},
			usableImportForTemplate:   make(map[*ast.TemplateLiteralExpr]ImportDetails),
			needsSpecialNilErrorCheck: make(map[ast.Node]ImportDetails),
			hasDotImport:              hasDotImport,
		},
	}

	for _, v := range f.Decls {
		switch v := v.(type) {
		case *ast.FuncDecl:
			c.shadowedImports.setShadowed(c, v.Name.Name)
		case *ast.GenDecl:
			for _, s := range v.Specs {
				switch s := s.(type) {
				case *ast.ImportSpec:
					path, err := strconv.Unquote(s.Path.Value)
					if err != nil {
						panic(err)
					}
					if s.Name != nil && path != tgoModule {
						c.shadowedImports.setShadowed(c, s.Name.Name)
					}
				case *ast.TypeSpec:
					c.shadowedImports.setShadowed(c, s.Name.Name)
				case *ast.ValueSpec:
					for _, v := range s.Names {
						c.shadowedImports.setShadowed(c, v.Name)
					}
				default:
					panic("unreachable")
				}
			}
		default:
			panic("unreachable")
		}
	}

	ast.Walk(c, f)
	usableGlobalImport := ""
	if len(tgoImports) != 0 {
		// TODO: can be shadowed, it might not matter.
		usableGlobalImport = tgoImports[0]
	}
	return Info{
		TgoFuncs:                  c.ctx.tgoFuncs,
		SpecialTgoImportIdent:     c.ctx.specialTgoImport,
		UsableImportForTemplate:   c.ctx.usableImportForTemplate,
		NeedsSpecialNilErrorCheck: c.ctx.needsSpecialNilErrorCheck,
		UsableGlobalImport:        usableGlobalImport,
	}
}

type contextAnalyzerContext struct {
	f *ast.File

	tgoFuncs                  map[*ast.FuncType]struct{}
	usableImportForTemplate   map[*ast.TemplateLiteralExpr]ImportDetails
	needsSpecialNilErrorCheck map[ast.Node]ImportDetails

	tgoImports   []string
	hasDotImport bool

	specialTgoImport string
}

func (c *contextAnalyzerContext) specialImportIdent() string {
	if c.specialTgoImport == "" {
		c.specialTgoImport = astutil.FileUniqueIdent(c.f, "__tgo")
	}
	return c.specialTgoImport
}

type contextAnalyzer struct {
	ctx             *contextAnalyzerContext
	shadowedImports bitField
}

func (f *contextAnalyzer) setNilUsableness(n ast.Node) {
	if f.shadowedImports.isSetBit(bitBuiltinNil) {
		if f.ctx.hasDotImport && !f.shadowedImports.isSetBit(bitTgoIsNotNil) {
			f.ctx.needsSpecialNilErrorCheck[n] = ImportDetails{DotImport: true}
		}
		for i, v := range f.ctx.tgoImports {
			if !f.shadowedImports.isSetImport(i) {
				f.ctx.needsSpecialNilErrorCheck[n] = ImportDetails{ImportIdent: v}
				break
			}
		}
		if _, ok := f.ctx.needsSpecialNilErrorCheck[n]; !ok {
			f.ctx.needsSpecialNilErrorCheck[n] = ImportDetails{ImportIdent: f.ctx.specialImportIdent()}
		}
	}
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
			ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Cond)
			ast.Walk(&contextAnalyzer{ctx: f.ctx, shadowedImports: s.clone()}, v.Body)
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
		if v.Name == "error" && !shadowedBefore.isSetBit(bitBuiltinError) {
			okReturn = true
		} else if f.ctx.hasDotImport && v.Name == "Error" && !shadowedBefore.isSetBit(bitTgoError) {
			okReturn = true
		}
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			for i, importName := range f.ctx.tgoImports {
				if ident.Name == importName && v.Sel.Name == "Error" && !shadowedBefore.isSetImport(i) {
					okReturn = true
					break
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
				if ident.Name == importName && v.Sel.Name == "Ctx" && !shadowedBefore.isSetImport(i) {
					tgoFunc = true
					return
				}
			}
		}
	case *ast.Ident:
		if f.ctx.hasDotImport && v.Name == "Ctx" && !shadowedBefore.isSetBit(bitTgoCtx) {
			tgoFunc = true
			return
		}
	}

	return
}

func (f *contextAnalyzer) Visit(list ast.Node) ast.Visitor {
	switch n := list.(type) {
	case *ast.ElementBlockStmt:
		f.setNilUsableness(n)
		ast.Walk(f, n.OpenTag)
		f.analyzeStmts(n.Body)
		ast.Walk(f, n.EndTag)
		return nil
	case *ast.OpenTag:
		f.setNilUsableness(n)
		f.analyzeStmts(n.Body)
		return nil
	case *ast.EndTag, *ast.AttributeStmt:
		f.setNilUsableness(n)
		return f
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
	case *ast.CommClause:
		// SelectStmt contains a BlockStmt, which contains CommClauses only,
		// they are already handled by analyzeStmts.
		panic("unreachable")
	case *ast.FuncDecl:
		tgo, shadowed := f.checkFuncType(orBitField(f.shadowedImports, f.checkFieldList(n.Recv)), n.Type)
		if tgo {
			f.ctx.tgoFuncs[n.Type] = struct{}{}
		}
		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: shadowed,
		}
	case *ast.FuncLit:
		tgo, shadowed := f.checkFuncType(f.shadowedImports, n.Type)
		if tgo {
			f.ctx.tgoFuncs[n.Type] = struct{}{}
		}
		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: shadowed,
		}
	case *ast.TemplateLiteralExpr:
		f.setNilUsableness(n)

		if f.ctx.hasDotImport && !f.shadowedImports.isSetBit(bitTgoDynamicWrite) {
			f.ctx.usableImportForTemplate[n] = ImportDetails{DotImport: true}
		}
		for i, v := range f.ctx.tgoImports {
			if !f.shadowedImports.isSetImport(i) {
				f.ctx.usableImportForTemplate[n] = ImportDetails{ImportIdent: v}
				break
			}
		}
		if _, ok := f.ctx.usableImportForTemplate[n]; !ok {
			f.ctx.usableImportForTemplate[n] = ImportDetails{ImportIdent: f.ctx.specialImportIdent()}
		}

		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: f.shadowedImports.clone(),
		}
	default:
		return &contextAnalyzer{
			ctx:             f.ctx,
			shadowedImports: f.shadowedImports.clone(),
		}
	}
}

const (
	bitTgoCtx = 63 - iota
	bitTgoError
	bitTgoIsNotNil
	bitTgoDynamicWrite
	bitBuiltinError
	bitBuiltinNil

	availBitsForImports
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

func (b *bitField) setImport(n int) {
	if n < availBitsForImports {
		b.bitField |= 1 << n
		return
	}
	if b.other == nil {
		b.other = map[int]struct{}{}
	}
	b.other[n] = struct{}{}
}

func (b bitField) isSetImport(n int) bool {
	if n < availBitsForImports {
		return b.bitField&(1<<n) != 0
	}
	_, ok := b.other[n]
	return ok
}

func (b *bitField) setBit(bit uint) {
	b.bitField |= 1 << bit
}

func (b bitField) isSetBit(bit uint) bool {
	return b.bitField&(1<<bit) != 0
}

func (b *bitField) setShadowed(c *contextAnalyzer, n string) {
	for i, v := range c.ctx.tgoImports {
		if v == n {
			b.setImport(i)
		}
	}

	switch n {
	case "Error":
		b.setBit(bitTgoError)
	case "DynamicWrite":
		b.setBit(bitTgoDynamicWrite)
	case "IsNotNil":
		b.setBit(bitTgoIsNotNil)
	case "Ctx":
		b.setBit(bitTgoCtx)
	case "error":
		b.setBit(bitBuiltinError)
	case "nil":
		b.setBit(bitBuiltinNil)
	}
}
