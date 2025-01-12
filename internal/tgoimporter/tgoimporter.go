package tgoimporter

import (
	"sync"

	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/importer"
	"github.com/tgo-lang/lang/parser"
	"github.com/tgo-lang/lang/token"
	"github.com/tgo-lang/lang/types"
)

const tgoModuleSrc = `package tgo

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

type stringWriter struct {
	io.Writer
}

func (s stringWriter) WriteString(str string) (int, error) {
	// TODO: avoid allocating, with sync.Pool
	return s.Write([]byte(str))
}

type Ctx struct {
	w interface {
		io.Writer
		io.StringWriter
	}
}

func NewCtx(w io.Writer) Ctx {
	if w, ok := w.(interface {
		io.Writer
		io.StringWriter
	}); ok {
		return Ctx{w: w}
	}
	return Ctx{w: stringWriter{w}}
}

func (c *Ctx) Writer() io.Writer {
	return c.w
}

// TODO: this is "unsafe", name should somehow reflect that.
func (c *Ctx) WriteString(s string) error {
	_, err := c.w.WriteString(s)
	return err
}

var (
	htmlQuot = []byte("&#34;")
	htmlApos = []byte("&#39;")
	htmlAmp  = []byte("&amp;")
	htmlLt   = []byte("&lt;")
	htmlGt   = []byte("&gt;")
	htmlNull = []byte("\uFFFD")
)

func (c *Ctx) writeStringEscaped(s string) error {
	last := 0
	for i := 0; i < len(s); i++ {
		var escape []byte
		switch s[i] {
		case '\000':
			escape = htmlNull
		case '"':
			escape = htmlQuot
		case '\'':
			escape = htmlApos
		case '&':
			escape = htmlAmp
		case '<':
			escape = htmlLt
		case '>':
			escape = htmlGt
		default:
			continue
		}
		if err := c.WriteString(s[last:i]); err != nil {
			return err
		}
		if _, err := c.w.Write(escape); err != nil {
			return err
		}
		last = i + 1
	}
	return c.WriteString(s[last:])
}

var pool24 = sync.Pool{
	New: func() any {
		return &[24]byte{}
	},
}

var pool8 = sync.Pool{
	New: func() any {
		return &[8]byte{}
	},
}

func (c *Ctx) writeInt(num int) error {
	if num >= 0 && num < 100 {
		_, err := c.w.WriteString(strconv.FormatInt(int64(num), 10))
		return err
	}
	switch w := c.w.(type) {
	case *strings.Builder:
		_, err := w.Write(strconv.AppendInt(make([]byte, 0, 19), int64(num), 10))
		return err
	case *bytes.Buffer:
		_, err := w.Write(strconv.AppendInt(make([]byte, 0, 19), int64(num), 10))
		return err
	default:
		buf := pool24.Get().(*[24]byte)
		_, err := w.Write(strconv.AppendInt(buf[:0], int64(num), 10))
		pool24.Put(buf)
		return err
	}
}

func (c *Ctx) writeUint(num uint) error {
	if num < 100 {
		_, err := c.w.WriteString(strconv.FormatUint(uint64(num), 10))
		return err
	}
	switch w := c.w.(type) {
	case *strings.Builder:
		_, err := w.Write(strconv.AppendUint(make([]byte, 0, 19), uint64(num), 10))
		return err
	case *bytes.Buffer:
		_, err := w.Write(strconv.AppendUint(make([]byte, 0, 19), uint64(num), 10))
		return err
	default:
		buf := pool24.Get().(*[24]byte)
		_, err := w.Write(strconv.AppendInt(buf[:0], int64(num), 10))
		pool24.Put(buf)
		return err
	}
}

func (c *Ctx) writeRuneEscaped(r rune) error {
	switch r {
	case '&':
		return c.WriteString("&amp;")
	case '\'':
		return c.WriteString("&#39;")
	case '<':
		return c.WriteString("&lt;")
	case '>':
		return c.WriteString("&gt;")
	case '"':
		return c.WriteString("&#34;")
	}

	switch w := c.w.(type) {
	case *strings.Builder:
		_, err := w.WriteRune(r)
		return err
	case *bytes.Buffer:
		_, err := w.WriteRune(r)
		return err
	default:
		buf := pool8.New().(*[8]byte)
		_, err := w.Write(utf8.AppendRune(buf[:0], r))
		pool8.Put(buf)
		return err
	}
}

type Error = error

type UnsafeHTML string

// TODO: change name to TemplateLiteralPart?
// TOOD: rune in an alias of int32, so someone might pass an int32
type DynamicWriteAllowed interface {
	string | rune | int | uint | UnsafeHTML
}

// TODO: some prove (test) that would check wheter DynamicWrite is inlined (or more precisely devirualized).
// manual check is also fine.

// TODO: change name to WriteTemplateLiteralPart?
func DynamicWrite[T DynamicWriteAllowed](ctx Ctx, val T) error {
	switch val := any(val).(type) {
	case string:
		return ctx.writeStringEscaped(val)
	case rune:
		return ctx.writeRuneEscaped(val)
	case int:
		return ctx.writeInt(val)
	case uint:
		return ctx.writeUint(val)
	case UnsafeHTML:
		return ctx.WriteString(string(val))
	default:
		panic("unrechable")
	}
}
`

type TgoDefaultImporter struct {
	I types.ImporterFrom
}

func (f *TgoDefaultImporter) Import(path string) (*types.Package, error) {
	if path == "github.com/mateusz834/tgo" {
		return tgoPkg()
	}
	return f.I.Import(path)
}

func (f *TgoDefaultImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "github.com/mateusz834/tgo" {
		return tgoPkg()
	}
	return f.I.ImportFrom(path, dir, mode)
}

// TODO: test that proves (only for CI) that this is the same as in the tgo repo.
var tgoPkg = sync.OnceValues(func() (*types.Package, error) {
	fset := token.NewFileSet()
	tgoModuleFile, err := parser.ParseFile(fset, "tgo.go", tgoModuleSrc, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	cfg := types.Config{
		Importer: importer.Default(),
	}
	tgoPkg, err := cfg.Check("github.com/tgo-lang/tgo", fset, []*ast.File{tgoModuleFile}, nil)
	if err != nil {
		return nil, err
	}

	return tgoPkg, nil
})
