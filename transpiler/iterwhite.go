package transpiler

import (
	"iter"

	"github.com/mateusz834/tgoast/token"
)

// TODO: comment of *ast.Comment:
// The Text field contains the comment text without carriage returns (\r) that
// may have been present in the source. Because a comment's end position is
// computed using len(Text), the position reported by [Comment.End] does not match the
// true source end position for comments containing carriage returns.

type white uint8

const (
	_ white = iota

	// TODO: what about '\r'?

	whiteWhite   // " ", "\t"
	whiteIndent  // "\n", "\n\t"
	whiteSemi    // ";"
	whiteComment // /*comment*/, // comment
)

type iterWhiteResult struct {
	whiteType white
	pos       token.Pos
	text      string
}

func (i *iterWhiteResult) end() token.Pos {
	return i.pos + token.Pos(len(i.text))
}

func (t *transpiler) iterWhite(start, end token.Pos) iter.Seq[iterWhiteResult] {
	return func(yield func(iterWhiteResult) bool) {
		last := start
		for _, v := range t.f.Comments {
			if v.Pos() < start {
				continue
			}
			if v.Pos() > end {
				break
			}
			for _, v := range v.List {
				if !yieldIndent(t.src, last, v.Pos()-1, yield) {
					return
				}
				if !yield(iterWhiteResult{whiteComment, v.Pos(), v.Text}) {
					return
				}
				last = v.End()
			}
		}
		yieldIndent(t.src, last, end, yield)
	}
}

func yieldIndent(src string, start, end token.Pos, yield func(iterWhiteResult) bool) bool {
	// TODO: start/end might not always start at base == 0 (token.FileSet)
	// new(token.FileSet).File(0).Base()
	var (
		last      = int(start) - 1
		whiteType = whiteWhite
	)
	for i := last; i < int(end); i++ {
		switch src[i] {
		case ';':
			if len(src[last:i]) > 0 {
				if !yield(iterWhiteResult{whiteType, token.Pos(last + 1), src[last:i]}) {
					return false
				}
			}
			if !yield(iterWhiteResult{whiteSemi, token.Pos(i + 1), ";"}) {
				return false
			}
			whiteType = whiteWhite
			last = i + 1
		case '\n':
			if len(src[last:i]) > 0 {
				if !yield(iterWhiteResult{whiteType, token.Pos(last + 1), src[last:i]}) {
					return false
				}
			}
			whiteType = whiteIndent
			last = i
		case ' ', '\t':
			continue
		case '\r':
			panic("TODO")
		default:
			panic("unreachable")
		}
	}
	if len(src[last:end]) > 0 {
		return yield(iterWhiteResult{whiteType, token.Pos(last + 1), src[last:end]})
	}
	return true
}
