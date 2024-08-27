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
		out: slices.Grow([]byte{}, len(src)*2),

		lastIndentation: "\n",
	}
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

	inStaticWrite bool

	lastIndentation string
	prevIndent      bool

	tmp []byte

	scopeRemainingOpenCount        int
	forceAllBracesToBeClosedBefore int
}

func (t *transpiler) appendSource(s string) {
	if transpilerDebug {
		fmt.Printf("t.appendString(%q)\n", s)
	}
	t.flushTmp()
	t.out = append(t.out, s...)
	t.prevIndent = false
}

func (t *transpiler) appendFromSource(end token.Pos) {
	if transpilerDebug {
		fmt.Printf("t.appendFromSource(%v) -> ", end)
	}
	t.appendSource(t.src[t.lastPosWritten-1 : end-1])
	t.lastPosWritten = end
}

func (t *transpiler) flushTmp() {
	t.out = append(t.out, t.tmp...)
	t.tmp = t.tmp[:0]
	t.forceAllBracesToBeClosedBefore = t.scopeRemainingOpenCount
}

func (t *transpiler) transpile() {
	t.lastPosWritten = t.f.FileStart
	t.appendSource("//line ")
	t.appendSource(t.fs.File(t.f.FileStart).Name())
	t.appendSource(":1:1\n")
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
		// TODO: move this to transpileList ?
		if t.lineDirectiveMangled {
			var (
				onelineDirective = t.fs.Position(t.lastPosWritten).Line == t.fs.Position(n.Rbrace).Line
				beforeNewline    = true
			)
			for v := range t.iterWhite(t.lastPosWritten, n.Rbrace-1) {
				switch v.whiteType {
				case whiteWhite:
					if beforeNewline {
						onelineDirective = true
					}
				case whiteIndent:
					t.lastIndentation = v.text
					t.prevIndent = true
					beforeNewline = false
				case whiteComment:
					t.prevIndent = false
					if beforeNewline {
						onelineDirective = true
					}
				case whiteSemi:
					t.prevIndent = false
					if beforeNewline {
						onelineDirective = true
					}
				default:
					panic("unreachable")
				}
			}
			t.inStaticWrite = false
			t.lineDirectiveMangled = false
			t.writeLineDirective(onelineDirective, t.lastPosWritten)
		}
		t.appendFromSource(n.Rbrace + 1)
		return false
	case *ast.SwitchStmt:
		t.appendFromSource(n.Body.Lbrace + 1)
		t.transpileList(0, -1, n.Body.List)
		t.appendFromSource(n.Body.Rbrace + 1)
		return false
	case *ast.TypeSwitchStmt:
		t.appendFromSource(n.Body.Lbrace + 1)
		t.transpileList(0, -1, n.Body.List)
		t.appendFromSource(n.Body.Rbrace + 1)
		return false
	}
	return true
}

func (t *transpiler) writeLineDirective(oneline bool, pos token.Pos) {
	p := t.fs.Position(pos + 1)
	if oneline {
		t.appendSource(" /*line ")
	} else {
		t.appendSource("\n//line ")
	}
	t.appendSource(p.Filename)
	t.appendSource(":")
	t.appendSource(strconv.FormatInt(int64(p.Line), 10))
	t.appendSource(":")
	t.appendSource(strconv.FormatInt(int64(p.Column), 10))
	if oneline {
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
	t.flushTmp()

	if transpilerDebug {
		if !strings.HasPrefix(t.lastIndentation, "\n") {
			panic("unreachable")
		}
	}

	if !t.prevIndent {
		if transpilerDebug {
			fmt.Printf(
				"t.wantIndent(%v): appending %q\n",
				additionalIndent,
				t.lastIndentation+strings.Repeat("\t", additionalIndent),
			)
			alreadyIndented := false
			for _, v := range t.out[max(bytes.LastIndexByte(t.out, '\n')+1, 0):] {
				if v == ' ' || v == '\t' {
					continue
				}
				alreadyIndented = true
				break
			}
			if !alreadyIndented {
				panic("unreachable")
			}
		}
		t.out = t.appendIndent(t.out, additionalIndent)
		t.prevIndent = true
	} else if transpilerDebug {
		if additionalIndent > 0 {
			// TODO: figure this case out:
			panic("unreachable")
		}
		fmt.Printf("t.wantIndent(): already indented with %q\n", t.lastIndentation)
		if !bytes.HasSuffix(t.out, []byte(t.lastIndentation)) {
			panic("unreachable")
		}
	}
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
		if transpilerDebug {
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
			onelineDirective = t.fs.Position(t.lastPosWritten).Line == t.fs.Position(n.Pos()).Line
			beforeNewline    = true
		)
		for v := range t.iterWhite(t.lastPosWritten, n.Pos()-1) {
			switch v.whiteType {
			case whiteWhite:
				if beforeNewline {
					onelineDirective = true
				}
			case whiteIndent:
				t.lastIndentation = v.text
				t.prevIndent = true
				beforeNewline = false
			case whiteComment:
				t.prevIndent = false
				if beforeNewline {
					onelineDirective = true
				}
			case whiteSemi:
				t.prevIndent = false
				if beforeNewline {
					onelineDirective = true
				}
			default:
				panic("unreachable")
			}
		}

		if isTgo(n) {
			// TODO: we are ingnoring comments.
			// TODO: we are ignoring semis after non-tgo elements (a=3;<div>).

			// When the current node is a tgo-node, ignore the whitespace
			// the logic below will add the indentation (from t.lastIndentation),
			// when necessary.
			t.prevIndent = false
			t.lineDirectiveMangled = true
		} else {
			if t.lineDirectiveMangled {
				t.inStaticWrite = false
				t.lineDirectiveMangled = false
				if v, ok := prev.(*ast.EndTagStmt); ok {
					if t.fs.Position(v.End()).Line == t.fs.Position(n.Pos()).Line {
						t.appendSource(";")
					}
				} else if v, ok := prev.(*ast.EndTagStmt); ok {
					if t.fs.Position(v.End()).Line == t.fs.Position(n.Pos()).Line {
						t.appendSource(";")
					}
				}
				t.writeLineDirective(onelineDirective, t.lastPosWritten)
				t.appendFromSource(n.Pos()) // TODO: is this necessary here?
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

			for v := range t.iterWhite(t.lastPosWritten, n.ClosePos-1) {
				if v.whiteType == whiteIndent {
					t.lastIndentation = v.text
				} else {
					continue
					// TODO: figure case this out.
					panic("unreachable")
				}
			}
			t.prevIndent = false

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
	t.appendSource("if err := __tgo.DynamicWrite(__tgo_ctx, ")
	indent := t.lastIndentation
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
