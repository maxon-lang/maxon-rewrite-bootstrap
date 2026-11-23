package main

import (
	"fmt"
	"slices"
	"strings"
)

const maxonNodesFile = "../../self/ast/nodes_gen.maxon"

// astQualifier is how package syntax names a type of package ast.
const astQualifier = "ast."

func maxonKindCase(n *node) string { return lowerFirst(n.name) }

// maxonType is the Maxon type of a field or a helper's value of the Go type typ, with the types of package ast
// written after pkg: a record or a set of records is its number, a list of records the reference of its list in the
// tree, and a struct and a list of structs are owned values.
func (m *model) maxonType(typ, pkg string) (string, bool) {
	switch typ {
	case "string":
		return "String", true
	case "bool":
		return "bool", true
	case "I128":
		return pkg + "I128", true
	case "[]string":
		return pkg + "StringList", true
	}
	if _, isList, ok := m.refersToRecord(typ); ok {
		if isList {
			return "diag.U32", true
		}
		return pkg + "NodeID", true
	}
	elem, isList := strings.CutPrefix(typ, "[]")
	if n := m.nodeByName[strings.TrimPrefix(elem, "*")]; n != nil && !n.record && strings.HasPrefix(elem, "*") {
		if isList {
			return pkg + n.name + "List", true
		}
		return pkg + n.name, true
	}
	return "", false
}

// maxonZero is the value a field or a helper's value of the Go type typ starts as: nothing parsed yet.
func (m *model) maxonZero(typ, pkg string) string {
	t, ok := m.maxonType(typ, pkg)
	if !ok {
		m.fail(0, "the type %s has no Maxon type", typ)
	}
	switch {
	case t == "String":
		return `""`
	case t == "bool":
		return "false"
	case t == pkg+"I128":
		return pkg + "I128.fromCount(0 as Count)"
	case t == "diag.U32", t == pkg+"NodeID":
		return "0 as " + t
	case strings.HasSuffix(t, "List"):
		return t + ".create(0)"
	}
	return t + ".make(0 as " + pkg + "NodeID)"
}

// maxonField is the name of a struct's field in Maxon: the label's, or for a label that is a keyword, which no field
// can be named, the label with what the field holds.
func (m *model) maxonField(f *field) string {
	name := lowerFirst(f.name)
	if name == structIDField {
		m.fail(f.line, "field %s has the name of the struct's node", f.name)
	}
	if !slices.Contains(m.keywords, name) {
		return name
	}
	_, isList, records := m.refersToRecord(f.typ)
	switch {
	case f.typ == "bool":
		return "is" + f.name
	case records && !isList:
		return name + "Ref"
	case records || strings.HasPrefix(f.typ, "[]"):
		return name + "List"
	}
	m.fail(f.line, "field %s is a keyword, and its type %s gives it no other name", f.name, f.typ)
	return ""
}

// structIDField is the field of a struct that holds its node in the tree, which says where it starts.
const structIDField = "id"

func (m *model) setByName(name string) *set {
	for _, s := range m.sets {
		if s.name == name {
			return s
		}
	}
	return nil
}

// parsedFields are the fields of a node that its rules assign, in the order of allFields; Maxon has none of those that
// later phases fill in, since its phases keep what they find in tables of their own.
func (m *model) parsedFields(n *node) []*field {
	assigned := map[*field]bool{}
	m.walkElems(n.alts, func(e *elem) {
		if e.label != "" {
			assigned[m.fieldOf(n, e.label, e.line)] = true
		}
	})
	var fields []*field
	for _, f := range m.allFields(n) {
		if assigned[f] {
			fields = append(fields, f)
		}
	}
	return fields
}

// emitMaxonStruct writes the struct of a declaration node: its node in the tree and the fields its rules assign, a
// constructor of the struct with nothing parsed, its list, and the function that makes one for a node starting at start.
func emitMaxonStruct(w *writer, m *model, n *node) {
	fields := m.parsedFields(n)
	w.p("\nexport type %s\n\texport var %s as NodeID\n", n.name, structIDField)
	inits := []string{structIDField + ": " + structIDField}
	for _, f := range fields {
		t, ok := m.maxonType(f.typ, "")
		if !ok {
			m.fail(f.line, "field %s of %s has the type %s, which has no Maxon type", f.name, n.name, f.typ)
		}
		w.p("\texport var %s as %s\n", m.maxonField(f), t)
		inits = append(inits, m.maxonField(f)+": "+m.maxonZero(f.typ, ""))
	}
	w.p("\n\texport static function make(%s NodeID) returns %s\n", structIDField, n.name)
	w.p("\t\treturn %s{%s}\n", n.name, strings.Join(inits, ", "))
	w.p("\tend 'make'\nend '%s'\n", n.name)
	w.p("\nexport typealias %sList = Array with %s\n", n.name, n.name)
	ctor := "new" + n.name
	w.p("\nexport function %s(mutates t Tree, start diag.U32) returns %s\n", ctor, n.name)
	w.p("\treturn %s.make(t.alloc(NodeKind.%s, start: start))\n", n.name, maxonKindCase(n))
	w.p("end '%s'\n", ctor)
}

func maxonWordRead(p *plan, word int) string {
	if word == 0 {
		return "t.records[id as Count].a"
	}
	if p.blockWords() == 0 {
		return "t.records[id as Count].b"
	}
	return fmt.Sprintf("t.blockWordAt(id, offset: %d as Count)", word-1)
}

func maxonWordWrite(p *plan, word int, val string) string {
	if word == 0 {
		return fmt.Sprintf("t.records[id as Count].a = %s", val)
	}
	if p.blockWords() == 0 {
		return fmt.Sprintf("t.records[id as Count].b = %s", val)
	}
	return fmt.Sprintf("t.setBlockWordAt(id, offset: %d as Count, value: %s)", word-1, val)
}

// emitMaxonNodes generates self/ast/nodes_gen.maxon from Maxon.g4.
func emitMaxonNodes(m *model) string {
	w := &writer{}
	w.p("// Code generated by internal/grammargen from %s; DO NOT EDIT.\n", grammarName)
	w.p("export enum NodeKind\n")
	w.p("\tnone\n")
	for _, n := range m.nodes {
		w.p("\t%s\n", maxonKindCase(n))
	}
	w.p("end 'NodeKind'\n")

	w.p("\nexport function isReserved(word String) returns bool\n")
	if len(m.keywords) == 0 {
		w.p("\treturn false\n")
	} else {
		w.p("\treturn match word 'kw'\n")
		for i, k := range m.keywords {
			if i < len(m.keywords)-1 {
				w.p("\t\t%q or\n", k)
			} else {
				w.p("\t\t%q gives true\n", k)
			}
		}
		w.p("\t\tdefault gives false\n")
		w.p("\tend 'kw'\n")
	}
	w.p("end 'isReserved'\n")

	if len(m.slots) > 0 {
		w.p("\nexport type Slots\n")
		for _, c := range m.slots {
			w.p("\texport var %sSlots as diag.U32\n", lowerFirst(c))
		}
		w.p("\n\texport static function zero() returns Slots\n")
		var inits []string
		for _, c := range m.slots {
			inits = append(inits, fmt.Sprintf("%sSlots: 0 as diag.U32", lowerFirst(c)))
		}
		w.p("\t\treturn Slots{%s}\n", strings.Join(inits, ", "))
		w.p("\tend 'zero'\n")
		w.p("end 'Slots'\n")

		byCounter := map[string][]*node{}
		for _, n := range m.nodes {
			for _, f := range m.allFields(n) {
				if f.slot != "" {
					byCounter[f.slot] = append(byCounter[f.slot], n)
				}
			}
		}
		w.p("\nexport function numberSlots(tree Tree, mutates slots U32List) returns Slots\n")
		w.p("\tvar count = Slots.zero()\n")
		w.p("\tfor id in 1 upto tree.records.count() 'each'\n")
		w.p("\t\tlet k = tree.records[id].kind\n")
		for _, c := range m.slots {
			nodes := byCounter[c]
			if len(nodes) == 0 {
				continue
			}
			var conds []string
			for _, n := range nodes {
				conds = append(conds, "k == NodeKind."+maxonKindCase(n))
			}
			field := lowerFirst(c) + "Slots"
			w.p("\t\tif %s 'c_%s'\n", strings.Join(conds, " or "), field)
			w.p("\t\t\tcount.%s = try count.%s + 1 otherwise panic(\"slot overflow\")\n", field, field)
			w.p("\t\t\tslots[id] = count.%s\n", field)
			w.p("\t\tend 'c_%s'\n", field)
		}
		w.p("\tend 'each'\n")
		w.p("\treturn count\n")
		w.p("end 'numberSlots'\n")
	}

	for _, n := range m.nodes {
		if !n.record {
			emitMaxonStruct(w, m, n)
			continue
		}
		p := m.planOf(n)
		ctor := "new" + n.name
		w.p("\nexport function %s(mutates t Tree, start diag.U32) returns NodeID\n", ctor)
		w.p("\tlet id = t.alloc(NodeKind.%s, start: start)\n", maxonKindCase(n))
		if p.blockWords() > 0 {
			w.p("\tlet blk = t.reserve(%d as Count)\n", p.blockWords())
			w.p("\tt.records[id as Count].b = blk\n")
		}
		w.p("\treturn id\n")
		w.p("end '%s'\n", ctor)

		prefix := lowerFirst(n.name)
		for _, fp := range p.fields {
			fname := fp.f.name
			getter := prefix + fname
			setter := "set" + n.name + fname
			switch fp.st {
			case stBool:
				mask := 1 << fp.bit
				w.p("\nexport function %s(t Tree, id NodeID) returns bool\n", getter)
				w.p("\treturn (t.records[id as Count].flags and (%d as Byte)) != 0\n", mask)
				w.p("end '%s'\n", getter)
				w.p("\nexport function %s(mutates t Tree, id NodeID)\n", setter)
				w.p("\tt.records[id as Count].flags = t.records[id as Count].flags or (%d as Byte)\n", mask)
				w.p("end '%s'\n", setter)
			case stEnum:
				shift := fp.lane * 8
				w.p("\nexport function %s(t Tree, id NodeID) returns String\n", getter)
				if shift == 0 {
					w.p("\tlet code = (t.records[id as Count].aux and (255 as U16)) as Count\n")
				} else {
					w.p("\tlet code = ((t.records[id as Count].aux shr %d) and (255 as U16)) as Count\n", shift)
				}
				w.p("\treturn match code 'lit'\n")
				for i, l := range fp.lits {
					w.p("\t\t%d gives %q\n", i+1, l)
				}
				w.p("\t\tdefault gives \"\"\n")
				w.p("\tend 'lit'\n")
				w.p("end '%s'\n", getter)

				w.p("\nexport function %s(mutates t Tree, id NodeID, text String)\n", setter)
				w.p("\tlet code = match text 'lit'\n")
				for i, l := range fp.lits {
					w.p("\t\t%q gives %d as U16\n", l, i+1)
				}
				w.p("\t\tdefault panic(\"invalid enum text\")\n")
				w.p("\tend 'lit'\n")
				if shift == 0 {
					w.p("\tt.records[id as Count].aux = (t.records[id as Count].aux and (65280 as U16)) or code\n")
				} else {
					w.p("\tt.records[id as Count].aux = (t.records[id as Count].aux and (255 as U16)) or (try code shl %d otherwise 0 as U16)\n", shift)
				}
				w.p("end '%s'\n", setter)
			case stText, stLabel:
				readFn := "nameAt"
				if fp.st == stLabel {
					readFn = "labelAt"
				}
				w.p("\nexport function %s(t Tree, id NodeID) returns borrowed String\n", getter)
				w.p("\treturn t.%s(%s)\n", readFn, maxonWordRead(p, fp.word))
				w.p("end '%s'\n", getter)
				w.p("\nexport function %sRef(t Tree, id NodeID) returns diag.U32\n", getter)
				w.p("\treturn %s\n", maxonWordRead(p, fp.word))
				w.p("end '%sRef'\n", getter)
				w.p("\nexport function %sRef(mutates t Tree, id NodeID, ref diag.U32)\n", setter)
				w.p("\t%s\n", maxonWordWrite(p, fp.word, "ref"))
				w.p("end '%sRef'\n", setter)
			case stNode:
				w.p("\nexport function %s(t Tree, id NodeID) returns NodeID\n", getter)
				w.p("\treturn %s as NodeID\n", maxonWordRead(p, fp.word))
				w.p("end '%s'\n", getter)
				w.p("\nexport function %s(mutates t Tree, id NodeID, value NodeID)\n", setter)
				w.p("\t%s\n", maxonWordWrite(p, fp.word, "value as diag.U32"))
				w.p("end '%s'\n", setter)
			case stList, stTexts, stWord:
				w.p("\nexport function %sRef(t Tree, id NodeID) returns diag.U32\n", getter)
				w.p("\treturn %s\n", maxonWordRead(p, fp.word))
				w.p("end '%sRef'\n", getter)
				w.p("\nexport function %sRef(mutates t Tree, id NodeID, ref diag.U32)\n", setter)
				w.p("\t%s\n", maxonWordWrite(p, fp.word, "ref"))
				w.p("end '%sRef'\n", setter)
			case stI128:
				w.p("\nexport function %s(t Tree, id NodeID) returns I128\n", getter)
				w.p("\tlet lo = %s as Count\n", maxonWordRead(p, fp.word))
				w.p("\tlet hi = %s as Count\n", maxonWordRead(p, fp.word+1))
				w.p("\treturn I128.fromCount(lo or (try hi shl 32 otherwise 0 as Count))\n")
				w.p("end '%s'\n", getter)
				w.p("\nexport function %s(mutates t Tree, id NodeID, value I128)\n", setter)
				w.p("\t%s\n", maxonWordWrite(p, fp.word, "(value.lo and (4294967295 as Count)) as diag.U32"))
				w.p("\t%s\n", maxonWordWrite(p, fp.word+1, "((value.lo shr 32) and (4294967295 as Count)) as diag.U32"))
				w.p("end '%s'\n", setter)
			}
		}
	}
	return w.b.String()
}

// writer collects the text of a generated file.
type writer struct{ b strings.Builder }

func (w *writer) p(format string, args ...any) { fmt.Fprintf(&w.b, format, args...) }
