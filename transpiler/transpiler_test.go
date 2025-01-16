package transpiler

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	goparser "go/parser"
	goprinter "go/printer"
	goscanner "go/scanner"
	gotoken "go/token"

	"github.com/mateusz834/tgo/analyzer"
	"github.com/tgo-lang/lang/ast"
	"github.com/tgo-lang/lang/format"
	"github.com/tgo-lang/lang/parser"
	"github.com/tgo-lang/lang/printer"
	"github.com/tgo-lang/lang/token"
)

// TODO: transpilation of:
//func A(A){"\{a}\{""}l"}
//func a(a string) { "a" }

//const testSrc = `
//package A//
//import("github.com/mateusz834/tgo")
//func A(tgo.Ctx)error{"\{""}"//
//}
//func A(tgo.Ctx)error{"\{""}"
//func(tgo.Ctx)error{//
//type tgo A
//"\{""}"  }}`

//const testSrc = "package A//\nimport(\"github.com/mateusz834/tgo\") \nfunc A(tgo.Ctx)error{\"\\{\"\"}\"//\n}\nfunc A(tgo.Ctx)error{\"\\{\"\"}\"\nfunc(tgo.Ctx)error{//\ntype tgo A\n\"\\{\"\"}\"  }}"

//const testSrc = "package A\nimport()\nfunc()A()(...A)"

//const testSrc = "package A\n\nimport \"github.com/mateusz834/tgo\"\n\nfunc A(A tgo.Ctx) error { /*\n\t 0\f*/<div></div>\n}\n"

// TODO no semi before ""?
// const testSrc = `package A
// import"github.com/mateusz834/tgo"
// func A(tgo.Ctx)error{<div>
// A:""</div>}
// `

//const testSrc = `package templates
//
//import "github.com/mateusz834/tgo"
//
//func test(tgo.Ctx) error {
//	<div
//	L:
//	>
//	</div>
//}
//`

//// TODO: semi disappeares?
//const testSrc = `package A
//import"github.com/mateusz834/tgo"
//func A(tgo.Ctx)error{
//	<div;>"0"</div>
//}
//`

// TODO: test case.
//const testSrc = `package A
//
//import "github.com/mateusz834/tgo"
//
//func A(_,
//	A tgo.Ctx) error {
//	<div></div>
//}
//`

//const testSrc = `package A
//import."github.com/mateusz834/tgo" /*l*/ ;func A(Ctx)error{
//	DynamicWrite=""
//	"\{""}"
//}
//`

// TODO: wtf is the \x1c, and it works??
//const testSrc = "package test\n\nimport (\n\t\"github.com/mateusz834/tgo\"\n\t\"math\"\n)\n\nvar (\n\tstrVar        string         = \"str\"\n\tinteagerVar   int            = -100\n\tuInteagerVar  uint           = 100\n\tcharVar       rune           = 'r'\n\tunsafeHTMLVar tgo.UnsafeHTML = \"<div></div>\"\n)\n\nconst (\n\tstrTyped        string         = \"str\"\n\tinteagerTyped   int            = -100\n\tuInteagerTyped  uint = 100\n\tcharTyped       rune           = 'r'\n\tunsafeHTMLTyped tgo.UnsafeHTML = \"<div></div>\"\n)\n\nconst (\n\tstr       = \"str\"\n\tinteager  = -100\n\tuInteager = 100\n\tchar      = 'r'\n)\n\nfunc _(tgo.Ctx) error {\n\t\"\\{\"str\"} \\{100} \\{-100} \\{'r'} \\{tgo.UnsafeHTML(\"<div></div>\")}\"\n\t\"\\{strTyped} \\{inteagerTyped} \\{uInteagerTyped} \\{charTyped} \\{unsafeHTMLTyped}\"\n\t\"\\{strVar} \\{inteagerVar} \\{uInteagerVar} \\{charVar} \\{unsafeHTMLVar}\"\n\t\"\\{str} \\{inteager} \\{uInteager} \\{char}\"\n\treturn nil\n}\n\nfunc _(tgo.Ctx) error {\n\t<div\n\t\t@attr=\"\\{\"str\"} \\{100} \\{-100} \\{'r'} \\{tgo.UnsafeHTML(\"<div></div>\")}\"\n\t\t@attr=\"\\{strTyped} \\{inteagerTyped} \\{uInteagerTyped} \\{charTyped} \\{unsafeHTMLTyped}\"\n\t\t@attr=\"\\{strVar} \\{inteagerVar} \\{uInteagerVar} \\{charVar} \\{unsafeHTMLVar}\"\n\t\t@attr=\"\\{str} \\{inteager} \\{uInteager} \\{char}\"\n\t>\n\t</div>\n\treturn nil\n}\n\nfunc _(tgo.Ctx) error {\n\t\"\\{math.MaxUint} \\{math.MaxInt}\"\n\t\"\\{uint(math.MaxUint)} \\{int(math.MaxInt)}\"\n\treturn nil\n}\n\nfunc _[T tgo.DynamicWriteAllowed](_ tgo.Ctx, t T) error {\n\tvar zero T\n\t\"\\{zero} \\{*new(T)} \\{t}\"\n\t<div\n\t\t@attr=\"\\{zero} \\{*new(T)} \x1c{t}\"\n\t>\n\t</div>\n\treturn nil\n}\n\nfunc _[T intstring](_ tgo.Ctx, t T) error {\n\tvar zero T\n\t\"\\{zero} \\{*new(T)} \\{t}\"\n\t<div\n\t\t@attr=\"\\{zero} \\{*new(T)} \\{t}\"\n\t>\n\t</div>\n\treturn nil\n}\n\nfunc _(tgo.Ctx) error {\n\ttype strWrapperType string\n\t\"\\{strWrapperType /* ERROR \"strWrapperType does not satisfy tgo.DynamicWriteAllowed\" */ (\"test\")}\"\n\n\tvar a strWrapperType\n\t\"\\{a /* ERROR \"strWrapperType does not satisfy tgo.DynamicWriteAl\"\\{3.3 /* ERROR \"float64 does not satisfy tgo.DynamicWritto f3, cannot infer T\" */ ()}\"\n\treturn nil\n}\n"

const testSrc = `package test

import "github.com/mateusz834/tgo"

import "math"

func _(tgo.Ctx) error {
	"\{math.MaxUint}"
	return nil
}
`

func TestTest(t *testing.T) {
	fuzzSource(t, "/kadjfa", testSrc)
	return

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "0", testSrc, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		ast.Print(fset, f)
		t.Fatal(err)
	}

	var s strings.Builder
	format.Node(&s, fset, f)
	t.Log(s.String())

	ast.Print(fset, f)

	if err := analyzer.Analyze(fset, f); err != nil {
		t.Fatal(err)
	}

	out := Transpile(f, fset, testSrc)
	t.Logf("transpiled:\n%s", out)
	t.Logf("transpiled:\n%q", out)

	fsetgo := gotoken.NewFileSet()
	_, err = goparser.ParseFile(fsetgo, "transpiled.go", out, goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		if v, ok := err.(goscanner.ErrorList); ok {
			for _, v := range v {
				file := fsetgo.File(1)
				t.Logf("%v: %v", file.PositionFor(file.Pos(v.Pos.Offset), false), v)
			}
		}
		t.Fatalf("goparser.ParseFile(Transpile(src)) = %v; want = <nil>", err)
	}

	//var s strings.Builder
	//goPrinterConfig.Fprint(&s, fsetgo, fgo)
	//t.Logf("formatted:\n%v", s.String())
	//t.Logf("quoted formatted:\n%s", s.String())
}

var (
	update          = flag.Bool("update", false, "")
	printerConfig   = printer.Config{Tabwidth: 8, Mode: printer.UseSpaces | printer.TabIndent}
	goPrinterConfig = goprinter.Config{Tabwidth: 8, Mode: goprinter.UseSpaces | goprinter.TabIndent}
)

func TestTranspile(t *testing.T) {
	const testdata = "./testdata"
	files, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range files {
		if v.IsDir() {
			continue
		}
		t.Run(v.Name(), func(t *testing.T) {
			file := filepath.Join(testdata, v.Name())
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			// TODO: support multiple file inputs (go and tgo) and outputs.

			tgo, transpiled, _ := strings.Cut(string(content), "======\n")

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.tgo", tgo, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}

			fmt := true
			for _, v := range f.Comments {
				for _, v := range v.List {
					if v.Text == "//nofmt" {
						fmt = false
						break
					}
				}
			}

			if err := analyzer.Analyze(fset, f); err != nil {
				for _, v := range err.(analyzer.AnalyzeErrors) {
					t.Log(v)
				}
				t.Fatal(err)
			}

			var s strings.Builder
			printerConfig.Fprint(&s, fset, f)

			if *update {
				if fmt {
					tgo = s.String()
					fset = token.NewFileSet()
					f, err = parser.ParseFile(fset, "test.tgo", tgo, parser.ParseComments|parser.SkipObjectResolution)
					if err != nil {
						t.Fatal(err)
					}
				}
				transpiled = Transpile(f, fset, tgo)
				if err := os.WriteFile(file, []byte(tgo+"======\n"+transpiled), 0660); err != nil {
					t.Fatal(err)
				}
			}

			out := Transpile(f, fset, tgo)

			if fmt && s.String() != tgo {
				t.Fatal("file not formatted (format with -update)")
			}

			gofset := gotoken.NewFileSet()
			gof, err := goparser.ParseFile(gofset, "test.go", out, goparser.ParseComments|goparser.SkipObjectResolution)
			if err != nil {
				t.Logf("source:\n%s", tgo)
				t.Logf("transpiled:\n%s", out)
				t.Fatalf("failed to parse transpiled source: %v", err)
			}

			var goFmted strings.Builder
			goPrinterConfig.Fprint(&goFmted, gofset, gof)
			if fmt && goFmted.String() != out {
				t.Logf("source:\n%s", tgo)
				t.Log(gitDiff(t.TempDir(), out, goFmted.String()))
				t.Fatalf("transpiled output not formatted:\n%s\nwant:\n%s", out, goFmted.String())
			}

			// TODO: type-check output (also listing the errors in the file?)

			if out != transpiled {
				t.Log("make following changes to make this test pass:")
				t.Log(gitDiff(t.TempDir(), out, transpiled))
				t.Fatal("difference found")
			}
		})
	}
}

func gitDiff(tmpDir string, got, expect string) (string, error) {
	gotPath := filepath.Join(tmpDir, "got")
	gotFile, err := os.Create(gotPath)
	if err != nil {
		return "", err
	}
	defer gotFile.Close()
	if _, err := gotFile.WriteString(got); err != nil {
		return "", err
	}

	expectPath := filepath.Join(tmpDir, "expect")
	expectFile, err := os.Create(expectPath)
	if err != nil {
		return "", err
	}
	defer expectFile.Close()
	if _, err := expectFile.WriteString(expect); err != nil {
		return "", err
	}

	var out strings.Builder
	cmd := exec.Command("git", "diff", "-U 100000", "--no-index", "--color=always", "--ws-error-highlight=all", gotPath, expectPath)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && cmd.ProcessState.ExitCode() != 1 {
		return "", err
	}
	return out.String(), nil
}
