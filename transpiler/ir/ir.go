package ir

import (
	"github.com/mateusz834/tgoast/token"
)

type Node interface {
	node()
}

type File []Node

type Source struct {
	Source string
	Pos    token.Pos
}

type SourceCodeBlock struct {
	Nodes []Node
}

type StaticWrite struct {
	Strings string
}

type DynamicWrite struct {
	Nodes []Node
	Pos   token.Pos
}

func (n *Source) node()          {}
func (n *StaticWrite) node()     {}
func (n *DynamicWrite) node()    {}
func (n *SourceCodeBlock) node() {}

/*
<div
	sth = "test"
	@attr="value"
	sth = "test2"
></div>
*/
