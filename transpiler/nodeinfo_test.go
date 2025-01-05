package transpiler

import (
	"fmt"
	goast "go/ast"
	"reflect"
	"strconv"
	"strings"

	"github.com/mateusz834/tgo/tgofuncs"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/token"
)

// TODO: add tests for this.

func expectedNodes(f *ast.File, fset *token.FileSet) map[nodeInfo]struct{} {
	info := tgofuncs.Check(f)
	tgoFuncs := make(map[ast.Node]struct{})
	for _, v := range info.TgoFuncs {
		tgoFuncs[v] = struct{}{}
	}

	ctx := &nodeInfoAnalyzerContext{
		fset:     fset,
		tgoFunc:  tgoFuncs,
		nodeInfo: make(map[nodeInfo]struct{}),
	}

	ast.Walk(&nodeInfoAnalyzer{ctx: ctx}, f)
	return ctx.nodeInfo
}

type nodeInfoAnalyzerContext struct {
	fset     *token.FileSet
	nodeInfo map[nodeInfo]struct{}
	tgoFunc  map[ast.Node]struct{}
}

type nodeInfoAnalyzer struct {
	ctx         *nodeInfoAnalyzerContext
	ignoreIdent *ast.Ident
	inTgo       bool
}

func (a *nodeInfoAnalyzer) Visit(n ast.Node) ast.Visitor {
	funcHandler := func(n ast.Node, ft *ast.FuncType) *nodeInfoAnalyzer {
		var ignore *ast.Ident
		_, isTgo := a.ctx.tgoFunc[n]

		params := ft.Params.List
		if isTgo && len(params) != 0 {
			if params[0].Names != nil && params[0].Names[0].Name == "_" {
				ignore = params[0].Names[0]
			}
		}

		return &nodeInfoAnalyzer{
			ctx:         a.ctx,
			ignoreIdent: ignore,
			inTgo:       isTgo,
		}
	}

	switch n := n.(type) {
	case *ast.FuncDecl:
		return funcHandler(n, n.Type)
	case *ast.FuncLit:
		return funcHandler(n, n.Type)
	case *ast.AttributeStmt:
		if _, ok := n.Value.(*ast.TemplateLiteralExpr); ok {
			ast.Walk(a, n.Value)
		}
		return nil
	case *ast.TemplateLiteralExpr,
		*ast.TemplateLiteralPart, *ast.File:
		return a
	case *ast.ElementBlockStmt:
		ast.Walk(a, n.OpenTag)
		for i, v := range n.Body {
			unlabeled, _ := unlabel(v)
			if v, ok := unlabeled.(*ast.EmptyStmt); i == len(n.Body)-1 && ok && v.Implicit {
				continue
			}
			ast.Walk(a, v)
		}
		ast.Walk(a, n.EndTag)
		return nil
	case *ast.OpenTag:
		for i, v := range n.Body {
			unlabeled, _ := unlabel(v)
			if v, ok := unlabeled.(*ast.EmptyStmt); i == len(n.Body)-1 && ok && v.Implicit {
				continue
			}
			ast.Walk(a, v)
		}
		return nil
	case *ast.EndTag:
		return nil
	case *ast.CommentGroup, *ast.Comment:
		return nil
	case *ast.ExprStmt:
		switch n := n.X.(type) {
		case *ast.TemplateLiteralExpr:
			return a
		case *ast.BasicLit:
			if a.inTgo && n.Kind == token.STRING {
				return nil
			}
		}
	case *ast.Ident:
		if a.ignoreIdent == n {
			return nil
		}
	case *ast.CommClause:
		return nil
	case *ast.CaseClause:
		return nil
	case nil:
		return nil
	}

	info := genNodeInfo[token.Token](n, func(p token.Pos) (line int, column int) {
		pos := a.ctx.fset.Position(p)
		return pos.Line, pos.Column
	})

	if _, ok := a.ctx.nodeInfo[info]; ok {
		panic("unreachable")
	}
	a.ctx.nodeInfo[info] = struct{}{}

	return a
}

type pos struct {
	line, column int
}

type nodeInfo struct {
	nodeName  string
	nodeStart pos
	nodeEnd   pos
	other     string
}

func genNodeInfo[TOK fmt.Stringer, POS interface{ IsValid() bool }](
	n interface {
		Pos() POS
		End() POS
	},
	posToLineCol func(pos POS) (line int, column int),
) nodeInfo {
	v := reflect.ValueOf(n).Elem()

	var info strings.Builder
	info.Grow(32)

	appendPos := func(name string, pos POS) {
		if info.Len() != 0 {
			info.WriteString(";")
		}
		line, column := posToLineCol(pos)
		info.WriteString(name)
		info.WriteString(":")
		info.WriteString(strconv.FormatInt(int64(line), 10))
		info.WriteString(":")
		info.WriteString(strconv.FormatInt(int64(column), 10))
	}

	for i := range v.NumField() {
		fv := v.Field(i)
		fieldName := v.Type().Field(i).Name
		if fv.Type() == reflect.TypeFor[POS]() {
			appendPos(fieldName, fv.Interface().(POS))
		} else if fv.Type() == reflect.TypeFor[string]() {
			if info.Len() != 0 {
				info.WriteString(";")
			}
			info.WriteString(fieldName)
			info.WriteString(":")
			info.WriteString(strconv.Quote(fv.String()))
		} else if fv.Type() == reflect.TypeFor[TOK]() {
			if info.Len() != 0 {
				info.WriteString(";")
			}
			info.WriteString(fieldName)
			info.WriteString(":")
			info.WriteString(fv.Interface().(TOK).String())
		} else if fv.Type() == reflect.TypeFor[bool]() {
			if info.Len() != 0 {
				info.WriteString(";")
			}
			info.WriteString(fieldName)
			info.WriteString(":")
			info.WriteString(strconv.FormatBool(fv.Interface().(bool)))
		}
	}

	startLine, startCol := posToLineCol(n.Pos())
	endLine, endCol := posToLineCol(n.End())
	switch any(n).(type) {
	case *ast.LabeledStmt, *goast.LabeledStmt:
		endLine, endCol = 0, 0
	}

	return nodeInfo{
		nodeName:  v.Type().Name(),
		nodeStart: pos{line: startLine, column: startCol},
		nodeEnd:   pos{line: endLine, column: endCol},
		other:     info.String(),
	}
}
