package analyzer

import (
	"os"
	"testing"

	"github.com/mateusz834/tgoast/ast"
	"github.com/mateusz834/tgoast/format"
	"github.com/mateusz834/tgoast/parser"
	"github.com/mateusz834/tgoast/scanner"
	"github.com/mateusz834/tgoast/token"
)

const tgosrc = `package main

import "github.com/mateusz834/tgo"

func test(a string) error { <div>"hello"</div>; return nil }

func test(a string) error {
	<div>"hello"; i := 3 </div>
	a := 3
	<div>"hello"</div>
	<div>"hello"; foo(); i:=3 </div>

	<span @hello="lol" @test="test \{a}">
		<div>"hello"</div>
		<div>"hello \{a+a+"test"}"</div>
		<div>"hello \{func() string { if a == "test" {return "testing"}; return a}()}"</div>
	</span>
	<div>func() string { if a == "test" {return "testing"}; return a}()</div>
	<div>a:=2; a:=3;</div>
}
`

func TestTgo(t *testing.T) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "test.tgo", tgosrc, parser.SkipObjectResolution|parser.ParseComments)

	ast.Print(fs, f)

	if err != nil {
		if v, ok := err.(scanner.ErrorList); ok {
			for _, err := range v {
				t.Errorf("%v", err)
			}
		}
		t.Fatalf("%v", err)
	}

	err = Analyze(fs, f)
	if v, ok := err.(AnalyzeErrors); ok {
		for _, v := range v {
			t.Logf("%v", v)
		}
	}
	t.Logf("%v", err)

	err = format.Node(os.Stdout, fs, f)
	if err != nil {
		t.Fatal(err)
	}
}
