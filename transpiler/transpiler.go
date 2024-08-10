package transpiler

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

const transpilerDebug = true

func Transpile(f *ast.File, fs *token.FileSet, src string) string {
	t := transpiler{
		f:   f,
		fs:  fs,
		src: src,

		lineDirectiveMangled: true,
	}
	t.out = slices.Grow([]byte{}, len(src)*2)
	t.transpile()
	return string(t.out)
}

type transpiler struct {
	f   *ast.File
	fs  *token.FileSet
	src string
	out []byte

	lastPosWritten token.Pos

	lineDirectiveMangled bool
	lastIndentMangled    string
}

func (t *transpiler) lastIndent() string {
	if t.lastIndentMangled != "" {
		n := t.lastIndentMangled
		t.lastIndentMangled = ""
		return n
	}

	i := max(bytes.LastIndexByte(t.out, '\n')+1, 0)

	for j, v := range t.out[i:] {
		if v == ' ' || v == '\t' {
			continue
		}
		return string(t.out[i : i+j])
	}

	return string(t.out[i:])
}

func (t *transpiler) lastNewline() bool {
	i := max(bytes.LastIndexByte(t.out, '\n')+1, 0)
	for _, v := range t.out[i:] {
		if v != ' ' && v != '\t' {
			return false
		}
	}
	return true
}

func (t *transpiler) wantNewline() string {
	return t.wantNewlineIndent(0)
}

func (t *transpiler) wantNewlineIndent(additionalIndent int) string {
	indent := t.lastIndent()
	if additionalIndent != 0 && t.lastIndentMangled == "" {
		t.lastIndentMangled = indent
	}
	if !t.lastNewline() {
		t.appendString("\n")
		t.appendString(indent)
		t.appendString(strings.Repeat("\t", additionalIndent))
	}
	return indent
}

func (t *transpiler) appendString(s string) {
	if transpilerDebug {
		fmt.Printf("t.appendString(%q)\n", s)
	}
	t.out = append(t.out, s...)
}

func (t *transpiler) appendFromSource(end token.Pos) {
	if transpilerDebug {
		fmt.Printf("t.appendFromSource(%v) -> ", end)
	}
	t.appendString(t.src[t.lastPosWritten-1 : end-1])
	t.lastPosWritten = end
}

func (t *transpiler) transpile() {
	t.lastPosWritten = 1
	ast.Inspect(t.f, t.inspect)
	t.appendFromSource(t.f.FileEnd)
}

func (t *transpiler) inspect(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.BlockStmt:
		t.appendFromSource(n.Lbrace + 1)
		t.transpileList(0, -1, n.List)
		t.appendFromSource(n.Rbrace + 1)
		return false
	}
	return true
}

type directive uint8

const (
	directiveIgnore directive = iota
	directiveOneline
	directiveNormal
)

// TODO: list can be a next ast.Node, not a slice
func (t *transpiler) directive(curPos token.Pos, list []ast.Stmt) directive {
	if len(list) == 0 {
		panic("unreachable")
	}

	var (
		curPosLine = t.fs.Position(curPos).Line
		firstElPos = list[0].Pos()
	)

	for _, v := range t.f.Comments {
		if v.Pos() < curPos {
			// TODO: we can cache the pos to which we previously continued.
			continue
		}
		if v.Pos() > firstElPos {
			break
		}
		if t.fs.Position(v.Pos()).Line == curPosLine || t.semiBetween(curPos, v.Pos()) {
			return directiveOneline
		}
		return directiveNormal
	}

	v := list[0]
	switch v := v.(type) {
	case *ast.OpenTagStmt, *ast.EndTagStmt,
		*ast.AttributeStmt:
		return directiveIgnore
	case *ast.ExprStmt:
		if x, ok := v.X.(*ast.BasicLit); ok && x.Kind == token.STRING {
			return directiveIgnore
		} else if _, ok := v.X.(*ast.TemplateLiteralExpr); ok {
			return directiveIgnore
		}
	}
	if t.fs.Position(v.Pos()).Line == curPosLine || t.semiBetween(curPos, v.Pos()) {
		return directiveOneline
	}
	return directiveNormal
}

func (t *transpiler) semiBetween(start, end token.Pos) bool {
	for _, v := range t.src[start-1 : end-1] {
		if v == ';' {
			return true
		}
		if v == ' ' || v == '\t' || v == '\r' || v == '\n' {
			continue
		}
		panic("unreachable")
	}
	return false
}

func (t *transpiler) writeLineDirective(pos token.Pos, list []ast.Stmt) {
	d := t.directive(pos, list)
	if d == directiveIgnore {
		t.lineDirectiveMangled = true
		return
	}
	if !t.lineDirectiveMangled {
		return
	}
	t.lineDirectiveMangled = false

	p := t.fs.Position(pos + 1)
	if d == directiveOneline {
		t.appendString(" /*line ")
	} else {
		t.appendString("\n//line ")
	}
	t.appendString(p.Filename)
	t.appendString(":")
	t.appendString(strconv.FormatInt(int64(p.Line), 10))
	t.appendString(":")
	t.appendString(strconv.FormatInt(int64(p.Column), 10))
	if d == directiveOneline {
		t.appendString("*/")
	}
}

func (t *transpiler) transpileList(implicitIndentTabCount int, implicitIndentLine int, list []ast.Stmt) {
	for i, n := range list {
		t.writeLineDirective(t.lastPosWritten, list[i:])
		t.appendFromSource(n.Pos())
		switch n := n.(type) {
		case *ast.OpenTagStmt:
			if t.fs.Position(n.Pos()).Line != implicitIndentLine {
				implicitIndentTabCount = 0
				implicitIndentLine = -1
			}
			implicitIndentLine = t.fs.Position(n.Pos()).Line

			t.staticWriteIndent(implicitIndentTabCount, "<"+n.Name.Name)
			t.wantNewlineIndent(implicitIndentTabCount)

			t.appendString("{")
			t.lastPosWritten = n.Name.End()

			implicitIndentTabCount++
			t.transpileList(implicitIndentTabCount, implicitIndentLine, n.Body)
			implicitIndentTabCount--

			t.appendFromSource(n.End() - 1)
			t.wantNewlineIndent(implicitIndentTabCount)
			t.appendString("}")

			t.staticWriteIndent(implicitIndentTabCount, ">")
			t.wantNewlineIndent(implicitIndentTabCount)
			t.appendString("{")
			implicitIndentTabCount++
		case *ast.EndTagStmt:
			implicitIndentTabCount = max(implicitIndentTabCount-1, 0)
			t.wantNewlineIndent(implicitIndentTabCount)
			t.appendString("}")
			t.staticWriteIndent(implicitIndentTabCount, "</"+n.Name.Name+">")
		case *ast.AttributeStmt:
			if t.fs.Position(n.Pos()).Line != implicitIndentLine {
				implicitIndentLine = -1
				implicitIndentTabCount = 0
			}
			t.staticWriteIndent(implicitIndentTabCount, " "+n.AttrName.(*ast.Ident).Name)
			if n.Value != nil {
				switch x := n.Value.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						t.staticWriteIndent(implicitIndentTabCount, `=`+x.Value)
					}
				case *ast.TemplateLiteralExpr:
					t.lastPosWritten = x.Pos()
					for i := range x.Parts {
						t.staticWriteIndent(implicitIndentTabCount, x.Strings[i])
						t.lastPosWritten += token.Pos(len(x.Strings[i])) + 2
						t.dynamicWriteIndent(implicitIndentTabCount, x.Parts[i])
						t.lastPosWritten = x.Parts[i].End()
					}
					t.staticWriteIndent(implicitIndentTabCount, x.Strings[len(x.Strings)-1])
					t.lastPosWritten = x.End()
				}
			}
		case *ast.ExprStmt:
			if t.fs.Position(n.Pos()).Line != implicitIndentLine {
				implicitIndentLine = -1
				implicitIndentTabCount = 0
			}
			if x, ok := n.X.(*ast.BasicLit); ok && x.Kind == token.STRING {
				t.staticWriteIndent(implicitIndentTabCount, x.Value)
			} else if x, ok := n.X.(*ast.TemplateLiteralExpr); ok {
				t.lastPosWritten = x.Pos()
				for i := range x.Parts {
					t.staticWriteIndent(implicitIndentTabCount, x.Strings[i])
					t.lastPosWritten += token.Pos(len(x.Strings[i])) + 2
					if i > 0 {
						t.lastPosWritten++
					}
					t.dynamicWriteIndent(implicitIndentTabCount, x.Parts[i])
					t.lastPosWritten = x.Parts[i].End()
				}
				t.staticWriteIndent(implicitIndentTabCount, x.Strings[len(x.Strings)-1])
				t.lastPosWritten = x.End()
			} else {
				ast.Inspect(n, t.inspect)
				t.appendFromSource(n.End())
			}
		default:
			ast.Inspect(n, t.inspect)
			t.appendFromSource(n.End())
		}
		t.lastPosWritten = n.End()
	}
}

func (t *transpiler) dynamicWrite(n ast.Expr) {
	t.dynamicWriteIndent(0, n)
}

func (t *transpiler) staticWrite(s string) {
	t.staticWriteIndent(0, s)
}

func (t *transpiler) dynamicWriteIndent(additionalIndent int, n ast.Expr) {
	indent := t.wantNewline()
	if additionalIndent != 0 && t.lastIndentMangled == "" {
		t.lastIndentMangled = indent
	}
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("if err := __tgo.DynamicWrite(__tgo_ctx, ")

	ast.Inspect(n, t.inspect)
	t.appendFromSource(n.End())
	t.appendString("); err != nil {\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("\treturn err\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("}")
}

func (t *transpiler) staticWriteIndent(additionalIndent int, s string) {
	indent := t.wantNewline()
	if additionalIndent != 0 && t.lastIndentMangled == "" {
		t.lastIndentMangled = indent
	}
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("if err := __tgo_ctx.WriteString(`")
	t.appendString(s)
	t.appendString("`); err != nil {\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("\treturn err\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("}")
}
