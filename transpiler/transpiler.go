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

	inStaticWrite  bool
	staticWritePos int
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
	t.appendString("//line ")
	t.appendString(t.fs.Position(t.f.FileStart).Filename)
	t.appendString(":1:1\n")
	ast.Inspect(t.f, t.inspect)
	t.appendFromSource(t.f.FileEnd)
}

func (t *transpiler) inspect(n ast.Node) bool {
	t.inStaticWrite = false
	defer func() {
		t.inStaticWrite = false
	}()
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

func (t *transpiler) directive(curPos token.Pos, nextNode ast.Node) directive {
	var (
		curPosLine  = t.fs.Position(curPos).Line
		nextNodePos = nextNode.Pos()
	)

	for _, v := range t.f.Comments {
		if v.Pos() < curPos {
			// TODO: we can cache the pos to which we previously continued.
			continue
		}
		if v.Pos() > nextNodePos {
			break
		}
		if t.fs.Position(v.Pos()).Line == curPosLine || t.semiBetween(curPos, v.Pos()) {
			return directiveOneline
		}
		return directiveNormal
	}

	switch nextNode := nextNode.(type) {
	case *ast.OpenTagStmt, *ast.EndTagStmt,
		*ast.AttributeStmt:
		return directiveIgnore
	case *ast.ExprStmt:
		if x, ok := nextNode.X.(*ast.BasicLit); ok && x.Kind == token.STRING {
			return directiveIgnore
		} else if _, ok := nextNode.X.(*ast.TemplateLiteralExpr); ok {
			return directiveIgnore
		}
	}
	if t.fs.Position(nextNodePos).Line == curPosLine || t.semiBetween(curPos, nextNodePos) {
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

func (t *transpiler) writeLineDirective(pos token.Pos, next ast.Node) {
	d := t.directive(pos, next)
	if d == directiveIgnore {
		t.lineDirectiveMangled = true
		return
	}
	if !t.lineDirectiveMangled {
		return
	}
	t.inStaticWrite = false
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
	for _, n := range list {
		t.writeLineDirective(t.lastPosWritten, n)
		t.appendFromSource(n.Pos())
		switch n := n.(type) {
		case *ast.OpenTagStmt:
			if t.fs.Position(n.Pos()).Line != implicitIndentLine {
				implicitIndentTabCount = 0
				implicitIndentLine = -1
			}
			implicitIndentLine = t.fs.Position(n.Pos()).Line

			t.staticWriteIndentNoClearNewline(implicitIndentTabCount, "<"+n.Name.Name)
			t.wantNewlineIndent(implicitIndentTabCount)

			t.appendString("{")
			t.lastPosWritten = n.Name.End()

			t.transpileList(implicitIndentTabCount+1, implicitIndentLine, n.Body)

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
			if n.Value != nil {
				switch x := n.Value.(type) {
				case *ast.BasicLit:
					t.staticWriteIndent(implicitIndentTabCount, " "+n.AttrName.(*ast.Ident).Name+"=")
					if x.Kind == token.STRING {
						t.staticWriteIndent(implicitIndentTabCount, x.Value)
					}
				case *ast.TemplateLiteralExpr:
					t.staticWriteIndentNoClearNewline(implicitIndentTabCount, " "+n.AttrName.(*ast.Ident).Name+"=")
					t.lastPosWritten = x.Pos()
					for i := range x.Parts {
						t.staticWriteIndentNoClearNewline(implicitIndentTabCount, x.Strings[i])
						t.lastPosWritten += token.Pos(len(x.Strings[i])) + 2
						t.inStaticWrite = false
						t.dynamicWriteIndent(implicitIndentTabCount, x.Parts[i])
						t.lastPosWritten = x.Parts[i].End()
					}
					t.staticWriteIndentNoClearNewline(implicitIndentTabCount, x.Strings[len(x.Strings)-1])
					t.lastPosWritten = x.End()
				}
			} else {
				t.staticWriteIndent(implicitIndentTabCount, " "+n.AttrName.(*ast.Ident).Name)
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
					t.staticWriteIndentNoClearNewline(implicitIndentTabCount, x.Strings[i])
					t.lastPosWritten += token.Pos(len(x.Strings[i])) + 2
					if i > 0 {
						t.lastPosWritten++
					}
					t.inStaticWrite = false
					t.dynamicWriteIndent(implicitIndentTabCount, x.Parts[i])
					t.lastPosWritten = x.Parts[i].End()
				}
				t.staticWriteIndentNoClearNewline(implicitIndentTabCount, x.Strings[len(x.Strings)-1])
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

func (t *transpiler) dynamicWriteIndent(additionalIndent int, n ast.Expr) {
	indent := t.wantNewlineIndent(additionalIndent)
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
	if t.inStaticWrite {
		t.clearWithPrevNewline()
		t.out = slices.Insert(t.out, t.staticWritePos, []byte(s)...)
		t.staticWritePos += len(s)
		return
	}

	t.inStaticWrite = true
	indent := t.wantNewlineIndent(additionalIndent)
	t.appendString("if err := __tgo_ctx.WriteString(`")
	t.appendString(s)
	t.staticWritePos = len(t.out)
	t.appendString("`); err != nil {\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("\treturn err\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("}")
}

func (t *transpiler) staticWriteIndentNoClearNewline(additionalIndent int, s string) {
	if t.inStaticWrite {
		t.out = slices.Insert(t.out, t.staticWritePos, []byte(s)...)
		t.staticWritePos += len(s)
		return
	}

	t.inStaticWrite = true
	indent := t.wantNewlineIndent(additionalIndent)
	t.appendString("if err := __tgo_ctx.WriteString(`")
	t.appendString(s)
	t.staticWritePos = len(t.out)
	t.appendString("`); err != nil {\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("\treturn err\n")
	t.appendString(indent)
	t.appendString(strings.Repeat("\t", additionalIndent))
	t.appendString("}")
}

func (t *transpiler) clearWithPrevNewline() {
	if t.lastNewline() {
		i := bytes.LastIndexByte(t.out, '\n')
		if i > 0 {
			t.out = t.out[:i]
		}
	}
}
