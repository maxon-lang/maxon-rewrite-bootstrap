package main

import (
	"fmt"
	"slices"
	"strings"
)

// A node is either a Go struct, which is how a declaration is kept (later phases fill in its semantic fields and other
// modules read them), or a record in its module's tree: 16 bytes of kind, flags, a small auxiliary field, the
// offset of its text and two words, with what does not fit in the words in a block of the tree's list array.

// classifyNodes decides which nodes are records, and checks that a record holds only what a record can.
func (m *model) classifyNodes() {
	for _, name := range m.g.structs {
		if m.nodeByName[name] == nil {
			m.fail(0, "@ast::Structs names '%s', which is no node", name)
		}
	}
	for _, s := range m.sets {
		records := 0
		for _, n := range s.members {
			if n.record {
				records++
			}
		}
		if records != 0 && records != len(s.members) {
			m.fail(s.rule.line, "set '%s' has records and structs as members; a set is one or the other", s.name)
		}
	}
	for _, n := range m.nodes {
		if n.record {
			m.checkRecord(n)
		}
	}
}

// recordSet is the set of the name when its members are records.
func (m *model) recordSet(name string) *set {
	for _, s := range m.sets {
		if s.name == name && len(s.members) > 0 && s.members[0].record {
			return s
		}
	}
	return nil
}

func (m *model) recordNode(name string) *node {
	if n := m.nodeByName[name]; n != nil && n.record {
		return n
	}
	return nil
}

// refersToRecord is the node or set of a field type "*Node", "Set", or the element of "[]*Node" and "[]Set", when it is
// a record, and its name.
func (m *model) refersToRecord(typ string) (name string, isList, ok bool) {
	elem, isList := strings.CutPrefix(typ, "[]")
	elem = strings.TrimPrefix(elem, "*")
	if m.recordNode(elem) != nil || m.recordSet(elem) != nil {
		return elem, isList, true
	}
	return "", false, false
}

// goType is the Go type, in package ast, of a field or a helper's value: a record or a set of records is a view, and
// a list of them a List of views.
func (m *model) goType(typ string) string {
	if name, isList, ok := m.refersToRecord(typ); ok {
		if isList {
			return name + "List"
		}
		return name
	}
	return typ
}

type storage int

const (
	stBool storage = iota
	stEnum
	stText
	stLabel
	stNode
	stList
	stTexts
	stI128
	stWord
)

type fieldPlan struct {
	f  *field
	st storage
	// bit is the flag of a bool, lane the byte of Aux an enum is in, and word the first of the words a field holds.
	bit, lane, word int
	// lits are the texts an enum can be; its value is the index plus one, and 0 is none.
	lits []string
	// elem is the view type a node or a list of nodes holds.
	elem string
}

// plan is where a record's fields are: its words, flags and enum lanes.
type plan struct {
	fields []*fieldPlan
	words  int
}

const (
	maxFlags = 8
	maxLanes = 2
	// maxBlock is the most words a record keeps in a block of the list array.
	maxBlock = 16
)

// allFields are the fields of a node: those of its sets' bases, outermost first, then its own.
func (m *model) allFields(n *node) []*field {
	var chain []*set
	for s := n.set; s != nil; s = s.parent {
		chain = append([]*set{s}, chain...)
	}
	var fields []*field
	for _, s := range chain {
		fields = append(fields, s.base...)
	}
	return append(fields, n.fields...)
}

func (m *model) checkRecord(n *node) { m.planOf(n) }

// textKinds are what the labelled elements of a string field are made of.
func (m *model) textKinds(f *field) (tokens, literals []string) {
	var visit func(e *elem)
	visit = func(e *elem) {
		switch e.kind {
		case ekToken:
			if !slices.Contains(tokens, e.text) {
				tokens = append(tokens, e.text)
			}
		case ekLiteral:
			if !slices.Contains(literals, e.text) {
				literals = append(literals, e.text)
			}
		case ekGroup:
			for _, a := range e.group {
				for _, inner := range a.elems {
					visit(inner)
				}
			}
		}
	}
	for _, e := range f.elems {
		visit(e)
	}
	return tokens, literals
}

func (m *model) planOf(n *node) *plan {
	p := &plan{}
	flags, lanes := 0, 0
	line := n.line
	fail := func(f *field, format string, args ...any) {
		m.fail(max(f.line, line), "record %s: field %s %s", n.name, f.name, fmt.Sprintf(format, args...))
	}
	slots := 0
	for _, f := range m.allFields(n) {
		if f.slot != "" {
			slots++
			if slots > 1 {
				fail(f, "is a second slot; a node has one")
			}
			continue
		}
		if f.init != "" {
			fail(f, "has an initial value, which a record cannot hold")
		}
		fp := &fieldPlan{f: f}
		words := 1
		switch {
		case f.typ == "bool":
			fp.st, fp.bit = stBool, flags
			flags++
			words = 0
		case f.typ == "string":
			tokens, literals := m.textKinds(f)
			switch {
			case len(tokens) == 0 && len(literals) > 0:
				fp.st, fp.lane, fp.lits = stEnum, lanes, literals
				lanes++
				words = 0
			case len(tokens) == 1 && tokens[0] == "Label" && len(literals) == 0:
				fp.st = stLabel
			default:
				for _, t := range tokens {
					if t != "Name" {
						fail(f, "is text made of the token %s; only a Name or a Label can be read back from the module's text", t)
					}
				}
				for _, l := range literals {
					if !isWord(l) {
						fail(f, "is text that can be the literal '%s', which is not a word", l)
					}
				}
				fp.st = stText
			}
		case f.typ == "[]string":
			fp.st = stTexts
		case f.typ == "I128":
			fp.st, words = stI128, 2
		case f.typ == "Word":
			fp.st = stWord
		default:
			name, isList, ok := m.refersToRecord(f.typ)
			if !ok {
				fail(f, "has the type %s, which a record cannot hold: it holds flags, texts, integers, nodes and lists of nodes", f.typ)
			}
			fp.elem = name
			fp.st = stNode
			if isList {
				fp.st = stList
			}
		}
		if flags > maxFlags {
			fail(f, "is the %dth flag; a record has %d", flags, maxFlags)
		}
		if lanes > maxLanes {
			fail(f, "is the %dth choice of literals; a record has %d", lanes, maxLanes)
		}
		fp.word = p.words
		p.words += words
		p.fields = append(p.fields, fp)
	}
	if p.words-1 > maxBlock {
		m.fail(n.line, "record %s holds %d words, more than a block of %d", n.name, p.words, maxBlock+1)
	}
	return p
}

// blockWords is how many words of the list array a record with this plan reserves.
func (p *plan) blockWords() int {
	if p.words <= 2 {
		return 0
	}
	return p.words - 1
}

// word is the Go expression for the word i of the node n: in its record when it has no more than two, and otherwise the first
// in A and the rest in the block that B points to.
func (p *plan) word(i int) string {
	switch {
	case p.words <= 2 && i == 0:
		return "n.T.Records[n.ID].A"
	case p.words <= 2:
		return "n.T.Records[n.ID].B"
	case i == 0:
		return "n.T.Records[n.ID].A"
	}
	return fmt.Sprintf("n.T.Lists[n.T.Records[n.ID].B+%d]", i-1)
}
