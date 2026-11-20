package ast

import (
	"iter"
	"math"

	"minimaxon/internal/diag"
)

// Record is a node of a module's tree: 16 bytes, four to a cache line. What the two words hold depends on the kind, and
// grammargen writes the layout of each (a view's accessors are the only code that reads it): a flag per bool, a byte of
// Aux per choice of literals, and a word per child, list, text or half of an integer. A node with more than two words
// keeps the first in A and the rest in a block of the tree's list array, which B points to.
type Record struct {
	Kind  Kind
	Flags uint8
	Aux   uint16
	// Start is the offset in the module's text where the node begins, which is its position.
	Start uint32
	A, B  uint32
}

// Tree is the syntax of one module: its records, the lists and blocks they point into, and the text that its names and
// strings are read from. A NodeID is an index in Records, and 0 is no node. It is made by one parse, by the worker that
// parses the module, and read by every later phase. Resolution changes the kind of a record in place and nothing adds one.
type Tree struct {
	structSlabs
	Module  ModuleID
	Text    string
	Records []Record
	// Lists holds the lists of the tree and the blocks of the records that have more than two words. A list is its count
	// followed by that many words, and is named by the index of its count: 0 names the empty list, which Lists[0] is.
	Lists []uint32
	// Strings are the text parts of string literals that an escape made differ from the text (see StrLit.Parts).
	Strings []string
}

// recordsPerToken and listWordsPerToken size a tree for the tokens of its module: a node starts at a token, and a
// statement and the expression it holds start at the same one, so a module has about this many records and list
// words for each of its tokens.
const (
	recordsPerToken   = 0.5
	listWordsPerToken = 0.6
)

// NewTree makes the tree of a module with the given text and about tokens tokens to parse.
func NewTree(module ModuleID, text string, tokens int) *Tree {
	return &Tree{
		Module:  module,
		Text:    text,
		Records: make([]Record, 1, 1+int(float64(tokens)*recordsPerToken)),
		Lists:   make([]uint32, 1, 1+int(float64(tokens)*listWordsPerToken)),
	}
}

// Count is how many nodes the tree has: their NodeIDs are 1 to Count.
func (t *Tree) Count() int { return len(t.Records) - 1 }

func (t *Tree) alloc(kind Kind, start uint32) NodeID {
	id := NodeID(len(t.Records))
	t.Records = append(t.Records, Record{Kind: kind, Start: start})
	return id
}

// maxBlockWords is the most words a record reserves, which the generator keeps to.
const maxBlockWords = 17

var zeroWords [maxBlockWords]uint32

// reserve makes a block of n zeroed words in the list array and gives its index.
func (t *Tree) reserve(n int) uint32 {
	at := uint32(len(t.Lists))
	t.Lists = append(t.Lists, zeroWords[:n]...)
	return at
}

// AppendList adds a list to the list array and gives the index that names it: 0, the empty list, for no items.
func (t *Tree) AppendList(items []uint32) uint32 {
	if len(items) == 0 {
		return 0
	}
	at := uint32(len(t.Lists))
	t.Lists = append(t.Lists, uint32(len(items)))
	t.Lists = append(t.Lists, items...)
	return at
}

// Ref is what every view holds: the tree and the number of a node in it. It is how code names a node it does not own, and
// it is two words: a view is valid for as long as its tree is.
type Ref struct {
	T  *Tree
	ID NodeID
}

// Viewer is what a type is when it is a view of a node: a Ref and nothing else, so that a list of views can make one.
type Viewer interface{ ~struct{ Ref } }

// IsNil is whether the view names no node, as an optional child that is not there does.
func (r Ref) IsNil() bool { return r.ID == 0 }

// Kind is the kind of the node.
func (r Ref) Kind() Kind { return r.T.Records[r.ID].Kind }

// Position is where the node begins.
func (r Ref) Position() Pos { return Pos{Mod: r.T.Module, Off: r.T.Records[r.ID].Start} }

// Error is a diagnostic at the node.
func (r Ref) Error(code, msg string) *diag.CompileError { return r.Position().Error(code, msg) }

// List is a list of nodes of one type: the part of the tree's list array that holds their numbers.
type List[V Viewer] struct {
	T *Tree
	// Start is the index of the first item, and N is how many there are.
	Start, N uint32
}

// ListAt is the list that an index of the list array names.
func ListAt[V Viewer](t *Tree, ref uint32) List[V] {
	if ref == 0 {
		return List[V]{T: t}
	}
	return List[V]{T: t, Start: ref + 1, N: t.Lists[ref]}
}

// Ref is the index that names the list in the list array, for a record that holds it.
func (l List[V]) Ref() uint32 {
	if l.N == 0 {
		return 0
	}
	return l.Start - 1
}

func (l List[V]) Len() int { return int(l.N) }

// At is the item i of the list.
func (l List[V]) At(i int) V {
	return V(struct{ Ref }{Ref{l.T, NodeID(l.T.Lists[l.Start+uint32(i)])}})
}

// IDs are the numbers of the items.
func (l List[V]) IDs() []uint32 { return l.T.Lists[l.Start : l.Start+l.N] }

// Prefix is the first n items.
func (l List[V]) Prefix(n int) List[V] { return List[V]{T: l.T, Start: l.Start, N: uint32(n)} }

// All is the items in order, with their indexes.
func (l List[V]) All() iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		for i := range int(l.N) {
			if !yield(i, l.At(i)) {
				return
			}
		}
	}
}

// TextList is a list of names that the module's text holds.
type TextList struct {
	T        *Tree
	Start, N uint32
}

// TextListAt is the list of texts that an index of the list array names.
func TextListAt(t *Tree, ref uint32) TextList {
	if ref == 0 {
		return TextList{T: t}
	}
	return TextList{T: t, Start: ref + 1, N: t.Lists[ref]}
}

func (l TextList) Ref() uint32 {
	if l.N == 0 {
		return 0
	}
	return l.Start - 1
}

func (l TextList) Len() int { return int(l.N) }

// At is the name i of the list.
func (l TextList) At(i int) string { return l.T.NameAt(l.T.Lists[l.Start+uint32(i)]) }

// Strings are the names, as a slice that is made for the call.
func (l TextList) Strings() []string {
	out := make([]string, l.N)
	for i := range out {
		out[i] = l.At(i)
	}
	return out
}

// TextRef is what a record holds for a name or a label that starts at offset in the module's text: one more than the
// offset, so that 0 can mean there is none.
func TextRef(offset uint32) uint32 { return offset + 1 }

var identPart = func() (t [256]bool) {
	for c := range 256 {
		t[c] = c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	return t
}()

// NameAt is the name that a record's text reference names: the identifier that starts there. A name is as long as the
// identifier characters that run on from where it starts, so a record needs no length for it.
func (t *Tree) NameAt(ref uint32) string {
	if ref == 0 {
		return ""
	}
	start := int(ref - 1)
	end := start
	for end < len(t.Text) && identPart[t.Text[end]] {
		end++
	}
	return t.Text[start:end]
}

// LabelAt is the label that a record's text reference names: the text from where it starts up to the closing quote. A
// label has no quote inside it.
func (t *Tree) LabelAt(ref uint32) string {
	if ref == 0 {
		return ""
	}
	start := int(ref - 1)
	end := start
	for end < len(t.Text) && t.Text[end] != '\'' {
		end++
	}
	return t.Text[start:end]
}

// StrPart is a part of a string literal: a run of its text, or an interpolated expression, which is when Expr is not nil.
type StrPart struct {
	Text string
	Expr Expr
}

// The first word of a string part says what it is: 0 for an expression, whose number is the second word; this for text
// that an escape made differ from the module's text, which is Tree.Strings[the second word]; and otherwise one more
// than the length of a run of the module's text that starts at the second word.
const decodedPart = math.MaxUint32

// StrParts are the parts of a string literal, two words each in the list array.
type StrParts struct {
	T        *Tree
	Start, N uint32
}

// StrPartsAt is the parts that an index of the list array names.
func StrPartsAt(t *Tree, ref uint32) StrParts {
	if ref == 0 {
		return StrParts{T: t}
	}
	return StrParts{T: t, Start: ref + 1, N: t.Lists[ref]}
}

func (p StrParts) Len() int { return int(p.N) }

// At is the part i.
func (p StrParts) At(i int) StrPart {
	w := p.T.Lists[p.Start+2*uint32(i):]
	switch w[0] {
	case 0:
		return StrPart{Expr: Expr{Ref{p.T, NodeID(w[1])}}}
	case decodedPart:
		return StrPart{Text: p.T.Strings[w[1]]}
	}
	return StrPart{Text: p.T.Text[w[1] : w[1]+w[0]-1]}
}

// All is the parts in order, with their indexes.
func (p StrParts) All() iter.Seq2[int, StrPart] {
	return func(yield func(int, StrPart) bool) {
		for i := range int(p.N) {
			if !yield(i, p.At(i)) {
				return
			}
		}
	}
}

// IsPlain is whether the literal has no interpolation.
func (p StrParts) IsPlain() bool {
	for i := range int(p.N) {
		if p.T.Lists[p.Start+2*uint32(i)] == 0 {
			return false
		}
	}
	return true
}

// StrPartsBuilder makes the parts of a string literal in a tree.
type StrPartsBuilder struct{ words []uint32 }

// Reset empties the builder to make the parts of another literal.
func (b *StrPartsBuilder) Reset() { b.words = b.words[:0] }

// Run adds a run of the module's text, from offset to offset+length.
func (b *StrPartsBuilder) Run(offset, length uint32) { b.words = append(b.words, length+1, offset) }

// Decoded adds text that is not the module's text, as a string with an escape in it.
func (b *StrPartsBuilder) Decoded(t *Tree, text string) {
	t.Strings = append(t.Strings, text)
	b.words = append(b.words, decodedPart, uint32(len(t.Strings)-1))
}

// Interpolation adds an interpolated expression.
func (b *StrPartsBuilder) Interpolation(e Expr) { b.words = append(b.words, 0, uint32(e.ID)) }

// Finish adds the parts to the tree and gives the index that names them.
func (b *StrPartsBuilder) Finish(t *Tree) uint32 {
	at := uint32(len(t.Lists))
	t.Lists = append(t.Lists, uint32(len(b.words)/2))
	t.Lists = append(t.Lists, b.words...)
	return at
}

// Parts are the parts of the literal.
func (n StrLit) Parts() StrParts { return StrPartsAt(n.T, n.PartsRef()) }

// IsPlain is whether the literal has no interpolation.
func (n StrLit) IsPlain() bool { return n.Parts().IsPlain() }

// Value is the integer the literal writes, with its sign.
func (n IntLit) Value() I128 {
	if n.Negative() {
		return n.Magnitude().Neg()
	}
	return n.Magnitude()
}

// Form is whether the pattern is a literal, a range or a case.
func (n Pattern) Form() PatternKind {
	switch {
	case n.CaseName() != "":
		return PatternCase
	case !n.High().IsNil():
		return PatternRange
	}
	return PatternLiteral
}

// Form is what the arm does when it matches: it runs a statement, gives a value or panics.
func (n MatchArm) Form() ArmKind {
	switch {
	case !n.Action().IsNil():
		return ArmThen
	case n.Gives():
		return ArmGives
	}
	return ArmPanic
}

// BecomeName rewrites the member in place as the name of the declaration that a path of modules ends at, which is what
// resolution makes of 'geometry.units.scale': the name is the last of the path and it begins where the path does, at head.
// The node keeps its number, so what holds it, such as the member that goes on to name a field of it, still does.
func (n MemberExpr) BecomeName(head NameExpr) NameExpr {
	name := n.NameRef()
	n.T.Records[n.ID] = Record{Kind: KindNameExpr, Start: n.T.Records[head.ID].Start}
	renamed := NameExpr{n.Ref}
	renamed.SetNameRef(name)
	return renamed
}

// AppendCaptureRefs makes the nodes that stand for a name in the enclosing functions, one for each behaviour around the
// name, which is what a behaviour that takes the variable the name uses moves in (see Capture.Ref). They are copies of the
// name, kept after the last node, and the name holds the first: CaptureRef is the one for the behaviour at depth, counted
// from the outermost, from 0. Resolution makes them, because it is the phase that knows which names a behaviour can take,
// and no phase after it adds a node.
func (t *Tree) AppendCaptureRefs(use NameExpr, depth int) {
	first := NodeID(len(t.Records))
	for range depth {
		copyOf := t.alloc(KindNameExpr, t.Records[use.ID].Start)
		t.Records[copyOf].A = t.Records[use.ID].A
	}
	t.Records[use.ID].B = uint32(first)
}

// CaptureRef is the node that stands for the name in the function around the behaviour at depth (0 for the outermost
// behaviour around it), which the name's own behaviour takes the variable through.
func (n NameExpr) CaptureRef(depth int) NameExpr {
	first := n.T.Records[n.ID].B
	if first == 0 {
		panic(diag.Internal("resolution made no capture references for the name '%s' at %d", n.Name(), n.Position().Off))
	}
	return NameExpr{Ref{n.T, NodeID(first) + NodeID(depth)}}
}
