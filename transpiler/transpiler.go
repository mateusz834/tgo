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
		t.transpileList(n.List)
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
		if t.fs.Position(v.Pos()).Line == curPosLine {
			return directiveOneline
		}
		return directiveNormal
	}

	for _, v := range list {
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
		if t.fs.Position(v.Pos()).Line == curPosLine {
			return directiveOneline
		}
		return directiveNormal
	}

	panic("unreachable")
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

func (t *transpiler) transpileList(list []ast.Stmt) {
	var (
		implicitIndentTabCount = 0
		implicitIndentLine     = -1
	)

	for i, n := range list {
		t.writeLineDirective(t.lastPosWritten, list[i:])
		t.appendFromSource(n.Pos())
		switch n := n.(type) {
		case *ast.OpenTagStmt:
			if t.fs.Position(n.Pos()).Line != implicitIndentLine {
				implicitIndentTabCount = 0
				implicitIndentLine = -1
			}

			t.staticWriteIndent(implicitIndentTabCount, "<"+n.Name.Name)
			t.wantNewlineIndent(implicitIndentTabCount)

			t.appendString("{")
			t.lastPosWritten = n.Name.End()
			t.transpileList(n.Body)
			t.appendFromSource(n.End() - 1)
			t.wantNewlineIndent(implicitIndentTabCount)
			t.appendString("}")

			t.staticWriteIndent(implicitIndentTabCount, ">")
			t.wantNewlineIndent(implicitIndentTabCount)
			t.appendString("{")
			implicitIndentTabCount++
			implicitIndentLine = t.fs.Position(n.Pos()).Line
		case *ast.EndTagStmt:
			implicitIndentTabCount = max(implicitIndentTabCount-1, 0)
			t.wantNewlineIndent(implicitIndentTabCount)
			t.appendString("}")
			t.staticWriteIndent(implicitIndentTabCount, "</"+n.Name.Name+">")
		case *ast.AttributeStmt:
			t.staticWrite(" " + n.AttrName.(*ast.Ident).Name)
			if n.Value != nil {
				switch x := n.Value.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						t.staticWrite(`=` + x.Value)
					}
				case *ast.TemplateLiteralExpr:
					t.lastPosWritten = x.Pos()
					for i := range x.Parts {
						t.staticWrite(x.Strings[i])
						t.lastPosWritten += token.Pos(len(x.Strings[i])) + 2
						t.dynamicWrite(x.Parts[i])
						t.lastPosWritten = x.Parts[i].End()
					}
					t.staticWrite(x.Strings[len(x.Strings)-1])
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
					t.dynamicWriteIndent(implicitIndentTabCount, x.Parts[i])
					t.lastPosWritten = x.Parts[i].End()
				}
				t.staticWrite(x.Strings[len(x.Strings)-1])
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

// TODO: track line mapping?
// TODO: and then make a function that automatically inserts the line comment
// only when it is needed.

//type lineDirectiveState struct {
//	lastPos    token.Pos
//	lastTgoPos token.Pos
//}
//
//func (l *lineDirectiveState) shouldAddLineDirective(curGoFilePos, curTgoFilePos token.Pos) bool {
//	calcTgoPos := l.lastTgoPos + (l.lastPos - curGoFilePos)
//	if calcTgoPos < curTgoFilePos {
//		panic("unreachable")
//	}
//	return calcTgoPos > curTgoFilePos
//}
//
//func (l *lineDirectiveState) lineAdded(curGoFilePos, curTgoFilePos token.Pos) {
//	l.lastPos = curGoFilePos
//	l.lastTgoPos = curTgoFilePos
//}

//type transpiler struct {
//	f   *ast.File
//	fs  *token.FileSet
//	src string
//
//	out []byte
//	//tmp []byte
//
//	//lastSourcePosWritten token.Pos
//
//	//staticStartWritten bool
//	//indentPos          token.Pos
//
//	//lastWrittenPos token.Pos
//
//	//addedIndent       int
//	//staticAddedIndent int
//
//	//lastIgnored bool
//
//	//lds lineDirectiveState
//}

//func (t *transpiler) addLineDirective(tgoPos token.Pos) {
//	if t.lds.shouldAddLineDirective(token.Pos(len(t.out)+1), tgoPos) {
//		t.writeLineDirective(tgoPos)
//		t.lds.lineAdded(token.Pos(len(t.out)+1), tgoPos)
//	}
//}

/*
	<div @class="bg-red">; a := 2; <div>
*/

//func (t *transpiler) writeLineDirective(pos token.Pos) {
//	singleLine := false
//	switch t.src[pos] {
//	//case '\t', ' ':
//	case '\n':
//		pos += 2
//	default:
//		singleLine = true
//	}
//
//	p := t.fs.Position(pos)
//
//	if singleLine {
//		//if p.Column == 0 {
//		//	panic("unreachable")
//		//}
//		//p.Column--
//		t.writeSource(" /*line ")
//	} else {
//		t.writeSource("\n//line ")
//	}
//
//	t.writeSource(t.fs.File(pos).Name())
//	t.writeSource(":")
//	t.writeSource(strconv.FormatInt(int64(p.Line), 10))
//	t.writeSource(":")
//	t.writeSource(strconv.FormatInt(int64(p.Column), 10))
//
//	if singleLine {
//		t.writeSource("*/ ")
//	}
//}
//
//func (t *transpiler) transpile() {
//	t.writeSource("//line ")
//	t.writeSource(t.fs.File(t.f.Pos()).Name())
//	t.writeSource(":1:1\n")
//	ast.Walk(t, t.f)
//	t.appendString(t.src[t.lastSourcePosWritten-1:])
//}
//
//func (t *transpiler) transpileBlock(openPos, closePos token.Pos, list []ast.Stmt) {
//	t.writeSource(t.src[t.lastSourcePosWritten-1 : openPos-1])
//	t.lastSourcePosWritten = openPos
//
//	defer func(v bool) {
//		t.lastIgnored = v
//	}(t.lastIgnored)
//
//	var (
//		lastWhitePos = openPos
//		lastOut      = t.out
//		lastTmp      = t.tmp
//	)
//
//	lastEndPos := openPos
//	for _, v := range list {
//		t.writeSource(t.src[lastWhitePos-1 : lastEndPos])
//		t.writeLineDirective(lastEndPos)
//		t.writeSource(t.src[lastWhitePos : v.Pos()-1])
//
//		t.lastIgnored = false
//		ast.Walk(t, v)
//		if t.lastIgnored {
//			t.out = lastOut
//			t.tmp = lastTmp
//		}
//
//		lastOut = t.out
//		lastTmp = t.tmp
//		lastWhitePos = v.End()
//		lastEndPos = v.End()
//	}
//
//	if len(list) == 0 {
//		t.writeSource(t.src[openPos:closePos])
//	} else {
//		t.writeSource(t.src[list[len(list)-1].Pos():closePos])
//	}
//	t.lastSourcePosWritten = closePos
//}
//
//func (t *transpiler) Visit(n ast.Node) ast.Visitor {
//	switch n := n.(type) {
//	case *ast.BlockStmt:
//		t.transpileBlock(n.Pos(), n.End(), n.List)
//		return nil
//		//if t.staticStartWritten {
//		//	t.tmp = append(t.tmp, []byte(t.src[t.lastSourcePosWritten-1:n.Pos()])...)
//		//	t.lastSourcePosWritten = n.Pos() + 1
//		//}
//		//for i, v := range n.List {
//		//	ast.Walk(t, v)
//		//	switch v := v.(type) {
//		//	case *ast.OpenTagStmt:
//		//		t.writeSource("\n")
//		//		t.writeSource(t.indentAt(v.OpenPos))
//		//		next := n.List[i+1]
//		//		nextLine := t.fs.Position(next.Pos()).Line
//		//		curLine := t.fs.Position(v.End()).Line
//		//		if nextLine != curLine {
//		//			t.writeSource("{\n")
//		//			t.writeSource("//line new.tgo:")
//		//			t.writeSource(strconv.FormatInt(int64(n.Pos()), 10))
//		//		} else {
//		//			t.writeSource("{ /*line new.tgo:")
//		//			t.writeSource(strconv.FormatInt(int64(n.Pos()), 10))
//		//			t.writeSource("*/ ")
//		//		}
//		//		t.addedIndent++
//		//	case *ast.EndTagStmt:
//		//		t.writeSource("\n")
//		//		t.writeSource(t.indentAt(v.OpenPos))
//		//		t.writeSource("}")
//		//		t.addedIndent--
//		//	}
//		//}
//		//if t.staticStartWritten {
//		//	t.tmp = append(t.tmp, []byte(t.src[t.lastSourcePosWritten-1:n.End()])...)
//		//	t.lastSourcePosWritten = n.End() + 1
//		//}
//		//return nil
//	case *ast.OpenTagStmt:
//		if len(n.Body) == 0 {
//			t.writeStatic(n.Pos(), "<", n.Name.Name, ">")
//			t.lastSourcePosWritten = n.End()
//		} else {
//			t.writeStatic(n.Pos(), "<", n.Name.Name)
//			t.lastSourcePosWritten = n.Name.End()
//
//			//t.writeSource("\n")
//			//t.writeSource(t.indentAt(n.OpenPos))
//			//t.writeSource("{")
//
//			for _, n := range n.Body {
//				ast.Walk(t, n)
//			}
//
//			//t.writeSource("\n")
//			//t.writeSource(t.indentAt(n.OpenPos))
//			//t.writeSource("}")
//
//			t.writeStatic(n.Pos(), ">")
//		}
//		return nil
//	case *ast.EndTagStmt:
//		t.writeStatic(n.Pos(), "</", n.Name.Name, ">")
//		t.lastSourcePosWritten = n.End()
//		return nil
//	case *ast.AttributeStmt:
//		t.writeStatic(n.Pos(), " ", n.AttrName.(*ast.Ident).Name, "=", n.Value.(*ast.BasicLit).Value)
//		return nil
//	case *ast.ExprStmt:
//		switch n := n.X.(type) {
//		case *ast.BasicLit:
//			if n.Kind == token.STRING {
//				t.writeStatic(n.Pos(), n.Value)
//				return nil
//			}
//		case *ast.TemplateLiteralExpr:
//			panic("here")
//		}
//	}
//
//	t.endStatic()
//	return t
//}
//
//func (t *transpiler) writeSource(s string) {
//	if transpilerDebug {
//		fmt.Printf("t.writeSource(%q)\n", s)
//	}
//	if t.staticStartWritten {
//		t.tmp = append(t.tmp, s...)
//	} else {
//		t.appendString(s)
//	}
//}
//
//func (t *transpiler) appendString(s string) {
//	if transpilerDebug {
//		fmt.Printf("t.appendString(%q)\n", s)
//		//fmt.Printf("%s\n", debug.Stack())
//	}
//	t.out = append(t.out, s...)
//}
//
//func (t *transpiler) writeStatic(indentPos token.Pos, strs ...string) {
//	if !t.staticStartWritten {
//		if indentPos > t.lastSourcePosWritten {
//			t.appendString(t.src[t.lastSourcePosWritten-1 : indentPos-1])
//			t.lastSourcePosWritten = indentPos
//			t.indentPos = indentPos
//		}
//		for range t.addedIndent {
//			t.appendString("\t")
//		}
//		t.staticAddedIndent = t.addedIndent
//		t.appendString(`if err := __tgo_ctx.WriteString("`)
//		t.staticStartWritten = true
//	}
//	for _, v := range strs {
//		t.appendString(v)
//	}
//}
//
//func (t *transpiler) endStatic() {
//	if t.staticStartWritten {
//		indent := t.indentAt(t.indentPos)
//		t.appendString("\"); err != nil {\n")
//		t.appendString(indent)
//		for range t.staticAddedIndent {
//			t.appendString("\t")
//		}
//		t.appendString("\treturn err\n")
//		t.appendString(indent)
//		for range t.staticAddedIndent {
//			t.appendString("\t")
//		}
//		t.appendString("}")
//
//		t.appendString(string(t.tmp))
//
//		t.tmp = t.tmp[:0]
//		t.staticStartWritten = false
//	}
//}
//
//func (t *transpiler) indentAt(pos token.Pos) string {
//	beforePos := t.src[:pos-1]
//	i := max(strings.LastIndexByte(beforePos, '\n')+1, 0)
//
//	for j, v := range beforePos[i:] {
//		if v == ' ' || v == '\t' {
//			continue
//		}
//		return beforePos[i : i+j]
//	}
//
//	return beforePos[i:]
//}

//const debug = false
//
//func Transpile(fs *token.FileSet, f *ast.File, source string) string {
//	t := transpiler{
//		fs:     fs,
//		f:      f,
//		source: source,
//	}
//	t.out.Grow(len(source))
//	t.transpile()
//	return t.out.String()
//}
//
//type transpiler struct {
//	fs     *token.FileSet
//	f      *ast.File
//	source string
//
//	out strings.Builder
//
//	lastSourcePos token.Pos
//
//	staticDataFirstPos token.Pos
//	staticDataWrite    []string
//}
//
//func (t *transpiler) fromSource(i, j token.Pos) {
//	if debug {
//		fmt.Printf(
//			"appending original source (%v-%v): %q\n",
//			t.fs.Position(i), t.fs.Position(j), t.source[i-1:j-1],
//		)
//	}
//	if i < t.lastSourcePos {
//		panic("unreachable")
//	}
//	t.lastSourcePos = i
//	t.out.WriteString(t.source[i-1 : j-1])
//}
//
//func (t *transpiler) transpile() {
//	prevDeclEnd := t.f.FileStart
//	for _, v := range t.f.Decls {
//		t.fromSource(prevDeclEnd, v.Pos())
//		t.inspect(v)
//		prevDeclEnd = v.End()
//	}
//	t.fromSource(prevDeclEnd, t.f.FileEnd)
//}
//
//func (t *transpiler) staticData(indentPos token.Pos, s string) {
//	if len(t.staticDataWrite) == 0 {
//		t.staticDataFirstPos = indentPos
//	}
//	t.staticDataWrite = append(t.staticDataWrite, s)
//}
//
//func (t *transpiler) writeStaticData() {
//	if len(t.staticDataWrite) != 0 {
//		indent := t.indentAt(t.staticDataFirstPos)
//		t.out.WriteString("if err := __tgo.Write(`")
//		for _, v := range t.staticDataWrite {
//			t.out.WriteString(v)
//		}
//		t.out.WriteString("`); err != nil {")
//		t.out.WriteString("\n")
//		t.out.WriteString(indent)
//		t.out.WriteString("\treturn err\n")
//		t.out.WriteString(indent)
//		t.out.WriteString("}")
//	}
//	t.staticDataWrite = t.staticDataWrite[:0]
//}
//
//func (t *transpiler) indentAt(pos token.Pos) string {
//	beforePos := t.source[:pos-1]
//	i := max(strings.LastIndexByte(beforePos, '\n')+1, 0)
//	for j, v := range beforePos[i:] {
//		if v == ' ' || v == '\t' {
//			continue
//		}
//		return beforePos[i : i+j]
//	}
//	return beforePos[i:]
//}
//
//func (t *transpiler) inspect(n ast.Node) bool {
//	switch n := n.(type) {
//
//	case *ast.OpenTagStmt:
//		t.staticData(n.OpenPos, "<")
//		t.staticData(n.OpenPos, n.Name.Name)
//		if len(n.Body) != 0 {
//			inspectNodes(t, n.Body[0].Pos(), n.Body)
//			t.fromSource(n.Body[len(n.Body)-1].End(), n.ClosePos)
//			t.out.WriteByte('}')
//		}
//		t.staticData(n.OpenPos, ">")
//		return true
//	case *ast.EndTagStmt:
//		t.staticData(n.OpenPos, "</")
//		t.staticData(n.OpenPos, n.Name.Name)
//		t.staticData(n.OpenPos, ">")
//		return true
//	case *ast.TemplateLiteralExpr:
//	case *ast.AttributeStmt:
//	}
//
//	t.writeStaticData()
//
//	switch n := n.(type) {
//	case *ast.Ident:
//		t.fromSource(n.Pos(), n.End())
//	case *ast.Ellipsis:
//		panic("here")
//	case *ast.BasicLit:
//		t.fromSource(n.Pos(), n.End())
//	case *ast.FuncLit:
//		t.fromSource(n.Pos(), n.Body.Lbrace)
//		t.inspect(n.Body)
//	case *ast.CompositeLit:
//		start := n.Pos()
//		if n.Type != nil {
//			t.inspect(n.Type)
//			t.fromSource(n.Type.End(), n.Lbrace+1)
//			start = n.Lbrace + 1
//		}
//		if len(n.Elts) == 0 {
//			t.fromSource(start, n.Rbrace+1)
//			return false
//		}
//		inspectNodes(t, start, n.Elts)
//		t.fromSource(n.Elts[len(n.Elts)-1].End(), n.Rbrace+1)
//	case *ast.ParenExpr:
//		t.fromSource(n.Lparen, n.X.Pos())
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.Rparen+1)
//	case *ast.SelectorExpr:
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.End())
//	case *ast.IndexExpr:
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.Index.Pos())
//		t.inspect(n.Index)
//		t.fromSource(n.Index.End(), n.Rbrack+1)
//	case *ast.IndexListExpr:
//		panic("todo")
//	case *ast.SliceExpr:
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.Lbrack+1)
//		lastEnd := n.Lbrack + 1
//		if n.Low != nil {
//			t.fromSource(lastEnd, n.Low.Pos())
//			t.inspect(n.Low)
//			lastEnd = n.Low.End()
//		}
//		if n.High != nil {
//			t.fromSource(lastEnd, n.High.Pos())
//			t.inspect(n.High)
//			lastEnd = n.High.End()
//		}
//		if n.Max != nil {
//			t.fromSource(lastEnd, n.Max.Pos())
//			t.inspect(n.Max)
//			lastEnd = n.Max.End()
//		}
//		t.fromSource(lastEnd, n.Rbrack+1)
//	case *ast.TypeAssertExpr:
//		t.inspect(n.X)
//		if n.Type == nil {
//			t.fromSource(n.X.End(), n.Rparen+1)
//			return false
//		}
//		t.fromSource(n.X.End(), n.Type.Pos())
//		t.inspect(n.Type)
//		t.fromSource(n.Type.End(), n.Rparen+1)
//	case *ast.CallExpr:
//		t.inspect(n.Fun)
//		if len(n.Args) != 0 {
//			t.fromSource(n.Fun.End(), n.Lparen+1)
//			inspectNodes(t, n.Lparen+1, n.Args)
//			t.fromSource(n.Args[len(n.Args)-1].End(), n.Rparen+1)
//		} else {
//			t.fromSource(n.Fun.End(), n.End())
//		}
//	case *ast.StarExpr:
//		t.fromSource(n.Star, n.X.Pos())
//		t.inspect(n.X)
//	case *ast.UnaryExpr:
//		t.fromSource(n.OpPos, n.X.Pos())
//		t.inspect(n.X)
//	case *ast.BinaryExpr:
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.Y.Pos())
//		t.inspect(n.Y)
//	case *ast.KeyValueExpr:
//		t.inspect(n.Key)
//		t.fromSource(n.Key.End(), n.Value.Pos())
//		t.inspect(n.Value)
//
//	case *ast.GenDecl:
//		if n.Tok != token.VAR {
//			t.fromSource(n.Pos(), n.End())
//			return false
//		}
//		t.fromSource(n.Pos(), n.Specs[0].Pos())
//		inspectNodes(t, n.Specs[0].Pos(), n.Specs)
//		t.fromSource(n.Specs[len(n.Specs)-1].End(), n.End())
//	case *ast.ValueSpec:
//		t.fromSource(n.Pos(), n.Names[len(n.Names)-1].End())
//		start := n.Names[len(n.Names)-1].End()
//		if n.Type != nil {
//			t.fromSource(start, n.Type.Pos())
//			t.inspect(n.Type)
//			start = n.Type.End()
//		}
//		inspectNodes(t, start, n.Values)
//	case *ast.FuncDecl:
//		t.fromSource(n.Pos(), n.Body.Lbrace)
//		t.inspect(n.Body)
//
//	case *ast.DeclStmt:
//		t.inspect(n.Decl)
//	case *ast.EmptyStmt:
//		t.fromSource(n.Pos(), n.End())
//	case *ast.LabeledStmt:
//		t.inspect(n.Label)
//		t.fromSource(n.Label.End(), n.Stmt.Pos())
//		t.inspect(n.Stmt)
//	case *ast.ExprStmt:
//		t.inspect(n.X)
//	case *ast.SendStmt:
//		t.inspect(n.Chan)
//		t.fromSource(n.Chan.End(), n.Value.Pos())
//		t.inspect(n.Value)
//	case *ast.IncDecStmt:
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.End())
//	case *ast.AssignStmt:
//		inspectNodes(t, n.Pos(), n.Lhs)
//		t.fromSource(n.Lhs[len(n.Lhs)-1].End(), n.Rhs[0].Pos())
//		inspectNodes(t, n.Rhs[0].Pos(), n.Rhs)
//	case *ast.GoStmt:
//		t.fromSource(n.Pos(), n.Call.Pos())
//		t.inspect(n.Call)
//	case *ast.DeferStmt:
//		t.fromSource(n.Pos(), n.Call.Pos())
//		t.inspect(n.Call)
//	case *ast.ReturnStmt:
//		if len(n.Results) != 0 {
//			t.fromSource(n.Pos(), n.Results[0].Pos())
//			inspectNodes(t, n.Results[0].Pos(), n.Results)
//			return false
//		}
//		t.fromSource(n.Pos(), n.End())
//	case *ast.BranchStmt:
//		t.fromSource(n.Pos(), n.End())
//	case *ast.BlockStmt:
//		if len(n.List) != 0 {
//			t.fromSource(n.Pos(), n.List[0].Pos())
//			inspectNodes(t, n.List[0].Pos(), n.List)
//			t.fromSource(n.List[len(n.List)-1].End(), n.End())
//			return false
//		}
//		t.fromSource(n.Pos(), n.End())
//	case *ast.IfStmt:
//		start := n.Pos()
//		if n.Init != nil {
//			t.fromSource(n.Pos(), n.Init.Pos())
//			t.inspect(n.Init)
//			start = n.Init.End()
//		}
//		t.fromSource(start, n.Cond.Pos())
//		t.inspect(n.Cond)
//		t.fromSource(n.Cond.End(), n.Body.Pos())
//		t.inspect(n.Body)
//		if n.Else != nil {
//			t.fromSource(n.Body.End(), n.Else.Pos())
//			t.inspect(n.Else)
//		}
//	case *ast.CaseClause:
//		if len(n.List) != 0 {
//			t.fromSource(n.Pos(), n.List[0].Pos())
//			inspectNodes(t, n.List[0].Pos(), n.List)
//			t.fromSource(n.List[len(n.List)-1].End(), n.Colon+1)
//		} else {
//			t.fromSource(n.Pos(), n.Colon+1)
//		}
//		if len(n.Body) != 0 {
//			t.fromSource(n.Colon+1, n.Body[0].Pos())
//			inspectNodes(t, n.Body[0].Pos(), n.Body)
//		}
//	case *ast.SwitchStmt:
//		start := n.Pos()
//		if n.Init != nil {
//			t.fromSource(n.Pos(), n.Init.Pos())
//			t.inspect(n.Init)
//			start = n.Init.End()
//		}
//		if n.Tag != nil {
//			t.fromSource(start, n.Tag.Pos())
//			t.inspect(n.Tag)
//			start = n.Tag.End()
//		}
//		t.fromSource(start, n.Body.Pos())
//		t.inspect(n.Body)
//	case *ast.TypeSwitchStmt:
//		start := n.Pos()
//		if n.Init != nil {
//			t.fromSource(n.Pos(), n.Init.Pos())
//			t.inspect(n.Init)
//			start = n.Init.End()
//		}
//		t.fromSource(start, n.Assign.Pos())
//		t.inspect(n.Assign)
//		t.fromSource(n.Assign.End(), n.Body.Pos())
//		t.inspect(n.Body)
//	case *ast.CommClause:
//		if n.Comm != nil {
//			t.fromSource(n.Pos(), n.Comm.Pos())
//			t.inspect(n.Comm)
//			t.fromSource(n.Comm.End(), n.Colon+1)
//		} else {
//			t.fromSource(n.Pos(), n.Colon+1)
//		}
//		if len(n.Body) != 0 {
//			t.fromSource(n.Colon+1, n.Body[0].Pos())
//			inspectNodes(t, n.Body[0].Pos(), n.Body)
//		}
//	case *ast.SelectStmt:
//		t.fromSource(n.Pos(), n.Body.Pos())
//		t.inspect(n.Body)
//	case *ast.ForStmt:
//		start := n.Pos()
//		if n.Init != nil {
//			t.fromSource(start, n.Init.Pos())
//			t.inspect(n.Init)
//			start = n.Init.End()
//		}
//		if n.Cond != nil {
//			t.fromSource(start, n.Cond.Pos())
//			t.inspect(n.Cond)
//			start = n.Cond.End()
//		}
//		if n.Post != nil {
//			t.fromSource(start, n.Post.Pos())
//			t.inspect(n.Post)
//			start = n.Post.End()
//		}
//		t.fromSource(start, n.Body.Pos())
//		t.inspect(n.Body)
//	case *ast.RangeStmt:
//		start := n.Pos()
//		if n.Key != nil {
//			t.fromSource(start, n.Key.Pos())
//			t.inspect(n.Key)
//			start = n.Key.End()
//		}
//		if n.Value != nil {
//			t.fromSource(start, n.Value.Pos())
//			t.inspect(n.Value)
//			start = n.Value.End()
//		}
//		t.fromSource(start, n.X.Pos())
//		t.inspect(n.X)
//		t.fromSource(n.X.End(), n.Body.Pos())
//		t.inspect(n.Body)
//
//	case *ast.ArrayType, *ast.StructType,
//		*ast.FuncType, *ast.InterfaceType,
//		*ast.MapType, *ast.ChanType:
//		t.fromSource(n.Pos(), n.End())
//	case nil:
//		// TODO: panic?
//	default:
//		panic("unexpected type: " + fmt.Sprintf("%T", n))
//	}
//	return false
//}
//
//func inspectNodes[T ast.Node](t *transpiler, prevEndPos token.Pos, nodes []T) {
//	ignoreNext := false
//	for _, v := range nodes {
//		if !ignoreNext {
//			t.fromSource(prevEndPos, v.Pos())
//		}
//		ignoreNext = t.inspect(v)
//		prevEndPos = v.End()
//	}
//	t.writeStaticData()
//}

//type fileTranspiler struct {
//	f *ast.File
//
//	staticWrite            []string
//	staticWriteReplaceItem *ast.Stmt
//	ignore                 []*ast.Stmt
//}
//
//func (f *fileTranspiler) transpile() {
//	f.transpileNode(f.f)
//	f.flushStaticWrite()
//}
//
//func (f *fileTranspiler) transpileNode(n ast.Node) {
//	ast.Inspect(n, func(n ast.Node) bool {
//		switch n := n.(type) {
//		case *ast.BlockStmt:
//			f.transpileStmts(n.List)
//		case *ast.CaseClause:
//			f.transpileStmts(n.Body)
//		case *ast.CommClause:
//			f.transpileStmts(n.Body)
//		default:
//		}
//		return true
//	})
//}
//
//func (f *fileTranspiler) appendStatic(n *ast.Stmt, s string) {
//	if len(f.staticWrite) == 0 {
//		f.staticWriteReplaceItem = n
//	} else {
//		if f.staticWriteReplaceItem != n {
//			f.ignore = append(f.ignore, n)
//		}
//	}
//	f.staticWrite = append(f.staticWrite, s)
//}
//
//func (f *fileTranspiler) transpileStmts(list []ast.Stmt) {
//	for i, n := range list {
//		switch n := n.(type) {
//		case *ast.OpenTagStmt:
//			f.appendStatic(&list[i], "<")
//			f.appendStatic(&list[i], n.Name.Name)
//			f.transpileStmts(n.Body)
//			f.appendStatic(&list[i], ">")
//		case *ast.EndTagStmt:
//			f.appendStatic(&list[i], "<")
//			f.appendStatic(&list[i], n.Name.Name)
//			f.appendStatic(&list[i], "/>")
//		case *ast.AttributeStmt:
//			panic("here")
//		case *ast.ExprStmt:
//			if n, ok := n.X.(*ast.BasicLit); ok && n.Kind == token.STRING {
//				f.appendStatic(&list[i], n.Value)
//				continue
//			}
//			f.transpileNode(n)
//			f.flushStaticWrite()
//		default:
//			f.flushStaticWrite()
//			f.transpileNode(n)
//		}
//	}
//}
//
//func (f *fileTranspiler) flushStaticWrite() {
//	if len(f.staticWrite) == 0 && f.staticWriteReplaceItem == nil {
//		return
//	}
//	*f.staticWriteReplaceItem = &ast.ExprStmt{
//		X: &ast.BasicLit{
//			Kind:  token.STRING,
//			Value: strings.Join(f.staticWrite, ""),
//		},
//	}
//	for _, v := range f.ignore {
//		*v = nil
//	}
//	f.ignore = f.ignore[:0]
//	f.staticWrite = f.staticWrite[:0]
//	f.staticWriteReplaceItem = nil
//}
