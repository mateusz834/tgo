package transpiler

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/token"
)

const transpilerDebug = true

func Transpile(f *ast.File, fs *token.FileSet, src string) string {
	t := transpiler{
		f:                    f,
		fs:                   fs,
		src:                  src,
		lastSourcePosWritten: 1,
	}
	t.out = slices.Grow([]byte{}, len(src)*2)
	t.transpile()
	if len(t.tmp) != 0 {
		panic("unreachable")
	}
	return string(t.out)
}

// TODO: track line mapping?
// TODO: and then make a function that automatically inserts the line comment
// only when it is needed.

type lineDirectiveState struct {
	lastPos    token.Pos
	lastTgoPos token.Pos
}

func (l *lineDirectiveState) shouldAddLineDirective(curGoFilePos, curTgoFilePos token.Pos) bool {
	calcTgoPos := l.lastTgoPos + (l.lastPos - curGoFilePos)
	if calcTgoPos < curTgoFilePos {
		panic("unreachable")
	}
	return calcTgoPos > curTgoFilePos
}

type transpiler struct {
	f   *ast.File
	fs  *token.FileSet
	src string

	out []byte
	tmp []byte

	lastSourcePosWritten token.Pos

	staticStartWritten bool
	indentPos          token.Pos

	lastWrittenPos token.Pos

	addedIndent       int
	staticAddedIndent int

	lastIgnored bool

	lds lineDirectiveState
}

func (t *transpiler) addLineDirective(tgoPos token.Pos) {
	if t.lds.shouldAddLineDirective(token.Pos(len(t.out))+1, tgoPos) {
	}
}

func (t *transpiler) transpile() {
	ast.Walk(t, t.f)
	t.appendString(t.src[t.lastSourcePosWritten-1:])
}

func (t *transpiler) transpileBlock(openPos, closePos token.Pos, list []ast.Stmt) {
	singlelineLineDirective := false
	if len(list) != 0 {
		firstPos := list[0].Pos()
		openLine := t.fs.Position(openPos).Line
		if openLine == t.fs.Position(firstPos).Line {
			singlelineLineDirective = true
		} else {
			for _, v := range t.f.Comments {
				if v.Pos() > openPos && v.End() < firstPos {
					if openLine == t.fs.Position(v.Pos()).Line {
						singlelineLineDirective = true
						break
					}
				}
			}
		}
	}

	t.writeSource(t.src[t.lastSourcePosWritten-1 : openPos-1])
	t.lastSourcePosWritten = openPos

	if singlelineLineDirective {
		t.writeSource("{ /*line")
		t.writeSource("file.tgo:11:22")
		t.writeSource("*/ ")
	} else {
		t.writeSource("{\n")
		t.writeSource("//line file.tgo:11:22")
	}

	defer func(v bool) {
		t.lastIgnored = v
	}(t.lastIgnored)

	var (
		lastWhitePos = openPos + 1
		lastOut      = t.out
		lastTmp      = t.tmp
	)

	for _, v := range list {
		t.writeSource(t.src[lastWhitePos-1 : v.Pos()-1])

		t.lastIgnored = false
		ast.Walk(t, v)
		if t.lastIgnored {
			t.out = lastOut
			t.tmp = lastTmp
		}

		lastOut = t.out
		lastTmp = t.tmp
		lastWhitePos = v.End()
	}

	if len(list) == 0 {
		t.writeSource(t.src[openPos:closePos])
	} else {
		t.writeSource(t.src[list[len(list)-1].Pos():closePos])
	}
	t.lastSourcePosWritten = closePos
}

func (t *transpiler) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.BlockStmt:
		t.transpileBlock(n.Pos(), n.End(), n.List)
		return nil
		//if t.staticStartWritten {
		//	t.tmp = append(t.tmp, []byte(t.src[t.lastSourcePosWritten-1:n.Pos()])...)
		//	t.lastSourcePosWritten = n.Pos() + 1
		//}
		//for i, v := range n.List {
		//	ast.Walk(t, v)
		//	switch v := v.(type) {
		//	case *ast.OpenTagStmt:
		//		t.writeSource("\n")
		//		t.writeSource(t.indentAt(v.OpenPos))
		//		next := n.List[i+1]
		//		nextLine := t.fs.Position(next.Pos()).Line
		//		curLine := t.fs.Position(v.End()).Line
		//		if nextLine != curLine {
		//			t.writeSource("{\n")
		//			t.writeSource("//line new.tgo:")
		//			t.writeSource(strconv.FormatInt(int64(n.Pos()), 10))
		//		} else {
		//			t.writeSource("{ /*line new.tgo:")
		//			t.writeSource(strconv.FormatInt(int64(n.Pos()), 10))
		//			t.writeSource("*/ ")
		//		}
		//		t.addedIndent++
		//	case *ast.EndTagStmt:
		//		t.writeSource("\n")
		//		t.writeSource(t.indentAt(v.OpenPos))
		//		t.writeSource("}")
		//		t.addedIndent--
		//	}
		//}
		//if t.staticStartWritten {
		//	t.tmp = append(t.tmp, []byte(t.src[t.lastSourcePosWritten-1:n.End()])...)
		//	t.lastSourcePosWritten = n.End() + 1
		//}
		//return nil
	case *ast.OpenTagStmt:
		if len(n.Body) == 0 {
			t.writeStatic(n.Pos(), "<", n.Name.Name, ">")
			t.lastSourcePosWritten = n.End()
		} else {
			t.writeStatic(n.Pos(), "<", n.Name.Name)
			t.lastSourcePosWritten = n.Name.End()

			//t.writeSource("\n")
			//t.writeSource(t.indentAt(n.OpenPos))
			//t.writeSource("{")

			for _, n := range n.Body {
				ast.Walk(t, n)
			}

			//t.writeSource("\n")
			//t.writeSource(t.indentAt(n.OpenPos))
			//t.writeSource("}")

			t.writeStatic(n.Pos(), ">")
		}
		return nil
	case *ast.EndTagStmt:
		t.writeStatic(n.Pos(), "</", n.Name.Name, ">")
		t.lastSourcePosWritten = n.End()
		return nil
	case *ast.AttributeStmt:
		t.writeStatic(n.Pos(), " ", n.AttrName.(*ast.Ident).Name, "=", n.Value.(*ast.BasicLit).Value)
		return nil
	case *ast.ExprStmt:
		switch n := n.X.(type) {
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				t.writeStatic(n.Pos(), n.Value)
				return nil
			}
		case *ast.TemplateLiteralExpr:
			panic("here")
		}
	}

	t.endStatic()
	return t
}

func (t *transpiler) writeSource(s string) {
	if transpilerDebug {
		fmt.Printf("t.writeSource(%q)\n", s)
	}
	if t.staticStartWritten {
		t.tmp = append(t.tmp, s...)
	} else {
		t.appendString(s)
	}
}

func (t *transpiler) appendString(s string) {
	if transpilerDebug {
		fmt.Printf("t.appendString(%q)\n", s)
		//fmt.Printf("%s\n", debug.Stack())
	}
	t.out = append(t.out, s...)
}

func (t *transpiler) writeStatic(indentPos token.Pos, strs ...string) {
	if !t.staticStartWritten {
		if indentPos > t.lastSourcePosWritten {
			t.appendString(t.src[t.lastSourcePosWritten-1 : indentPos-1])
			t.lastSourcePosWritten = indentPos
			t.indentPos = indentPos
		}
		for range t.addedIndent {
			t.appendString("\t")
		}
		t.staticAddedIndent = t.addedIndent
		t.appendString(`if err := __tgo_ctx.WriteString("`)
		t.staticStartWritten = true
	}
	for _, v := range strs {
		t.appendString(v)
	}
}

func (t *transpiler) endStatic() {
	if t.staticStartWritten {
		indent := t.indentAt(t.indentPos)
		t.appendString("\"); err != nil {\n")
		t.appendString(indent)
		for range t.staticAddedIndent {
			t.appendString("\t")
		}
		t.appendString("\treturn err\n")
		t.appendString(indent)
		for range t.staticAddedIndent {
			t.appendString("\t")
		}
		t.appendString("}")

		t.appendString(string(t.tmp))

		t.tmp = t.tmp[:0]
		t.staticStartWritten = false
	}
}

func (t *transpiler) indentAt(pos token.Pos) string {
	beforePos := t.src[:pos-1]
	i := max(strings.LastIndexByte(beforePos, '\n')+1, 0)

	for j, v := range beforePos[i:] {
		if v == ' ' || v == '\t' {
			continue
		}
		return beforePos[i : i+j]
	}

	return beforePos[i:]
}

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
