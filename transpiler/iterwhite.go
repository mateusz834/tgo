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
	whiteInvalid white = iota

	whiteWhite   // " ", "\t", "\r", "\t\r "
	whiteIndent  // "\n", "\n\t", "\n\t\r "
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

// iterWhite return a iterator over whitespace (semis, comments, whitespace, newlines)
// found between start and end (exclusive).
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
				if !t.yieldIndent(t.src, last, v.Pos(), yield) {
					return
				}
				if !yield(iterWhiteResult{whiteComment, v.Pos(), v.Text}) {
					return
				}
				last = v.End()
			}
		}
		t.yieldIndent(t.src, last, end, yield)
	}
}

func (t *transpiler) yieldIndent(src string, start, end token.Pos, yield func(iterWhiteResult) bool) bool {
	var (
		lastSrcPos = t.posToOffset(start)
		endSrcPos  = t.posToOffset(end)
		whiteType  = whiteWhite
	)

	for i := lastSrcPos; i < endSrcPos; i++ {
		switch src[i] {
		case ';':
			if len(src[lastSrcPos:i]) > 0 {
				if !yield(iterWhiteResult{whiteType, t.offsetToPos(lastSrcPos), src[lastSrcPos:i]}) {
					return false
				}
			}
			if !yield(iterWhiteResult{whiteSemi, t.offsetToPos(i), ";"}) {
				return false
			}
			whiteType = whiteWhite
			lastSrcPos = i + 1
		case '\n':
			if len(src[lastSrcPos:i]) > 0 {
				if !yield(iterWhiteResult{whiteType, t.offsetToPos(lastSrcPos), src[lastSrcPos:i]}) {
					return false
				}
			}
			whiteType = whiteIndent
			lastSrcPos = i
		case ' ', '\t', '\r':
			continue
		default:
			panic("unreachable")
		}
	}
	if len(src[lastSrcPos:endSrcPos]) > 0 {
		return yield(iterWhiteResult{whiteType, t.offsetToPos(lastSrcPos), src[lastSrcPos:endSrcPos]})
	}
	return true
}
