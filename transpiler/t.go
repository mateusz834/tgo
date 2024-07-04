package transpiler

//type cursor struct {
//	node    ast.Node
//	replace *ast.Stmt
//}
//
//func (c *cursor) replaceParent(n ast.Stmt) {
//	*c.replace = n
//}
//
//type inspector struct {
//}
//
//func (s *inspector) postOrder(f func(cursor)) {
//}
//
//func walk(f func(cursor), n ast.Node) {
//	visitChildren(f, n)
//	f(cursor{node: n})
//}
//
//func walkStmtList(f func(cursor), list []ast.Stmt) {
//	for i, v := range list {
//		visitChildren(f, v)
//		f(cursor{node: v, replace: &list[i]})
//	}
//}
//
//func walkList[T ast.Node](f func(cursor), list []T) {
//	for _, v := range list {
//		visitChildren(f, v)
//		f(cursor{node: v})
//	}
//}
//
//func visitChildren(f func(cursor), node ast.Node) {
//	// walk children
//	// (the order of the cases matches the order
//	// of the corresponding node types in ast.go)
//	switch n := node.(type) {
//	// Comments and fields
//	case *ast.OpenTagStmt:
//		walkStmtList(f, n.Body)
//	case *ast.EndTagStmt:
//		walk(f, n.Name)
//	case *ast.AttributeStmt:
//		walk(f, n.AttrName)
//		walk(f, n.Value)
//	case *ast.TemplateLiteralExpr:
//		walkList(f, n.Parts)
//
//	case *ast.Comment:
//		// nothing to do
//
//	case *ast.CommentGroup:
//		for _, c := range n.List {
//			walk(f, c)
//		}
//
//	case *ast.Field:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		walkList(v, n.Names)
//		if n.Type != nil {
//			walk(f, n.Type)
//		}
//		if n.Tag != nil {
//			walk(f, n.Tag)
//		}
//		if n.Comment != nil {
//			walk(f, n.Comment)
//		}
//
//	case *ast.FieldList:
//		for _, f := range n.List {
//			walk(f, f)
//		}
//
//	// Expressions
//	case *ast.BadExpr, *ast.Ident, *ast.BasicLit:
//		// nothing to do
//
//	case *ast.Ellipsis:
//		if n.Elt != nil {
//			walk(f, n.Elt)
//		}
//
//	case *ast.FuncLit:
//		walk(f, n.Type)
//		walk(f, n.Body)
//
//	case *ast.CompositeLit:
//		if n.Type != nil {
//			walk(f, n.Type)
//		}
//		walkList(v, n.Elts)
//
//	case *ast.ParenExpr:
//		walk(f, n.X)
//
//	case *ast.SelectorExpr:
//		walk(f, n.X)
//		walk(f, n.Sel)
//
//	case *ast.IndexExpr:
//		walk(f, n.X)
//		walk(f, n.Index)
//
//	case *ast.IndexListExpr:
//		walk(f, n.X)
//		for _, index := range n.Indices {
//			walk(f, index)
//		}
//
//	case *ast.SliceExpr:
//		walk(f, n.X)
//		if n.Low != nil {
//			walk(f, n.Low)
//		}
//		if n.High != nil {
//			walk(f, n.High)
//		}
//		if n.Max != nil {
//			walk(f, n.Max)
//		}
//
//	case *ast.TypeAssertExpr:
//		walk(f, n.X)
//		if n.Type != nil {
//			walk(f, n.Type)
//		}
//
//	case *ast.CallExpr:
//		walk(f, n.Fun)
//		walkList(v, n.Args)
//
//	case *ast.StarExpr:
//		walk(f, n.X)
//
//	case *ast.UnaryExpr:
//		walk(f, n.X)
//
//	case *ast.BinaryExpr:
//		walk(f, n.X)
//		walk(f, n.Y)
//
//	case *ast.KeyValueExpr:
//		walk(f, n.Key)
//		walk(f, n.Value)
//
//	// Types
//	case *ast.ArrayType:
//		if n.Len != nil {
//			walk(f, n.Len)
//		}
//		walk(f, n.Elt)
//
//	case *ast.StructType:
//		walk(f, n.Fields)
//
//	case *ast.FuncType:
//		if n.TypeParams != nil {
//			walk(f, n.TypeParams)
//		}
//		if n.Params != nil {
//			walk(f, n.Params)
//		}
//		if n.Results != nil {
//			walk(f, n.Results)
//		}
//
//	case *ast.InterfaceType:
//		walk(f, n.Methods)
//
//	case *ast.MapType:
//		walk(f, n.Key)
//		walk(f, n.Value)
//
//	case *ast.ChanType:
//		walk(f, n.Value)
//
//	// Statements
//	case *ast.BadStmt:
//		// nothing to do
//
//	case *ast.DeclStmt:
//		walk(f, n.Decl)
//
//	case *ast.EmptyStmt:
//		// nothing to do
//
//	case *ast.LabeledStmt:
//		walk(f, n.Label)
//		walk(f, n.Stmt)
//
//	case *ast.ExprStmt:
//		walk(f, n.X)
//
//	case *ast.SendStmt:
//		walk(f, n.Chan)
//		walk(f, n.Value)
//
//	case *ast.IncDecStmt:
//		walk(f, n.X)
//
//	case *ast.AssignStmt:
//		walkList(v, n.Lhs)
//		walkList(v, n.Rhs)
//
//	case *ast.GoStmt:
//		walk(f, n.Call)
//
//	case *ast.DeferStmt:
//		walk(f, n.Call)
//
//	case *ast.ReturnStmt:
//		walkList(v, n.Results)
//
//	case *ast.BranchStmt:
//		if n.Label != nil {
//			walk(f, n.Label)
//		}
//
//	case *ast.BlockStmt:
//		walkList(v, n.List)
//
//	case *ast.IfStmt:
//		if n.Init != nil {
//			walk(f, n.Init)
//		}
//		walk(f, n.Cond)
//		walk(f, n.Body)
//		if n.Else != nil {
//			walk(f, n.Else)
//		}
//
//	case *ast.CaseClause:
//		walkList(v, n.List)
//		walkList(v, n.Body)
//
//	case *ast.SwitchStmt:
//		if n.Init != nil {
//			walk(f, n.Init)
//		}
//		if n.Tag != nil {
//			walk(f, n.Tag)
//		}
//		walk(f, n.Body)
//
//	case *ast.TypeSwitchStmt:
//		if n.Init != nil {
//			walk(f, n.Init)
//		}
//		walk(f, n.Assign)
//		walk(f, n.Body)
//
//	case *ast.CommClause:
//		if n.Comm != nil {
//			walk(f, n.Comm)
//		}
//		walkList(v, n.Body)
//
//	case *ast.SelectStmt:
//		walk(f, n.Body)
//
//	case *ast.ForStmt:
//		if n.Init != nil {
//			walk(f, n.Init)
//		}
//		if n.Cond != nil {
//			walk(f, n.Cond)
//		}
//		if n.Post != nil {
//			walk(f, n.Post)
//		}
//		walk(f, n.Body)
//
//	case *ast.RangeStmt:
//		if n.Key != nil {
//			walk(f, n.Key)
//		}
//		if n.Value != nil {
//			walk(f, n.Value)
//		}
//		walk(f, n.X)
//		walk(f, n.Body)
//
//	// Declarations
//	case *ast.ImportSpec:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		if n.Name != nil {
//			walk(f, n.Name)
//		}
//		walk(f, n.Path)
//		if n.Comment != nil {
//			walk(f, n.Comment)
//		}
//
//	case *ast.ValueSpec:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		walkList(v, n.Names)
//		if n.Type != nil {
//			walk(f, n.Type)
//		}
//		walkList(v, n.Values)
//		if n.Comment != nil {
//			walk(f, n.Comment)
//		}
//
//	case *ast.TypeSpec:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		walk(f, n.Name)
//		if n.TypeParams != nil {
//			walk(f, n.TypeParams)
//		}
//		walk(f, n.Type)
//		if n.Comment != nil {
//			walk(f, n.Comment)
//		}
//
//	case *ast.BadDecl:
//		// nothing to do
//
//	case *ast.GenDecl:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		for _, s := range n.Specs {
//			walk(f, s)
//		}
//
//	case *ast.FuncDecl:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		if n.Recv != nil {
//			walk(f, n.Recv)
//		}
//		walk(f, n.Name)
//		walk(f, n.Type)
//		if n.Body != nil {
//			walk(f, n.Body)
//		}
//
//	// Files and packages
//	case *ast.File:
//		if n.Doc != nil {
//			walk(f, n.Doc)
//		}
//		walk(f, n.Name)
//		walkList(v, n.Decls)
//		// don't walk n.Comments - they have been
//		// visited already through the individual
//		// nodes
//
//	case *ast.Package:
//		for _, v := range n.Files {
//			walk(f, v)
//		}
//
//	default:
//		panic(fmt.Sprintf("ast.Walk: unexpected node type %T", n))
//	}
//}
