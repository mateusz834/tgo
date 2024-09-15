package transpiler

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mateusz834/tgo/debug"
	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

func Transpile(f *ast.File, fs *token.FileSet, src string) string {
	t := transpiler{
		f:   f,
		fs:  fs,
		src: src,
		out: slices.Grow([]byte{}, len(src)*2),

		lastIndentation: "\n",
	}
	t.transpile()
	if len(t.tmp) != 0 {
		panic("unreachable")
	}
	return string(t.out)
}

type transpiler struct {
	f   *ast.File
	fs  *token.FileSet
	src string

	out []byte
	tmp []byte

	lastPosWritten token.Pos // last position processed by the transpiler of the src.

	// if set to true, then some generated source was written before, and line
	// mapping would get out of sync when appending from original source,
	// before appending anything from src a line directive needs
	// to be written.
	lineDirectiveMangled bool

	inStaticWrite bool // if true, then inside of a static string write call.

	lastIndentation string // last indentation found in the source, prefixed with a newline.

	scopeRemainingOpenCount        int
	forceAllBracesToBeClosedBefore int
}

func (t *transpiler) posToOffset(p token.Pos) int {
	return t.fs.File(t.f.FileStart).Offset(p)
}

func (t *transpiler) offsetToPos(off int) token.Pos {
	return t.fs.File(t.f.FileStart).Pos(off)
}

func (t *transpiler) appendSource(s string) {
	if debug.Verbose {
		fmt.Printf("t.appendString(%q)\n", s)
	}
	t.flushTmp()
	t.out = append(t.out, s...)
}

func (t *transpiler) appendFromSource(end token.Pos) {
	if debug.Verbose {
		fmt.Printf("t.appendFromSource(%v (%v)) -> ", t.fs.Position(end), end)
	}
	t.appendSource(t.src[t.posToOffset(t.lastPosWritten):t.posToOffset(end)])
	t.lastPosWritten = end
}

func (t *transpiler) flushTmp() {
	t.out = append(t.out, t.tmp...)
	t.tmp = t.tmp[:0]
	t.forceAllBracesToBeClosedBefore = t.scopeRemainingOpenCount
}

func (t *transpiler) transpile() {
	// Gopls does not format files that are generated.
	// See: https://go.dev/issue/49555
	t.appendSource("// Code generated by tgo - DO NOT EDIT.\n\n")

	t.lastPosWritten = t.f.FileStart
	t.appendSource("//line ")
	t.appendSource(t.fs.File(t.f.FileStart).Name())
	t.appendSource(":1:1\n")
	ast.Inspect(t.f, t.inspect)
	t.appendFromSource(t.f.FileEnd)
}

func (t *transpiler) addLineDirectiveBeforeRbrace(rbracePos token.Pos) {
	if t.lineDirectiveMangled {
		var (
			onelineDirective = t.fs.Position(t.lastPosWritten).Line == t.fs.Position(rbracePos).Line
			beforeNewline    = true
			firstWhite       = false
			afterFirst       = false
		)
		// TODO: we don't get a whiteIdent, when a comment spans multiple lines.
		// do we need to handle this case? What is broken currently?
		for v := range t.iterWhite(t.lastPosWritten, rbracePos) {
			switch v.whiteType {
			case whiteWhite:
				if beforeNewline {
					onelineDirective = true
				}
				if !afterFirst {
					firstWhite = true
				}
			case whiteIndent:
				t.lastIndentation = v.text
				beforeNewline = false
			case whiteComment:
				if beforeNewline {
					onelineDirective = true
				}
			case whiteSemi:
				if beforeNewline {
					onelineDirective = true
				}
			default:
				panic("unreachable")
			}
			afterFirst = true
		}
		t.inStaticWrite = false
		t.lineDirectiveMangled = false
		t.writeLineDirective(onelineDirective, !firstWhite, t.lastPosWritten)
	}
}

func (t *transpiler) inspect(n ast.Node) bool {
	t.inStaticWrite = false
	defer func() {
		t.inStaticWrite = false
	}()
	switch n := n.(type) {
	case *ast.BlockStmt:
		// TODO: line directive before this and what about *ast.SwitchStmt and TypeSwitchStmt.
		t.appendFromSource(n.Lbrace + 1)
		t.transpileList(0, -1, n.List)
		t.addLineDirectiveBeforeRbrace(n.Rbrace)
		t.appendFromSource(n.Rbrace + 1)
		return false
	}
	return true
}

func (t *transpiler) writeLineDirective(oneline, addSpace bool, pos token.Pos) {
	p := t.fs.Position(pos + 1)
	if oneline {
		t.appendSource(" /*line ")
	} else {
		t.appendSource("\n//line ")
	}
	if oneline && addSpace {
		p.Column--
	}
	t.appendSource(p.Filename)
	t.appendSource(":")
	t.appendSource(strconv.FormatInt(int64(p.Line), 10))
	t.appendSource(":")
	t.appendSource(strconv.FormatInt(int64(p.Column), 10))
	if oneline && addSpace {
		t.appendSource("*/ ")
	} else if oneline {
		t.appendSource("*/")
	}
}

func (t *transpiler) appendSourceIndented(additionalIndent int, source string) {
	t.wantIndent(additionalIndent)
	t.appendSource(source)
}

func (t *transpiler) appendIndent(b []byte, additionalIndent int) []byte {
	b = append(b, t.lastIndentation...)
	for range additionalIndent {
		b = append(b, '\t')
	}
	return b
}

func (t *transpiler) wantIndent(additionalIndent int) {
	if debug.Verbose {
		fmt.Printf(
			"t.wantIndent(%v): appending %q\n",
			additionalIndent,
			t.lastIndentation+strings.Repeat("\t", additionalIndent),
		)
	}
	t.flushTmp()
	t.out = t.appendIndent(t.out, additionalIndent)
}

func isTgo(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.OpenTagStmt, *ast.EndTagStmt, *ast.AttributeStmt:
		return true
	case *ast.ExprStmt:
		x, isBasicLit := n.X.(*ast.BasicLit)
		_, isTemplate := n.X.(*ast.TemplateLiteralExpr)
		return (isBasicLit && x.Kind == token.STRING) || isTemplate
	}
	return false
}

type scopeState struct {
	beforeLen int
}

func (t *transpiler) scopeStart(additionalIndent int) scopeState {
	beforeLen := len(t.tmp)
	t.tmp = t.appendIndent(t.tmp, additionalIndent)
	t.tmp = append(t.tmp, '{')
	t.scopeRemainingOpenCount++
	return scopeState{
		beforeLen: beforeLen,
	}
}

func (t *transpiler) scopeEnd(s scopeState, additionalIndent int) {
	if t.scopeRemainingOpenCount <= t.forceAllBracesToBeClosedBefore {
		t.tmp = t.appendIndent(t.tmp, additionalIndent)
		t.tmp = append(t.tmp, '}')
		t.forceAllBracesToBeClosedBefore--
	} else {
		if debug.Debug {
			for _, v := range t.tmp[s.beforeLen:] {
				switch v {
				case ' ', '\t', '\n', '{', '}':
				default:
					panic("unreachable")
				}
			}
		}
		t.tmp = t.tmp[:s.beforeLen]
	}
	t.scopeRemainingOpenCount--
}

func (t *transpiler) transpileList(additionalIndent int, lastIndentLine int, list []ast.Stmt) {
	var (
		prev      ast.Node
		bodyScope = make([]scopeState, 0, 16)
	)
	for _, n := range list {
		var (
			onelineDirective     = t.fs.Position(t.lastPosWritten).Line == t.fs.Position(n.Pos()).Line
			beforeNewline        = true
			firstWhite           = false
			afterFirst           = false
			lastNewlineOrNodePos = n.Pos()
		)
		for v := range t.iterWhite(t.lastPosWritten, n.Pos()) {
			switch v.whiteType {
			case whiteWhite:
				if beforeNewline {
					onelineDirective = true
				}
				if !afterFirst {
					firstWhite = true
				}
			case whiteIndent:
				t.lastIndentation = v.text
				beforeNewline = false
				lastNewlineOrNodePos = v.pos
			case whiteComment:
				if beforeNewline {
					onelineDirective = true
				}
			case whiteSemi:
				if beforeNewline {
					onelineDirective = true
				}
			default:
				panic("unreachable")
			}
			afterFirst = true
		}

		if isTgo(n) {
			// Preserve whitespace, comments and semicolons up to last newline (or up to n.Pos()
			// if no newline found between prev and n.).
			if prev != nil && !isTgo(prev) {
				t.appendFromSource(lastNewlineOrNodePos)
			}

			// TODO: we are ingnoring comments between tgo tags.

			// When the current node is a tgo-node, ignore the whitespace
			// the logic below will add the indentation (from t.lastIndentation),
			// when necessary.
			t.lineDirectiveMangled = true
		} else {
			if t.lineDirectiveMangled {
				t.inStaticWrite = false
				t.lineDirectiveMangled = false
				t.writeLineDirective(onelineDirective, !firstWhite, t.lastPosWritten)
				t.appendFromSource(n.Pos())
			}
		}

		switch n := n.(type) {
		case *ast.OpenTagStmt:
			if t.fs.Position(n.Pos()).Line != lastIndentLine {
				additionalIndent = 0
			}
			lastIndentLine = t.fs.Position(n.Pos()).Line

			t.staticWriteIndent(additionalIndent, "<")
			t.staticWriteIndent(additionalIndent, n.Name.Name)

			tagScope := t.scopeStart(additionalIndent)
			t.lastPosWritten = n.Name.End()
			t.transpileList(additionalIndent+1, lastIndentLine, n.Body)

			for v := range t.iterWhite(t.lastPosWritten, n.ClosePos) {
				if v.whiteType == whiteIndent {
					t.lastIndentation = v.text
				} else {
					continue
					// TODO: figure case this out.
					panic("unreachable")
				}
			}

			t.scopeEnd(tagScope, additionalIndent)

			t.staticWriteIndent(additionalIndent, ">")
			bodyScope = append(bodyScope, t.scopeStart(additionalIndent))
			additionalIndent++
			t.lastPosWritten = n.End()
		case *ast.EndTagStmt:
			additionalIndent = max(additionalIndent-1, 0)

			t.scopeEnd(bodyScope[len(bodyScope)-1], additionalIndent)
			bodyScope = bodyScope[:len(bodyScope)-1]

			t.staticWriteIndent(additionalIndent, "</")
			t.staticWriteIndent(additionalIndent, n.Name.Name)
			t.staticWriteIndent(additionalIndent, ">")
			t.lastPosWritten = n.End()
		case *ast.AttributeStmt:
			if t.fs.Position(n.Pos()).Line != lastIndentLine {
				additionalIndent = 0
			}
			lastIndentLine = t.fs.Position(n.Pos()).Line
			if n.Value != nil {
				switch x := n.Value.(type) {
				case *ast.BasicLit:
					t.staticWriteIndent(additionalIndent, " "+n.AttrName.(*ast.Ident).Name+"=")
					if x.Kind == token.STRING {
						t.staticWriteIndent(additionalIndent, x.Value)
					}
				case *ast.TemplateLiteralExpr:
					t.staticWriteIndent(additionalIndent, " "+n.AttrName.(*ast.Ident).Name+"=")
					t.transpileTemplateLiteral(additionalIndent, x)
				}
			} else {
				t.staticWriteIndent(additionalIndent, " "+n.AttrName.(*ast.Ident).Name)
			}
			t.lastPosWritten = n.End()
		case *ast.ExprStmt:
			if t.fs.Position(n.Pos()).Line != lastIndentLine {
				additionalIndent = 0
			}
			lastIndentLine = t.fs.Position(n.Pos()).Line
			if x, ok := n.X.(*ast.BasicLit); ok && x.Kind == token.STRING {
				t.staticWriteIndent(additionalIndent, x.Value)
				t.lastPosWritten = n.End()
			} else if x, ok := n.X.(*ast.TemplateLiteralExpr); ok {
				t.transpileTemplateLiteral(additionalIndent, x)
			} else {
				ast.Inspect(n, t.inspect)
				t.appendFromSource(n.End())
				t.lastPosWritten = n.End()
			}
		case *ast.CaseClause:
			t.appendFromSource(n.Colon + 1)
			t.transpileList(additionalIndent+1, lastIndentLine, n.Body)
		default:
			ast.Inspect(n, t.inspect)
			t.appendFromSource(n.End())
			t.lastPosWritten = n.End()
		}

		prev = n
	}
}

func (t *transpiler) transpileTemplateLiteral(additionalIndent int, x *ast.TemplateLiteralExpr) {
	for i := range x.Parts {
		t.staticWriteIndent(additionalIndent, x.Strings[i])
		t.lastPosWritten = x.Parts[i].Pos()
		t.inStaticWrite = false
		t.dynamicWriteIndent(additionalIndent, x.Parts[i])
	}
	t.staticWriteIndent(additionalIndent, x.Strings[len(x.Strings)-1])
	t.lastPosWritten = x.End()
}

func (t *transpiler) dynamicWriteIndent(additionalIndent int, n ast.Expr) {
	t.wantIndent(additionalIndent)
	t.appendSource("if err := __tgo.DynamicWrite(__tgo_ctx")

	// TODO: comments before n.

	// We have to add a line directive before a comma, because
	// the go parser does not preserve positon of commas,
	// when formatting the comments get moved before the comma.
	// See: https://go.dev/issue/13113
	if t.fs.PositionFor(n.Pos(), false).Line != t.fs.PositionFor(n.Pos()-3, false).Line {
		panic("unreachable")
	}
	// TODO: why -3, not 2?
	t.writeLineDirective(true, false, n.Pos()-3)
	t.appendSource(", ")

	indent := t.lastIndentation
	// TODO: figure out whether t.lineDirectiveMangled behaves right with this.
	ast.Inspect(n, t.inspect)
	t.lastIndentation = indent

	t.appendFromSource(n.End())
	t.appendSource("); err != nil {")
	t.wantIndent(additionalIndent)
	t.appendSource("\treturn err")
	t.wantIndent(additionalIndent)
	t.appendSource("}")
}

func (t *transpiler) staticWriteIndent(additionalIndent int, s string) {
	if t.inStaticWrite {
		t.out = append(t.out, s...)
		return
	}
	t.inStaticWrite = true
	t.wantIndent(additionalIndent)
	t.appendSource("if err := __tgo_ctx.WriteString(`")
	t.appendSource(s)
	t.tmp = append(t.tmp, "`); err != nil {"...)
	t.tmp = t.appendIndent(t.tmp, additionalIndent)
	t.tmp = append(t.tmp, "\treturn err"...)
	t.tmp = t.appendIndent(t.tmp, additionalIndent)
	t.tmp = append(t.tmp, '}')
}
