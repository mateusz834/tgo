package transpiler

import (
	"cmp"
	"fmt"
	goast "go/ast"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mateusz834/tgo/tgofuncs"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/parser"
	"github.com/tgo-lang/lang/token"
)

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
	posToLineCol func(pos POS) token.Position,
) nodeInfo {
	v := reflect.ValueOf(n).Elem()

	var info strings.Builder
	info.Grow(32)

	appendPos := func(name string, pos POS) {
		if info.Len() != 0 {
			info.WriteString(";")
		}
		p := posToLineCol(pos)
		info.WriteString(name)
		info.WriteString(":")
		info.WriteString(strconv.FormatInt(int64(p.Line), 10))
		info.WriteString(":")
		info.WriteString(strconv.FormatInt(int64(p.Column), 10))
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

	start := posToLineCol(n.Pos())
	end := posToLineCol(n.End())
	switch any(n).(type) {
	case *ast.LabeledStmt, *goast.LabeledStmt,
		*ast.CommClause, *goast.CommClause,
		*ast.CaseClause, *goast.CaseClause:
		end.Line, end.Column = -1, -1
	}

	return nodeInfo{
		nodeName:  v.Type().Name(),
		nodeStart: pos{line: start.Line, column: start.Column},
		nodeEnd:   pos{line: end.Line, column: end.Column},
		other:     info.String(),
	}
}

func tgoExpectedNodeInfos(f *ast.File, fset *token.FileSet) map[nodeInfo]struct{} {
	info, _ := tgofuncs.Check(f)

	ctx := &nodeInfoAnalyzerContext{
		fset:     fset,
		tgoFunc:  info.TgoFuncs,
		nodeInfo: make(map[nodeInfo]struct{}),
	}

	ast.Walk(&nodeInfoAnalyzer{ctx: ctx}, f)
	return ctx.nodeInfo
}

type nodeInfoAnalyzerContext struct {
	fset     *token.FileSet
	nodeInfo map[nodeInfo]struct{}
	tgoFunc  map[*ast.FuncType]struct{}
}

type nodeInfoAnalyzer struct {
	ctx                  *nodeInfoAnalyzerContext
	tgoFuncBlankCtxIdent *ast.Ident
	inTgo                bool
}

func (a *nodeInfoAnalyzer) Visit(n ast.Node) ast.Visitor {
	funcHandler := func(ft *ast.FuncType) *nodeInfoAnalyzer {
		var tgoFuncBlankCtxIdent *ast.Ident
		_, isTgo := a.ctx.tgoFunc[ft]
		if isTgo {
			names := ft.Params.List[0].Names
			if names != nil && names[0].Name == "_" {
				tgoFuncBlankCtxIdent = names[0]
			}
		}

		return &nodeInfoAnalyzer{
			ctx:                  a.ctx,
			tgoFuncBlankCtxIdent: tgoFuncBlankCtxIdent,
			inTgo:                isTgo,
		}
	}

	switch n := n.(type) {
	case *ast.FuncDecl:
		return funcHandler(n.Type)
	case *ast.FuncLit:
		return funcHandler(n.Type)
	case *ast.Attribute:
		if _, ok := n.Value.(*ast.TemplateLiteral); ok {
			ast.Walk(a, n.Value)
		}
		return nil
	case *ast.Element:
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
	case *ast.TemplateLiteral, *ast.Text:
		return nil
	case *ast.Ident:
		if a.tgoFuncBlankCtxIdent == n {
			return nil
		}
	case *ast.File, *ast.TemplateLiteralPart:
		return a
	case *ast.EndTag, *ast.CommentGroup, *ast.Comment, nil:
		return nil
	}

	info := genNodeInfo[token.Token](n, a.ctx.fset.Position)
	if _, ok := a.ctx.nodeInfo[info]; ok {
		panic("unreachable")
	}
	a.ctx.nodeInfo[info] = struct{}{}

	return a
}

func TestTgoExpectedNodeInfos(t *testing.T) {
	const testdata = "./testdata/nodeinfos"
	files, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range files {
		if v.IsDir() {
			continue
		}
		t.Run(v.Name(), func(t *testing.T) {
			file := filepath.Join(testdata, v.Name())
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			tgo, nodeinfos, _ := strings.Cut(string(content), "======\n")

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.tgo", tgo, parser.ParseComments|parser.SkipObjectResolution|parser.ParseTgo)
			if err != nil {
				t.Fatal(err)
			}

			nis := slices.SortedFunc(maps.Keys(tgoExpectedNodeInfos(f, fset)), func(a, b nodeInfo) int {
				return cmp.Or(
					cmp.Compare(a.nodeStart.line, b.nodeStart.line),
					cmp.Compare(a.nodeStart.column, b.nodeStart.column),
					cmp.Compare(a.nodeEnd.line, b.nodeEnd.line),
					cmp.Compare(a.nodeEnd.column, b.nodeEnd.column),
					cmp.Compare(a.nodeName, b.nodeName),
					cmp.Compare(a.other, b.other),
				)
			})

			var s strings.Builder
			for _, ni := range nis {
				s.WriteString(fmt.Sprintf("%v\n", ni))
			}

			if *update {
				nodeinfos = s.String()
				if err := os.WriteFile(file, []byte(tgo+"======\n"+nodeinfos), 0660); err != nil {
					t.Fatal(err)
				}
			}

			if nodeinfos != s.String() {
				t.Logf("got nodeinfos:\n%s", s.String())
				t.Logf("want nodeinfos:\n%s", nodeinfos)
				t.Log(gitDiff(t.TempDir(), s.String(), nodeinfos))
			}
		})
	}
}
