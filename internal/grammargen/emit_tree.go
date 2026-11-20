package main

import (
	"fmt"
	"strings"
)

// kindName is the constant of a node's kind.
func kindName(n *node) string { return "Kind" + n.name }

func emitKinds(w *writer, m *model) {
	w.p("// Kind says which node a record is: every node of the grammar has one, and a struct's record in the tree holds only\n")
	w.p("// its kind and where it starts, so that every node has a number and a position.\n")
	w.p("type Kind uint8\n\nconst (\n\tKindNone Kind = iota\n")
	for _, n := range m.nodes {
		w.p("\t%s\n", kindName(n))
	}
	w.p(")\n\nvar kindNames = [...]string{\"none\"")
	for _, n := range m.nodes {
		w.p(", %q", n.name)
	}
	w.p("}\n\nfunc (k Kind) String() string {\n\tif int(k) >= len(kindNames) {\n\t\treturn fmt.Sprintf(\"Kind(%%d)\", int(k))\n\t}\n\treturn kindNames[k]\n}\n\n")
}

// emitRecordSet writes the view of a set of records, the conversions to and from the views of its members, and its
// list type.
func emitRecordSet(w *writer, s *set) {
	w.p("// %s is a view of a node of one of %d kinds: use Kind, or one of the As methods, to tell which.\n", s.name, len(s.members))
	w.p("type %s struct{ Ref }\n\ntype %sList = List[%s]\n\n", s.name, s.name, s.name)
	for _, n := range s.members {
		w.p("func (n %s) %s() %s { return %s{n.Ref} }\n\n", n.name, s.name, s.name, s.name)
		w.p("// To%s is the view of the node as a %s, for code that has found out it is one.\n", n.name, n.name)
		w.p("func (s %s) To%s() %s { return %s{s.Ref} }\n\n", s.name, n.name, n.name, n.name)
		w.p("// As%s is the view of the node as a %s, if it is one.\n", n.name, n.name)
		w.p("func (s %s) As%s() (%s, bool) {\n\tif s.ID == 0 || s.T.Records[s.ID].Kind != %s {\n\t\treturn %s{}, false\n\t}\n\treturn %s{s.Ref}, true\n}\n\n",
			s.name, n.name, n.name, kindName(n), n.name, n.name)
	}
}

func emitRecord(w *writer, m *model, n *node) {
	p := m.planOf(n)
	w.p("// %s is a view of a %s node.\n", n.name, n.name)
	w.p("type %s struct{ Ref }\n\ntype %sList = List[%s]\n\n", n.name, n.name, n.name)
	w.p("func (t *Tree) New%s(start uint32) %s {\n\tid := t.alloc(%s, start)\n", n.name, n.name, kindName(n))
	if p.blockWords() > 0 {
		w.p("\tt.Records[id].B = t.reserve(%d)\n", p.blockWords())
	}
	w.p("\treturn %s{Ref{t, id}}\n}\n\n", n.name)
	flagConst := func(fp *fieldPlan) string { return fmt.Sprintf("flag%s%s", n.name, fp.f.name) }
	for _, fp := range p.fields {
		if fp.st == stBool {
			w.p("const %s uint8 = 1 << %d\n\n", flagConst(fp), fp.bit)
		}
	}
	for _, fp := range p.fields {
		name := fp.f.name
		switch fp.st {
		case stBool:
			w.p("func (n %s) %s() bool { return n.T.Records[n.ID].Flags&%s != 0 }\n\n", n.name, name, flagConst(fp))
			w.p("func (n %s) Set%s() { n.T.Records[n.ID].Flags |= %s }\n\n", n.name, name, flagConst(fp))
		case stEnum:
			table := lowerFirst(n.name) + name + "Texts"
			w.p("var %s = [...]string{\"\"", table)
			for _, l := range fp.lits {
				w.p(", %q", l)
			}
			w.p("}\n\n")
			shift := fp.lane * 8
			w.p("func (n %s) %s() string { return %s[uint8(n.T.Records[n.ID].Aux>>%d)] }\n\n", n.name, name, table, shift)
			w.p("func (n %s) Set%s(text string) {\n\tvar i uint16\n\tswitch text {\n", n.name, name)
			for i, l := range fp.lits {
				w.p("\tcase %q:\n\t\ti = %d\n", l, i+1)
			}
			w.p("\tdefault:\n\t\tpanic(diag.Internal(\"%s.%s cannot be %%q\", text))\n\t}\n", n.name, name)
			w.p("\tr := &n.T.Records[n.ID]\n\tr.Aux = r.Aux&^(0xff<<%d) | i<<%d\n}\n\n", shift, shift)
		case stText, stLabel:
			read := "NameAt"
			if fp.st == stLabel {
				read = "LabelAt"
			}
			w.p("func (n %s) %s() string { return n.T.%s(%s) }\n\n", n.name, name, read, p.word(fp.word))
			w.p("// %sRef is where the text is in the module's text, plus one: 0 is no text.\n", name)
			w.p("func (n %s) %sRef() uint32 { return %s }\n\n", n.name, name, p.word(fp.word))
			w.p("func (n %s) Set%sRef(ref uint32) { %s = ref }\n\n", n.name, name, p.word(fp.word))
		case stNode:
			w.p("func (n %s) %s() %s { return %s{Ref{n.T, NodeID(%s)}} }\n\n", n.name, name, fp.elem, fp.elem, p.word(fp.word))
			w.p("func (n %s) Set%s(v %s) { %s = uint32(v.ID) }\n\n", n.name, name, fp.elem, p.word(fp.word))
		case stList:
			w.p("func (n %s) %s() %sList { return ListAt[%s](n.T, %s) }\n\n", n.name, name, fp.elem, fp.elem, p.word(fp.word))
			w.p("func (n %s) Set%s(l %sList) { %s = l.Ref() }\n\n", n.name, name, fp.elem, p.word(fp.word))
		case stTexts:
			w.p("func (n %s) %s() TextList { return TextListAt(n.T, %s) }\n\n", n.name, name, p.word(fp.word))
			w.p("func (n %s) Set%s(l TextList) { %s = l.Ref() }\n\n", n.name, name, p.word(fp.word))
		case stI128:
			w.p("func (n %s) %s() I128 { return I128{Lo: uint64(%s) | uint64(%s)<<32} }\n\n", n.name, name, p.word(fp.word), p.word(fp.word+1))
			w.p("func (n %s) Set%s(v I128) {\n\tif v.Hi != 0 {\n\t\tpanic(diag.Internal(\"%s.%s holds 64 bits, not %%v\", v))\n\t}\n\t%s = uint32(v.Lo)\n\t%s = uint32(v.Lo >> 32)\n}\n\n",
				n.name, name, n.name, name, p.word(fp.word), p.word(fp.word+1))
		case stWord:
			w.p("func (n %s) %sRef() uint32 { return %s }\n\n", n.name, name, p.word(fp.word))
			w.p("func (n %s) Set%sRef(ref uint32) { %s = ref }\n\n", n.name, name, p.word(fp.word))
		}
	}
}

// emitTreeParts writes what the tree holds for the structs: a chunk list for each, their constructors, and the
// numbering of the slots.
func emitTreeParts(w *writer, m *model) {
	w.p("// structSlabs make the structs of a module's tree, which are few and read by every phase: each takes its record in the\n")
	w.p("// tree for its number and its position, and its struct from a chunk of its type, twice the one before up to a limit.\n")
	w.p("type structSlabs struct {\n")
	for _, n := range m.nodes {
		if !n.record {
			w.p("\t%s slab[%s]\n", lowerFirst(n.name), n.name)
		}
	}
	w.p("}\n\n")
	for _, n := range m.nodes {
		if n.record {
			continue
		}
		w.p("func (t *Tree) New%s(start uint32) *%s {\n\tn := t.%s.next()\n\tn.Mod, n.Off, n.ID = t.Module, start, t.alloc(%s, start)\n",
			n.name, n.name, lowerFirst(n.name), kindName(n))
		for _, f := range m.allFields(n) {
			if f.init != "" {
				w.p("\tn.%s = %s\n", f.name, f.init)
			}
		}
		w.p("\treturn n\n}\n\n")
	}

	w.p("// Slots counts the slots of each table that a phase fills in for a kind of node. A node of a kind that has a slot field is\n")
	w.p("// numbered from the counter of its table in the order of the tree, so a table is a slice that a counter sizes. Slots\n")
	w.p("// count from 1: 0 is no slot.\n")
	w.p("type Slots struct {\n")
	for _, c := range m.slots {
		w.p("\t%s uint32\n", c)
	}
	w.p("}\n\n")
	w.p("// NumberSlots gives each node that has a slot its number in slots, which has an entry for every node and holds zeros: the\n")
	w.p("// slot of a node is slots[id], and the counts say how long each table is. A node is read in the kind it has now, so\n")
	w.p("// numbering after resolution counts a qualified name as a name.\n")
	w.p("func (t *Tree) NumberSlots(slots []uint32) (count Slots) {\n")
	w.p("\tfor id := 1; id < len(t.Records); id++ {\n\t\tswitch t.Records[id].Kind {\n")
	byCounter := map[string][]*node{}
	for _, n := range m.nodes {
		for _, f := range m.allFields(n) {
			if f.slot != "" {
				byCounter[f.slot] = append(byCounter[f.slot], n)
			}
		}
	}
	for _, c := range m.slots {
		var kinds []string
		for _, n := range byCounter[c] {
			kinds = append(kinds, kindName(n))
		}
		if len(kinds) == 0 {
			continue
		}
		w.p("\t\tcase %s:\n\t\t\tcount.%s++\n\t\t\tslots[id] = count.%s\n", strings.Join(kinds, ", "), c, c)
	}
	w.p("\t\t}\n\t}\n\treturn count\n}\n\n")
}
