package transpiler

import (
	"fmt"
	"html"
	"math"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/mateusz834/tgo/tgofuncs"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/token"
)

const (
	debug   = false
	verbose = false
)

// TODO: what would happen?
//<div
//L:
//>

func Transpile(f *ast.File, fs *token.FileSet, src string) string {
	info := tgofuncs.Check(f)

	tgofuncs := make(map[ast.Node]struct{})
	for _, v := range info.TgoFuncs {
		tgofuncs[v] = struct{}{}
	}

	t := transpiler{
		ctx: &transpilerCtx{
			f:   f,
			fs:  fs,
			src: src,

			tgofuncs: tgofuncs,
			tgoIdent: fileUniqueIdent(f, "__tgo_ctx"),
			info:     info,

			out: slices.Grow([]byte{}, len(src)*2),
		},
		lastIndentation: "\n",
	}
	t.transpile()
	if len(t.ctx.tmp) != 0 {
		panic("unreachable")
	}
	return string(t.ctx.out)
}

type transpilerCtx struct {
	f   *ast.File
	fs  *token.FileSet
	src string

	out []byte
	tmp []byte

	tgofuncs                map[ast.Node]struct{}
	info                    tgofuncs.Info
	tgoIdent                string
	tgoAddtionalImportIdent string

	lastPosWritten token.Pos // last position processed by the transpiler of the src.

	// if set to true, then some generated source was written before, and line
	// mapping would get out of sync when appending from original source,
	// before appending anything from src a line directive needs
	// to be written.
	lineDirectiveMangled bool

	inStaticWrite bool // if true, then inside of a static string write call.

	implicitBlockStmtCount            int
	implicitBlockStmtForceCloseBefore int
}

type transpiler struct {
	ctx              *transpilerCtx
	lastIndentation  string // last indentation found in the source, prefixed with a newline.
	additionalIndent int
	inTgoFunc        bool
}

// posToOffset converts a token.Pos into an source offset in t.ctx.src.
func (t *transpiler) posToOffset(p token.Pos) int {
	return t.ctx.fs.File(t.ctx.f.FileStart).Offset(p)
}

// offsetToPos converts an t.ctx.src source offset into token.Pos.
func (t *transpiler) offsetToPos(off int) token.Pos {
	return t.ctx.fs.File(t.ctx.f.FileStart).Pos(off)
}

func debugPrintf(format string, args ...any) {
	pc, file, line, _ := runtime.Caller(2)
	funcName := ""
	f, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if f.Func != nil {
		funcName = f.Func.Name()
		i := strings.LastIndexByte(funcName, '.')
		if i >= 0 {
			funcName = funcName[i+1:]
		}
	}
	fmt.Printf("%v:%v (%v) "+format+"\n", append([]any{filepath.Base(file), line, funcName}, args...)...)
}

// skipSourceUpTo updates the t.ctx.lastPosWritten to pos.
func (t *transpiler) skipSourceUpTo(pos token.Pos) {
	if verbose {
		pos := t.ctx.fs.Position(pos)
		debugPrintf("skipSourceUpTo(%v:%v (offset: %v))", pos.Line, pos.Column, pos.Offset)
	}

	if t.ctx.lastPosWritten > pos {
		panic("unreachable")
	}

	// We should set lineDirectiveMangled to true here, but currently we should
	// not get here with t.ctx.lineDirectiveMangled == false, so we can assert
	// that for now instead.
	if !t.ctx.lineDirectiveMangled {
		panic("unreachable")
	}

	t.ctx.lastPosWritten = pos
}

// appendFromSource appends t.ctx.src[t.ctx.lastPosWritten:end] into t.ctx.out,
// updating the t.ctx.lastPosWritten to end.
func (t *transpiler) appendFromSource(end token.Pos) {
	if t.ctx.lastPosWritten == end {
		return
	}

	// While appending source from t.ctx.src into t.ctx.out, we
	// need to be sure that the line directive is valid, and was
	// not mangled by any code that we generated.
	if t.ctx.lineDirectiveMangled {
		panic("unreachable")
	}

	// At this point t.ctx.tmp, must be empty, because the t.ctx.lineDirectiveMangled
	// is equal to false (see assert above), which means that a line directive
	// has been written before, which caused the t.ctx.tmp to be flushed.
	// There is one case, where it might fail: call to [writeLineDirective],
	// then [tmpAppendSource] (or [tmpIndent]), then [appendFromSource].
	// This way we would get t.ctx.lineDirectiveMangled == false and len(t.ctx.tmp) != 0,
	// but with the current transpiler, this kind of call combination is not possible.
	// TODO: maybe we should set t.ctx.lineDirectiveMangled = true in [tmpIndent] and [tmpAppendSource]?
	// Wouldn't that cause duplicated line directives? We can "rollback" scopes ([scopeStart], [scopeEnd]).
	if len(t.ctx.tmp) != 0 {
		panic("unreachable")
	}

	src := t.ctx.src[t.posToOffset(t.ctx.lastPosWritten):t.posToOffset(end)]
	if verbose {
		pos := t.ctx.fs.Position(end)
		debugPrintf("appendFromSource(%v:%v (offset: %v)) -> %q", pos.Line, pos.Column, pos.Offset, src)
	}
	t.ctx.out = append(t.ctx.out, src...)
	t.ctx.inStaticWrite = false
	t.ctx.lastPosWritten = end
}

// appendSource appends a string into t.ctx.out, flushing t.ctx.tmp.
// Invalidates the current line directive.
func (t *transpiler) appendSource(s string) {
	t.flushTmp()
	if verbose {
		debugPrintf("appendString(%q)", s)
	}
	t.ctx.out = append(t.ctx.out, s...)
	// TODO: move to flushTmp?
	t.ctx.inStaticWrite = false
	t.ctx.lineDirectiveMangled = true
}

// indent append the current indentation to t.ctx.out, flushing t.tmp.
// Invalidates the current line directive.
func (t *transpiler) indent() {
	t.flushTmp()
	if verbose {
		debugPrintf(
			"indent() -> %q; t.ctx.additionalIndent = %v",
			t.lastIndentation+strings.Repeat("\t", t.additionalIndent),
			t.additionalIndent,
		)
	}
	t.ctx.out = t.appendIndent(t.ctx.out)
	t.ctx.inStaticWrite = false
	t.ctx.lineDirectiveMangled = true
}

func (t *transpiler) appendIndent(b []byte) []byte {
	b = append(b, t.lastIndentation...)
	for range t.additionalIndent {
		b = append(b, '\t')
	}
	return b
}

// tmpAppendSource appends s into t.ctx.tmp.
func (t *transpiler) tmpAppendSource(s string) {
	if verbose {
		debugPrintf("tmpAppendSource(%q)", s)
	}

	t.ctx.tmp = append(t.ctx.tmp, s...)
	if verbose {
		debugPrintf("t.ctx.tmp = %q", t.ctx.tmp)
	}
}

// tmpAppendSource appends the current indentation into t.ctx.tmp.
func (t *transpiler) tmpIndent() {
	if verbose {
		debugPrintf(
			"tmpIndent() -> %q; t.ctx.additionalIndent = %v",
			t.lastIndentation+strings.Repeat("\t", t.additionalIndent),
			t.additionalIndent,
		)
	}

	t.ctx.tmp = t.appendIndent(t.ctx.tmp)
	if verbose {
		debugPrintf("t.ctx.tmp = %q", t.ctx.tmp)
	}
}

// flushTmp flushes pending source inside of t.ctx.tmp into t.ctx.out.
func (t *transpiler) flushTmp() {
	if verbose && len(t.ctx.tmp) != 0 {
		debugPrintf("flushTmp() -> %q", t.ctx.tmp)
	}

	t.ctx.out = append(t.ctx.out, t.ctx.tmp...)
	t.ctx.tmp = t.ctx.tmp[:0]

	// Some source is going to be written soon, so the current scope, is not going
	// to be empty, preserve that information so that all left braces, openned till this
	// point would get closed (see the (*transpiler).scopeEnd method).
	t.ctx.implicitBlockStmtForceCloseBefore = t.ctx.implicitBlockStmtCount
}

type scopeState struct {
	beforeLen int
}

// scopeStart starts a new BlockStmt for open tags and ElementBlockStmts.
// Each scopeStart must have a corresponding scopeEnd call.
func (t *transpiler) scopeStart() scopeState {
	if verbose {
		debugPrintf("scopeStart()")
	}

	// Before every scope start, there must be at least one WriteString call, consider
	// both cases where we need start a new scope (OpenTag and ElementBlockStmt body):
	//
	//	<div
	//		one()
	//	>
	//		two()
	//	</div>
	//
	// Before starting a scope for the one() call, just before that there must be a "<div" write,
	// same for the scope of the two() call, ">" write must exist just before the scope starts.
	//
	// So at this point t.ctx.lineDirectiveMangled must be set to true.
	if !t.ctx.lineDirectiveMangled {
		panic("unreachable")
	}

	beforeLen := len(t.ctx.tmp)
	t.tmpIndent()
	t.tmpAppendSource("{")
	t.ctx.implicitBlockStmtCount++
	return scopeState{beforeLen: beforeLen}
}

// scopeEnd end a scope started by scopeStart.
func (t *transpiler) scopeEnd(s scopeState) {
	if t.ctx.implicitBlockStmtCount <= t.ctx.implicitBlockStmtForceCloseBefore {
		if verbose {
			debugPrintf("scopeEnd() -> keep")
		}

		// Some source has been written bettwen scopeState and scopeEnd calls,
		// so we need to close the BlockStmt with an corresponding right brace.
		//
		// We cannot flush t.ctx.tmp and write the end brace into the t.ctx.out directly, because
		// this will limit our static string concatenation optimization (see [staticWriteIndent]).
		// For example:
		//
		//	func test(tgo.Ctx) error {
		// 		<div>
		// 			_ = "sth"
		// 			"test"
		// 		</div>
		// 	}
		//
		// "test" and </div> can be written in a single WriteString call, appending into t.ctx.out,
		// would cause t.ctx.inStaticWrite to be set to false, which would cause both of the strings
		// be written separately ("test", then "</div>") (see [appendSource] and [staticWriteIndent]).
		//
		// In every other case, the t.ctx.tmp has been already flushed (see [appendSource],
		// [appendFromSource], [indent], [flushTmp]), meaning that the only way len(t.ctx.tmp) > 0
		// is when t.ctx.inStaticWrite is set to true.
		if !t.ctx.inStaticWrite && len(t.ctx.tmp) != 0 {
			panic("unreachable")
		}

		// We do not have to set t.ctx.lineDirectiveMangled to true, since the next operation
		// in the transpiler would set that for us, consider:
		//
		//	func _(tgo.Ctx) error {
		//		<div
		//			one()
		//		>
		//			two()
		//		</div>
		//	}
		//
		// lineDirectiveMangled will be set to true, by the static write call.
		//
		// But for correctness let's do this, maybe it might be usefull for our asserts.
		// At this point we know that this right brace is going to be written, so we know
		// that we indeed mangled the line directive.
		t.ctx.lineDirectiveMangled = true

		t.tmpIndent()
		t.tmpAppendSource("}")
		t.ctx.implicitBlockStmtForceCloseBefore--
	} else {
		if verbose {
			debugPrintf("scopeEnd() -> drop")
		}

		if debug {
			for _, v := range t.ctx.tmp[s.beforeLen:] {
				switch v {
				case ' ', '\t', '\n', '{', '}':
				default:
					panic("unreachable")
				}
			}
		}

		// Nothing has been written between the last scopeState and this
		// scopeEnd call, so it is safe to ignore it, as we don't want to produce
		// empty BlockStmts in the transpiled code.
		t.ctx.tmp = t.ctx.tmp[:s.beforeLen]

		if verbose {
			debugPrintf("t.ctx.tmp = %q", t.ctx.tmp)
		}
	}
	t.ctx.implicitBlockStmtCount--
}

type lineDirective uint8

const (
	_ lineDirective = iota

	lineDirectiveFullLine              // "\n//line :line:col"
	lineDirectiveFullLineAdditonalLine // "\n//line :line:col\n"

	lineDirectiveOneLine        // "/*line :line:col*/"
	lineDirectiveOneLineLSpace  // " /*line :line:col*/"
	lineDirectiveOneLineRSpace  // "/*line :line:col*/ "
	lineDirectiveOneLineLRSpace // " /*line :line:col*/ "

)

// writeLineDirective writes a line directive, in one of the format as provided
// in the ld argument. Sets t.ctx.lineDirectiveMangled to false.
func (t *transpiler) writeLineDirective(ld lineDirective, pos token.Pos) {
	// We should not add a line directive when we already have a valid one.
	if !t.ctx.lineDirectiveMangled {
		panic("unreachable")
	}

	var p token.Position
	switch ld {
	case lineDirectiveOneLineLSpace, lineDirectiveOneLine:
		p = t.ctx.fs.Position(pos)
	case lineDirectiveOneLineRSpace, lineDirectiveOneLineLRSpace:
		p = t.ctx.fs.Position(pos)
		p.Column--

		// Caller requested a space after a oneline line directive, but it is not possible
		// to add one. Column in the line directive must be >= 1. Silently fallback to
		// [lineDirectiveOneLine], this can only happen when the input source was not formatted, so
		// we are fine. For example:
		//
		//	func A(
		//	tgo.Ctx) error {<div></div>}
		//
		// Here we are naming the fist param to "__tgo_ctx", thus we need a line directive
		// for the tgo.Ctx type (at Column == 1).
		if p.Column == 0 {
			p.Column = 1
			ld = lineDirectiveOneLine
		}
	case lineDirectiveFullLine:
		p = t.ctx.fs.Position(pos + 1)

		// We should not produce sources that have FullLine line directives,
		// with Column != 1, like: "//line :1:2".
		if p.Column != 1 {
			panic("unreachable")
		}
	case lineDirectiveFullLineAdditonalLine:
		p = t.ctx.fs.Position(pos + 1)
		if p.Column != 1 {
			panic("unreachable")
		}
		p.Line--
		if p.Line == 0 {
			panic("unreachable")
		}
	default:
		panic("unreachable")
	}

	switch ld {
	case lineDirectiveOneLineLSpace, lineDirectiveOneLineLRSpace:
		t.appendSource(" /*line :")
	case lineDirectiveOneLineRSpace, lineDirectiveOneLine:
		t.appendSource("/*line :")
	default:
		t.appendSource("\n//line :")
	}

	t.appendSource(strconv.FormatInt(int64(p.Line), 10))
	t.appendSource(":")
	t.appendSource(strconv.FormatInt(int64(p.Column), 10))

	switch ld {
	case lineDirectiveOneLineRSpace, lineDirectiveOneLineLRSpace:
		t.appendSource("*/ ")
	case lineDirectiveOneLineLSpace, lineDirectiveOneLine:
		t.appendSource("*/")
	case lineDirectiveFullLineAdditonalLine:
		t.appendSource("\n")
	}

	t.ctx.lineDirectiveMangled = false
}

func (t *transpiler) writeLineDirectiveSkipWhite(ld lineDirective, nodeStartPos token.Pos) {
	if ld == lineDirectiveOneLineLSpace {
		for v := range t.iterWhite(t.ctx.lastPosWritten, nodeStartPos) {
			if v.whiteType == whiteWhite {
				t.skipSourceUpTo(v.end())
				ld = lineDirectiveOneLineLRSpace
			}
			break
		}
	}
	t.writeLineDirective(ld, t.ctx.lastPosWritten)
}

func (t *transpiler) transpile() {
	// Gopls does not format files that are generated.
	// See: https://go.dev/issue/49555
	t.appendSource("// Code generated by tgo - DO NOT EDIT.\n\n")

	t.skipSourceUpTo(t.ctx.f.FileStart)
	t.appendSource("//line ")
	t.appendSource(t.ctx.fs.File(t.ctx.f.FileStart).Name())
	t.appendSource(":1:1\n")
	t.ctx.lineDirectiveMangled = false

	if t.ctx.info.NeedsSpecialTgoImport {
		t.ctx.tgoAddtionalImportIdent = fileUniqueIdent(t.ctx.f, "__tgo")

		added := false
		for _, v := range t.ctx.f.Decls {
			if v, ok := v.(*ast.GenDecl); ok && v.Tok == token.IMPORT {
				i := slices.IndexFunc(v.Specs, func(v ast.Spec) bool {
					path, err := strconv.Unquote(v.(*ast.ImportSpec).Path.Value)
					if err != nil {
						panic(err) // unreachable, AST is valid
					}
					return path == "github.com/mateusz834/tgo"
				})

				if i == -1 {
					continue
				}

				if v.Rparen.IsValid() {
					tgoSpec := v.Specs[i].(*ast.ImportSpec)

					nextPos := v.Rparen
					if len(v.Specs) != i+1 {
						nextPos = v.Specs[i+1].(*ast.ImportSpec).Pos()
					}

					lastIndent := nextPos
					for v := range t.iterWhite(tgoSpec.End(), nextPos) {
						if v.whiteType == whiteIndent {
							lastIndent = v.pos
						}
					}
					t.appendFromSource(lastIndent)

					t.appendSource("\n\t")
					t.appendSource(t.ctx.tgoAddtionalImportIdent)
					t.appendSource(` "github.com/mateusz834/tgo"`)

					ld := lineDirectiveFullLine
					if t.ctx.fs.Position(t.ctx.lastPosWritten+1).Column != 1 {
						ld = lineDirectiveOneLineLRSpace
					}
					t.writeLineDirective(ld, t.ctx.lastPosWritten)
					added = true
					break
				}
			}
		}

		if !added {
			last := -1
			for i, v := range t.ctx.f.Decls {
				if v, ok := v.(*ast.GenDecl); ok && v.Tok == token.IMPORT {
					last = i
				}
			}

			lastImportDecl := t.ctx.f.Decls[last].(*ast.GenDecl)

			// TODO: this cannot panic because of a guard NeedsSpecialTgoImport :).
			nextDecl := t.ctx.f.Decls[last+1]

			lastIndent := nextDecl.Pos()
			for v := range t.iterWhite(lastImportDecl.End(), nextDecl.Pos()) {
				if v.whiteType == whiteIndent {
					lastIndent = v.pos
				}
			}
			t.appendFromSource(lastIndent)

			t.appendSource("\nimport ")
			t.appendSource(t.ctx.tgoAddtionalImportIdent)
			t.appendSource(" \"github.com/mateusz834/tgo\"\n")

			ld := lineDirectiveFullLineAdditonalLine
			if t.ctx.fs.Position(t.ctx.lastPosWritten+1).Column != 1 {
				ld = lineDirectiveOneLineRSpace
			}

			t.writeLineDirective(ld, t.ctx.lastPosWritten)
		}
	}

	ast.Walk(t, t.ctx.f)
	t.appendFromSource(t.ctx.f.FileEnd)

	needsErrorAssert := false
	for v := range t.ctx.tgofuncs {
		switch v := v.(type) {
		case *ast.FuncLit:
			if v, ok := v.Type.Results.List[0].Type.(*ast.Ident); ok && v.Name == "error" {
				needsErrorAssert = true
				break
			}
		case *ast.FuncDecl:
			if v, ok := v.Type.Results.List[0].Type.(*ast.Ident); ok && v.Name == "error" {
				needsErrorAssert = true
				break
			}
		}
	}

	if needsErrorAssert {
		// TODO: import ident should not be static.
		t.appendSource(`
// Assert that no other file in this package overrides the error builtin interface.
var _ = (*tgo.Error)((*error)(nil))
`)
	}
}

func (t *transpiler) tgoFunc(n ast.Node, funcType *ast.FuncType, body *ast.BlockStmt) {
	if body == nil {
		return
	}

	indentation := t.blockIndent(body)

	if _, ok := t.ctx.tgofuncs[n]; ok {
		needsCtx := false
		ast.Inspect(n, func(x ast.Node) bool {
			if isTgo(x, true) {
				needsCtx = true
				return false
			}
			if _, ok := x.(*ast.FuncLit); ok && n != x {
				return false
			}
			return true
		})
		if !needsCtx {
			ast.Walk(&transpiler{
				ctx:             t.ctx,
				lastIndentation: indentation,
				inTgoFunc:       true,
			}, body)
			return
		}

		params := funcType.Params
		param := params.List[0]
		if param.Names == nil {
			t.appendFromSource(param.Type.Pos())
			t.appendSource(t.ctx.tgoIdent)
			t.writeLineDirective(lineDirectiveOneLineLRSpace, t.ctx.lastPosWritten)
			for _, param := range funcType.Params.List[1:] {
				t.appendFromSource(param.Type.Pos())
				t.appendSource("_")
				t.writeLineDirective(lineDirectiveOneLineLRSpace, t.ctx.lastPosWritten)
			}
			t.appendFromSource(params.Closing)
		} else if param.Names[0].Name == "_" {
			t.appendFromSource(param.Names[0].Pos())
			t.appendSource(t.ctx.tgoIdent)
			t.skipSourceUpTo(param.Names[0].End())

			// The code below does not handle:
			//func a(a, /*l*/
			////comment
			//	b int) {
			//}

			// TODO: add test cases. and comments :).
			ld := lineDirectiveOneLineLSpace
			if len(param.Names) > 1 {
				firstNameLine := t.ctx.fs.Position(param.Names[0].Pos()).Line
				secondNameLine := t.ctx.fs.Position(param.Names[1].Pos()).Line
				if firstNameLine != secondNameLine {
					t.ctx.lineDirectiveMangled = false // TODO: fix
					t.appendFromSource(t.ctx.fs.File(t.ctx.f.FileStart).LineStart(firstNameLine+1) - 1)
					t.ctx.lineDirectiveMangled = true // TODO: fix
					ld = lineDirectiveFullLine
				}
			}

			t.writeLineDirective(ld, t.ctx.lastPosWritten)
			t.appendFromSource(params.Closing)
		} else {
			t := &transpiler{
				ctx:             t.ctx,
				lastIndentation: indentation,
				inTgoFunc:       true,
			}
			t.appendFromSource(body.Lbrace + 1)
			t.transpileList(body.List, params.List[0].Names[0].Name)
			if t.ctx.lineDirectiveMangled {
				// The line directive was mangled, by the transpilation of a tgo node.
				// Such as:
				//
				//	"testing" // test
				//	"test"    // test
				//
				// Because we converted these into function calls, the aligment of comments
				// no longer holds true, so we have to skip the trailing space, so that the generated code
				// looks like this (we keep it formatted):
				//
				//	if err := __tgo_ctx.WriteString("testingtest"); err != nil {
				//		return err
				//	} /*line :X:11*/ // test
				//
				// not like:
				//
				//	if err := __tgo_ctx.WriteString("testingtest"); err != nil {
				//	        return err
				//	} /*line :X:8*/    // test
				//
				t.writeLineDirectiveSkipWhite(t.whiteAlg(t.ctx.lastPosWritten, body.Rbrace).ld, body.Rbrace)
			}
			t.appendFromSource(body.Rbrace + 1)
			return
		}
		ast.Walk(&transpiler{
			ctx:             t.ctx,
			lastIndentation: indentation,
			inTgoFunc:       true,
		}, body)
		return
	}
	ast.Walk(&transpiler{
		ctx:             t.ctx,
		lastIndentation: indentation,
		inTgoFunc:       false,
	}, body)
}

func (t *transpiler) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.FuncDecl:
		t.tgoFunc(n, n.Type, n.Body)
		return nil
	case *ast.FuncLit:
		t.tgoFunc(n, n.Type, n.Body)
		return nil
	case *ast.BlockStmt:
		// TODO: line directive before this and what about *ast.SwitchStmt and TypeSwitchStmt.ctx.
		t := &transpiler{
			ctx:             t.ctx,
			lastIndentation: t.blockIndent(n),
			inTgoFunc:       t.inTgoFunc,
		}
		t.appendFromSource(n.Lbrace + 1)
		t.transpileList(n.List, "")
		if t.ctx.lineDirectiveMangled {
			t.writeLineDirectiveSkipWhite(t.whiteAlg(t.ctx.lastPosWritten, n.Rbrace).ld, n.Rbrace)
		}
		t.appendFromSource(n.Rbrace + 1)
		return nil
	}
	return t
}

func isTgo(n ast.Node, inTgoFunc bool) bool {
	switch n := n.(type) {
	case *ast.OpenTag, *ast.AttributeStmt, *ast.ElementBlockStmt:
		return true
	case *ast.EndTag:
		panic("unreachable")
	case *ast.ExprStmt:
		x, isBasicLit := n.X.(*ast.BasicLit)
		_, isTemplate := n.X.(*ast.TemplateLiteralExpr)
		return (isBasicLit && x.Kind == token.STRING && inTgoFunc) || isTemplate
	}
	return false
}

type whiteAlgResult struct {
	ld                   lineDirective
	lastNewlineOrNodePos token.Pos
}

func (t *transpiler) whiteAlg(start, end token.Pos) whiteAlgResult {
	var (
		onelineDirective = t.ctx.fs.Position(start).Line == t.ctx.fs.Position(end).Line

		// Note that the current implementation is wrong in case of a multiline
		// comment, it is not a problem for what we are using it now.
		beforeNewline = true

		firstWhite           = false
		afterFirst           = false
		lastNewlineOrNodePos = start
	)

	for v := range t.iterWhite(start, end) {
		switch v.whiteType {
		case whiteWhite:
			if beforeNewline {
				onelineDirective = true
			}
			if !afterFirst {
				firstWhite = true
			}
		case whiteIndent:
			//t.lastIndentation = v.text
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

	ld := lineDirectiveFullLine
	if onelineDirective {
		ld = lineDirectiveOneLineLRSpace
		if firstWhite {
			ld = lineDirectiveOneLineLSpace
		}
	}

	return whiteAlgResult{
		ld:                   ld,
		lastNewlineOrNodePos: lastNewlineOrNodePos,
	}
}

func unlabel(n ast.Stmt) (ast.Stmt, token.Pos) {
	lastLabelPos := token.NoPos
	for {
		if l, ok := n.(*ast.LabeledStmt); ok {
			n = l.Stmt
			lastLabelPos = l.Colon + 1
			continue
		}
		break
	}
	return n, lastLabelPos
}

func (t *transpiler) transpileList(list []ast.Stmt, name string) {
	for i, n := range list {
		r := t.whiteAlg(t.ctx.lastPosWritten, n.Pos())

		if i == 0 && name != "" {
			var (
				lastCommentEndPos = t.ctx.lastPosWritten
				lastIndent        bool
				lastWhite         bool
			)
			for v := range t.iterWhite(t.ctx.lastPosWritten, n.Pos()) {
				lastIndent = false
				lastWhite = false
				switch v.whiteType {
				case whiteWhite:
					lastWhite = true
				case whiteIndent:
					lastIndent = true
					lastCommentEndPos = v.pos
				case whiteComment:
					lastCommentEndPos = v.end()
				case whiteSemi:
				default:
					panic("unreachable")
				}
			}

			t.appendFromSource(lastCommentEndPos)
			t.indent()
			t.appendSource(t.ctx.tgoIdent)
			t.appendSource(" := ")
			t.appendSource(name)

			if !isTgo(n, t.inTgoFunc) {
				if lastIndent {
					r.ld = lineDirectiveFullLine
				} else if lastWhite {
					r.ld = lineDirectiveOneLine
					t.indent()
				} else {
					r.ld = lineDirectiveOneLineRSpace
					t.indent()
				}
			}
			r.lastNewlineOrNodePos = lastCommentEndPos
		}

		unlabeled, lastLabelEndPos := unlabel(n)
		if lastLabelEndPos.IsValid() {
			if t.ctx.lineDirectiveMangled {
				t.writeLineDirective(r.ld, t.ctx.lastPosWritten)
			}
			t.appendFromSource(lastLabelEndPos)
			if n, ok := unlabeled.(*ast.EmptyStmt); ok && i == len(list)-1 && n.Implicit {
				return
			}
			r = t.whiteAlg(lastLabelEndPos, unlabeled.Pos())
		}

		t.transpileStmt(r, unlabeled)
	}
}

func (t *transpiler) transpileStmt(r whiteAlgResult, n ast.Stmt) {
	if isTgo(n, t.inTgoFunc) {
		// When previous node was non-tgo and now we have a tgo node,
		// preserve whitespace, comments and semicolons up to last newline
		// (or up to n.Pos() if no newline found between prev and n).
		if !t.ctx.lineDirectiveMangled {
			t.appendFromSource(r.lastNewlineOrNodePos)
		}

		// TODO: we are ingnoring comments between tgo tags.

		// When the current node is a tgo-node, ignore the whitespace
		// the logic below will add the indentation (from t.ctx.lastIndentation),
		// when necessary.
	} else {
		if t.ctx.lineDirectiveMangled {
			t.writeLineDirectiveSkipWhite(r.ld, n.Pos())
		}
	}

	switch n := n.(type) {
	case *ast.ElementBlockStmt:
		t.staticWriteIndent("<")
		t.staticWriteIndent(n.OpenTag.Name.Name)

		tagScope := t.scopeStart()
		t.skipSourceUpTo(n.OpenTag.Name.End())

		t.additionalIndent++
		t.transpileList(n.OpenTag.Body, "")
		t.additionalIndent--

		t.scopeEnd(tagScope)

		t.staticWriteIndent(">")
		t.skipSourceUpTo(n.OpenTag.End())

		bodyScope := t.scopeStart()
		t.additionalIndent++
		t.transpileList(n.Body, "")
		t.additionalIndent--
		t.scopeEnd(bodyScope)

		t.staticWriteIndent("</")
		t.staticWriteIndent(n.EndTag.Name.Name)
		t.staticWriteIndent(">")
		t.skipSourceUpTo(n.End())
	case *ast.OpenTag:
		t.staticWriteIndent("<")
		t.staticWriteIndent(n.Name.Name)

		tagScope := t.scopeStart()
		t.skipSourceUpTo(n.Name.End())

		t.additionalIndent++
		t.transpileList(n.Body, "")
		t.additionalIndent--

		t.scopeEnd(tagScope)

		t.staticWriteIndent(">")
		t.skipSourceUpTo(n.End())
	case *ast.EndTag:
		panic("unreachable")
	case *ast.AttributeStmt:
		if n.Value != nil {
			switch x := n.Value.(type) {
			case *ast.BasicLit:
				t.staticWriteIndent(" ")
				t.staticWriteIndent(n.AttrName.(*ast.Ident).Name)
				t.staticWriteIndent("=")
				if x.Kind == token.STRING {
					t.staticWriteIndentGoString(x.Value)
				}
			case *ast.TemplateLiteralExpr:
				t.staticWriteIndent(" " + n.AttrName.(*ast.Ident).Name + "=")
				t.transpileTemplateLiteral(x)
			}
		} else {
			t.staticWriteIndent(" ")
			t.staticWriteIndent(n.AttrName.(*ast.Ident).Name)
		}
		t.skipSourceUpTo(n.End())
	case *ast.ExprStmt:
		if x, ok := n.X.(*ast.BasicLit); ok && x.Kind == token.STRING {
			if t.inTgoFunc {
				t.staticWriteIndentGoString(x.Value)
				t.skipSourceUpTo(n.End())
			} else {
				t.appendFromSource(n.End())
			}
		} else if x, ok := n.X.(*ast.TemplateLiteralExpr); ok {
			t.transpileTemplateLiteral(x)
		} else {
			ast.Walk(t, n)
			t.appendFromSource(n.End())
		}
	case *ast.CaseClause:
		for _, v := range n.List {
			ast.Walk(t, v)
		}
		t.appendFromSource(n.Colon + 1)
		t.transpileList(n.Body, "")
	case *ast.CommClause:
		if n.Comm != nil {
			ast.Walk(t, n.Comm)
		}
		t.appendFromSource(n.Colon + 1)
		t.transpileList(n.Body, "")
	default:
		ast.Walk(t, n)
		t.appendFromSource(n.End())
	}
}

func (t *transpiler) transpileTemplateLiteral(x *ast.TemplateLiteralExpr) {
	for i := range x.Parts {
		if i == 0 {
			t.staticWriteIndentGoString(x.Strings[i] + "\"")
		} else {
			t.staticWriteIndentGoString("\"" + x.Strings[i] + "\"")
		}
		t.dynamicWriteIndent(x, x.Parts[i])
	}
	t.staticWriteIndentGoString("\"" + x.Strings[len(x.Strings)-1])
	t.skipSourceUpTo(x.End())
}

func (t *transpiler) dynamicWriteIndent(x *ast.TemplateLiteralExpr, n *ast.TemplateLiteralPart) {
	t.indent()

	t.appendSource("if err := ")
	if d, ok := t.ctx.info.UsableImportForTemplate[x]; ok {
		if d.DotImport {
			t.appendSource("DynamicWrite(")
		} else {
			t.appendSource(d.ImportIdent)
			t.appendSource(".DynamicWrite(")
		}
	} else if t.ctx.info.NeedsSpecialTgoImport {
		t.appendSource(t.ctx.tgoAddtionalImportIdent)
		t.appendSource(".DynamicWrite(")
	} else {
		panic("unreachable")
	}

	t.appendSource(t.ctx.tgoIdent)
	t.appendSource(", " +
		// We wrap n in parentheses to create a *ast.ParenExpr,
		// because the Go parser does not preserve the position of commas.
		// Without the parentheses, comments get moved before the comma during formatting.
		// See: https://go.dev/issue/13113
		"(",
	)

	t.skipSourceUpTo(n.LBrace + 1)
	lineDirectivePos := t.ctx.lastPosWritten

	// TODO writeLineDirectiveSkipWhite?
	var prev iterWhiteResult
	for v := range t.iterWhite(t.ctx.lastPosWritten, n.X.Pos()) {
		if prev.whiteType != whiteInvalid {
			if prev.whiteType == whiteWhite && prev.text == " " && v.whiteType == whiteComment {
				// Comment prefixed with a space, skip it, so we
				// don't end up with two spaces in a row.
				t.ctx.lastPosWritten++
			}
			break
		}
		prev = v
	}

	t.writeLineDirective(lineDirectiveOneLineLRSpace, lineDirectivePos)

	// TODO: figure out whether t.ctx.lineDirectiveMangled behaves right with this.
	// now we have a panic (assert) in appendFromSource, so it might be right.

	t.appendFromSource(n.X.Pos())
	indent := t.lastIndentation
	ast.Walk(t, n)
	t.lastIndentation = indent
	t.appendFromSource(n.End() - 1)

	t.appendSource(")); err != nil {")
	t.indent()
	t.appendSource("\treturn err")
	t.indent()
	t.appendSource("}")
}

func (t *transpiler) staticWriteIndentGoString(s string) {
	s, err := strconv.Unquote(s)
	if err != nil {
		panic(err) // unreachable, AST is valid
	}
	s = strconv.Quote(html.EscapeString(s))
	t.staticWriteIndent(s[1 : len(s)-1])
}

func (t *transpiler) staticWriteIndent(s string) {
	if verbose {
		debugPrintf("staticWriteIndent(%q); t.ctx.inStaticWrite = %v", s, t.ctx.inStaticWrite)
	}

	if t.ctx.inStaticWrite {
		t.ctx.out = append(t.ctx.out, s...)
		return
	}

	t.indent()
	t.appendSource("if err := ")
	t.appendSource(t.ctx.tgoIdent)
	t.appendSource(".WriteString(\"")
	t.appendSource(s)

	if !t.ctx.lineDirectiveMangled {
		panic("unreachable")
	}

	// TODO: describe why to tmp.
	t.tmpAppendSource("\"); err != nil {")
	t.tmpIndent()
	t.tmpAppendSource("\treturn err")
	t.tmpIndent()
	t.tmpAppendSource("}")

	t.ctx.inStaticWrite = true
}

// blockIndent returns an indentation to be used inside of the provided [*ast.BlockStmt].
func (t *transpiler) blockIndent(b *ast.BlockStmt) string {
	// The most realiable way of getting the indentation of an block statement
	// is to look at the indentation between the last statement and the ending
	// brace and append '\t' to it.
	//
	// We cannot just: return t.lastIndentation + "\t", because of cases like these:
	//
	//	_ = 0 |
	//		func() int {
	//			return 1
	//		}()
	//
	// That is as printed by go/printer. Also printer always adds a newline before
	// an end brace, so following code:
	//
	//	{
	//		_ = 3 /* coomment
	//	ends here */ }
	//
	// is printed as:
	//
	//	{
	//		_ = 3 /* coomment
	//		end here */
	//	}
	//
	// We have to be careful with the way go/parser parses ending labels, in such code:
	//
	//	{
	//	label:
	//	}
	//
	// (*ast.LabeledStmt).End() == (*ast.BlockStmt).End(), the (*ast.LabeledStmt).Stmt is set to
	// &ast.EmptyStmt{End: rbracePos, Implicit: true}, thus End() of the *ast.LabeledStmt, returns
	// the same position as the *ast.BlockStmt.
	// Quote from the Go spec: "a semicolon may be omitted before a closing ")" or "}"."
	// In that case, we ignore the *ast.EmptyStmt, and use the Colon position of the
	// label as the end position.

	start := b.Lbrace + 1
	if len(b.List) != 0 {
		last := b.List[len(b.List)-1]
		for {
			if l, ok := last.(*ast.LabeledStmt); ok {
				if v, ok := l.Stmt.(*ast.EmptyStmt); ok && v.Implicit {
					start = l.Colon + 1
					break
				}
				last = l.Stmt
				continue
			}
			start = last.End()
			break
		}
	}

	lastIndent := ""
	for v := range t.iterWhite(start, b.Rbrace) {
		lastIndent = "" // TODO: why?
		switch v.whiteType {
		case whiteIndent:
			lastIndent = v.text
		}
	}

	if lastIndent != "" {
		return lastIndent + "\t"
	}

	// We have not found an indentation in the BlockStmt this can happen when:
	//
	// - The file is not formatted, we produce a "formatted" output, only when
	//   the input file was also formatted, so this fallback is fine.
	//
	// - The file is formatted, but it contains a oneline function, like
	//
	//		func() int { return 5 }
	//
	//   This would not cause any problem with functions, without any tgo-nodes.
	//   In that cases we don't really care about the indentation, we will just
	//   copy the function as-is to the transpiled output. However, if the oneline
	//   function contained tgo-nodes, that that is problematic, consider a func:
	//
	//    func(tgo.Ctx) error { "test" }
	//
	//    This would've been an issue in this case: (see comment at the beginning
	//    of this function for reference)
	//
	//       _ = 0 |
	//       	func _(tgo.Ctx) error { "test" }
	//
	//    To produce a formatted file we need to add indentation, so the output should look like:
	//
	//       _ = 0 |
	//       	func(__tgo_ctx tgo.Ctx) error {
	//       		if err := __tgo_ctx.WriteString("test"); err != nil {
	//       			return err
	//       		}
	//       	}
	//
	//    The fallback accounts only for one "\t", not two. To avoid this issue (and
	//    possibly others) the tgo printer, does not produce oneline function bodies for
	//    functions containing tgo-nodes, such functions will alvays be multiline.

	return t.lastIndentation + "\t"
}

// fileUniqueIdent return an identifier that is not used throughout the entire
// file. Returns defaultIdent, if it is not used in the file, otherwise an identifier
// based on defaultIdent is generated.
func fileUniqueIdent(f *ast.File, defaultIdent string) string {
	used := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if n.Name == defaultIdent {
				used = true
				return false
			}
		}
		return true
	})
	if !used {
		return defaultIdent
	}

	usedIdents := make(map[string]struct{})
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			usedIdents[n.Name] = struct{}{}
		}
		return true
	})

	for i := range uint(math.MaxUint) {
		ident := defaultIdent + strconv.FormatUint(uint64(i), 10)
		if _, ok := usedIdents[ident]; !ok {
			return ident
		}
	}

	panic("unreachable")
}
