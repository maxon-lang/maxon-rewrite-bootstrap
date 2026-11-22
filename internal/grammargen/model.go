package main

import (
	"fmt"
	"slices"
	"strings"
)

type ruleKind int

const (
	rkNode ruleKind = iota
	rkSet
	rkHelper
)

// model is the grammar resolved into what the generator emits: tokens, keywords, sets and nodes.
type model struct {
	g           *grammar
	ruleByName  map[string]*rule
	kind        map[*rule]ruleKind
	tokenByName map[string]*tokenDef
	punctuation map[string]*tokenDef
	keywords    []string
	sets        []*set
	setOfRule   map[*rule]*set
	nodes       []*node
	nodeOfRule  map[*rule]*node
	nodeByName  map[string]*node
	astByName   map[string]*astBlock
	leftRec     map[*rule]bool
	// slots are the counters of the slot fields, in the order the nodes first have them.
	slots []string
}

type set struct {
	name   string
	rule   *rule
	parent *set
	base   []*field
	// members are every node in the set, nested sets included, in grammar order; the visitors list them.
	members []*node
}

type node struct {
	name   string
	rule   *rule
	alts   []*alt
	set    *set
	fields []*field
	line   int
	// record is whether the node is a record in the module's tree, as opposed to a Go struct (see @ast::Structs).
	record bool
}

type field struct {
	name string
	typ  string
	init string
	gap  bool
	// slot names the counter of a field declared `Slot[Name]`: the arena gives each node that has the field the next
	// number of that counter, so a table a phase fills in for those nodes is a slice the counter sizes. The field is a
	// uint32.
	slot string
	line int
	// elems are the labelled elements that assign the field, which say what it is made of: a name, a label or a
	// choice of literals.
	elems []*elem
}

func (m *model) fail(line int, format string, args ...any) {
	panic(&grammarError{m.g.file, line, fmt.Sprintf(format, args...)})
}

func capitalize(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func buildModel(g *grammar) *model {
	m := &model{
		g:           g,
		ruleByName:  map[string]*rule{},
		kind:        map[*rule]ruleKind{},
		tokenByName: map[string]*tokenDef{},
		punctuation: map[string]*tokenDef{},
		setOfRule:   map[*rule]*set{},
		nodeOfRule:  map[*rule]*node{},
		nodeByName:  map[string]*node{},
		astByName:   map[string]*astBlock{},
		leftRec:     map[*rule]bool{},
	}
	for _, t := range g.tokens {
		if m.tokenByName[t.name] != nil {
			m.fail(t.line, "token '%s' is declared twice", t.name)
		}
		m.tokenByName[t.name] = t
		if t.literal != "" {
			m.punctuation[t.literal] = t
		}
	}
	for _, r := range g.rules {
		if m.ruleByName[r.name] != nil {
			m.fail(r.line, "rule '%s' is declared twice", r.name)
		}
		m.ruleByName[r.name] = r
	}
	for _, b := range g.astBlocks {
		if m.astByName[b.name] != nil {
			m.fail(b.line, "@ast::%s is declared twice", b.name)
		}
		m.astByName[b.name] = b
	}
	m.checkReferences()
	m.classify()
	m.checkPredicates()
	m.collectKeywords()
	m.expandKeywordSets()
	m.buildSets()
	m.buildNodes()
	m.checkAstBlocks()
	m.collectSlots()
	m.classifyNodes()
	return m
}

// A field says it holds a slot in its type: Slot[Counter].
const slotTypePrefix, slotTypeSuffix = "Slot[", "]"

// collectSlots turns each field declared `Slot[Counter]` into a uint32 that the arena numbers from that counter.
func (m *model) collectSlots() {
	take := func(f *field) {
		counter, ok := strings.CutPrefix(f.typ, slotTypePrefix)
		if !ok {
			return
		}
		counter, ok = strings.CutSuffix(counter, slotTypeSuffix)
		if !ok || counter == "" {
			m.fail(f.line, "the type of field %s is %s; a slot is written Slot[Counter]", f.name, f.typ)
		}
		f.slot, f.typ = counter, "uint32"
		if !slices.Contains(m.slots, counter) {
			m.slots = append(m.slots, counter)
		}
	}
	for _, s := range m.sets {
		for _, f := range s.base {
			take(f)
		}
	}
	for _, n := range m.nodes {
		for _, f := range n.fields {
			take(f)
		}
	}
}

func (m *model) walkElems(alts []*alt, visit func(*elem)) {
	for _, a := range alts {
		for _, e := range a.elems {
			visit(e)
			if e.kind == ekGroup {
				m.walkElems(e.group, visit)
			}
		}
	}
}

func (m *model) checkReferences() {
	for _, r := range m.g.rules {
		m.walkElems(r.alts, func(e *elem) {
			switch e.kind {
			case ekRule:
				if m.ruleByName[e.text] == nil {
					m.fail(e.line, "rule '%s' is not declared", e.text)
				}
			case ekToken:
				if e.text == keywordSet {
					break
				}
				if e.text != "EOF" && m.tokenByName[e.text] == nil {
					m.fail(e.line, "token '%s' is not declared", e.text)
				}
				if t := m.tokenByName[e.text]; t != nil && t.skip {
					m.fail(e.line, "token '%s' is skipped, so no rule can use it", e.text)
				}
			case ekLiteral:
				if m.punctuation[e.text] == nil && !isWord(e.text) {
					m.fail(e.line, "'%s' is neither a keyword nor the text of a token", e.text)
				}
			}
		})
	}
}

// checkPredicates refuses a predicate anywhere but at the start of the only alternative of an optional group, or at the
// start of an alternative of a set rule or of a group of several alternatives, or after the operator of an alternative of an
// operator rule (a guard), the places the parser tests it.
func (m *model) checkPredicates() {
	var walk func(r *rule, alts []*alt, allowed bool)
	walk = func(r *rule, alts []*alt, allowed bool) {
		for _, a := range alts {
			guarded := r != nil && m.kind[r] == rkSet && m.isLeftRecursive(r, a)
			for i, e := range a.elems {
				if e.kind == ekPredicate && !(allowed && i == 0) && !(guarded && i == 2) {
					m.fail(e.line, "a predicate {%s}? belongs at the start of the only alternative of an optional group, or of an alternative of a set rule", e.text)
				}
				if e.kind == ekGroup {
					walk(nil, e.group, (e.suffix == '?' && len(e.group) == 1) || len(e.group) > 1)
				}
			}
		}
	}
	for _, r := range m.g.rules {
		walk(r, r.alts, m.kind[r] == rkSet)
	}
}

func isWord(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}

// passThrough reports whether an alternative only wraps one rule's value: no labels, one rule reference, and
// otherwise single literals or tokens.
func (m *model) passThrough(a *alt) (*rule, bool) {
	var ref *rule
	for _, e := range a.elems {
		if e.label != "" || e.suffix != 0 {
			return nil, false
		}
		switch e.kind {
		case ekRule:
			r := m.ruleByName[e.text]
			if ref != nil || r.returns != nil {
				return nil, false
			}
			ref = r
		case ekGroup:
			return nil, false
		}
	}
	return ref, ref != nil
}

func (m *model) classify() {
	for _, r := range m.g.rules {
		switch {
		case r.returns != nil:
			m.kind[r] = rkHelper
		case m.isSetRule(r):
			m.kind[r] = rkSet
		default:
			m.kind[r] = rkNode
		}
	}
	for _, r := range m.g.rules {
		for _, a := range r.alts {
			if len(a.elems) > 0 && a.elems[0].kind == ekRule && a.elems[0].text == r.name && a.elems[0].suffix == 0 {
				if m.kind[r] != rkSet {
					m.fail(a.line, "only a set rule may be left-recursive, and '%s' is not one", r.name)
				}
				m.leftRec[r] = true
			}
			if a.assocRight && m.kind[r] != rkSet {
				m.fail(a.line, "<assoc=right> belongs on an operator alternative of a set rule")
			}
		}
	}
}

func (m *model) isSetRule(r *rule) bool {
	for _, a := range r.alts {
		if a.label != "" {
			continue
		}
		if _, ok := m.passThrough(a); !ok {
			return false
		}
	}
	return true
}

func (m *model) collectKeywords() {
	seen := map[string]bool{}
	for _, r := range m.g.rules {
		m.walkElems(r.alts, func(e *elem) {
			if e.kind == ekLiteral && m.punctuation[e.text] == nil && !seen[e.text] {
				seen[e.text] = true
				m.keywords = append(m.keywords, e.text)
			}
		})
	}
}

func (m *model) buildSets() {
	for _, r := range m.g.rules {
		if m.kind[r] == rkSet {
			s := &set{name: capitalize(r.name), rule: r}
			m.sets = append(m.sets, s)
			m.setOfRule[r] = s
		}
	}
	for _, s := range m.sets {
		for _, a := range s.rule.alts {
			if a.label != "" {
				continue
			}
			ref, _ := m.passThrough(a)
			if ref == s.rule {
				continue
			}
			if inner := m.setOfRule[ref]; inner != nil {
				if inner.parent != nil {
					m.fail(a.line, "set '%s' is already part of set '%s'", inner.name, inner.parent.name)
				}
				inner.parent = s
			}
		}
	}
	for _, s := range m.sets {
		if b := m.astByName[s.name+"Base"]; b != nil {
			for _, f := range b.fields {
				s.base = append(s.base, &field{name: f.name, typ: f.typ, init: f.init, gap: f.gap, line: f.line})
			}
		}
	}
}

func (m *model) node(name string, line int) *node {
	if n := m.nodeByName[name]; n != nil {
		return n
	}
	n := &node{name: name, line: line, record: !slices.Contains(m.g.structs, name)}
	m.nodeByName[name] = n
	m.nodes = append(m.nodes, n)
	return n
}

func (m *model) buildNodes() {
	for _, r := range m.g.rules {
		switch m.kind[r] {
		case rkNode:
			if r.builds != "" {
				continue
			}
			n := m.node(capitalize(r.name), r.line)
			n.rule = r
			n.alts = r.alts
			m.nodeOfRule[r] = n
		case rkSet:
			s := m.setOfRule[r]
			for _, a := range r.alts {
				if a.label == "" {
					continue
				}
				n := m.node(a.label, a.line)
				if n.rule != nil || (n.set != nil && n.set != s) {
					m.fail(a.line, "node '%s' is defined twice", a.label)
				}
				n.set = s
				n.alts = append(n.alts, a)
			}
		}
	}
	m.attachBuilders()
	for _, s := range m.sets {
		for _, a := range s.rule.alts {
			if a.label != "" {
				continue
			}
			ref, _ := m.passThrough(a)
			if n := m.nodeOfRule[ref]; n != nil {
				if n.set != nil && n.set != s {
					m.fail(a.line, "node '%s' is already in set '%s'", n.name, n.set.name)
				}
				n.set = s
			}
		}
	}
	for _, s := range m.sets {
		s.members = m.membersOf(s)
	}
	for _, n := range m.nodes {
		m.buildFields(n)
	}
}

func (m *model) membersOf(s *set) []*node {
	var members []*node
	seen := map[*node]bool{}
	add := func(n *node) {
		if !seen[n] {
			seen[n] = true
			members = append(members, n)
		}
	}
	for _, a := range s.rule.alts {
		if a.label != "" {
			add(m.nodeByName[a.label])
			continue
		}
		ref, _ := m.passThrough(a)
		if n := m.nodeOfRule[ref]; n != nil {
			add(n)
		} else if inner := m.setOfRule[ref]; inner != nil && inner != s {
			for _, n := range m.membersOf(inner) {
				add(n)
			}
		}
	}
	return members
}

// baseField finds a field the node inherits from its set's base, or a base the set inherits.
func (m *model) baseField(n *node, name string) (*field, bool) {
	for s := n.set; s != nil; s = s.parent {
		for _, f := range s.base {
			if f.name == name {
				return f, true
			}
		}
	}
	return nil, false
}

// valueType is the Go type, in package ast, of what parsing a rule produces.
func (m *model) valueType(r *rule) string {
	switch m.kind[r] {
	case rkNode:
		if n := m.nodeOfRule[r]; n.record {
			return n.name
		}
		return "*" + m.nodeOfRule[r].name
	case rkSet:
		return m.setOfRule[r].name
	case rkHelper:
		return r.returns.typ
	}
	panic(fmt.Sprintf("grammargen: unhandled rule kind %d", m.kind[r]))
}

// labelType infers a label's field type from the element it labels.
func (m *model) labelType(e *elem) string {
	var t string
	switch e.kind {
	case ekLiteral:
		t = "bool"
	case ekToken:
		t = "string"
	case ekRule:
		t = m.valueType(m.ruleByName[e.text])
	case ekGroup:
		t = m.groupType(e)
	default:
		panic(fmt.Sprintf("grammargen: unhandled element kind %d", e.kind))
	}
	if e.appendTo {
		return "[]" + t
	}
	return t
}

func (m *model) groupType(e *elem) string {
	terminals, rules := 0, map[string]bool{}
	var ruleTypes []string
	for _, a := range e.group {
		if len(a.elems) != 1 || a.elems[0].label != "" || a.elems[0].suffix != 0 || a.elems[0].kind == ekGroup {
			m.fail(e.line, "a labelled group holds single literals, tokens or rules")
		}
		inner := a.elems[0]
		if inner.kind == ekRule {
			t := m.valueType(m.ruleByName[inner.text])
			if !rules[t] {
				rules[t] = true
				ruleTypes = append(ruleTypes, t)
			}
		} else {
			terminals++
		}
	}
	switch {
	case len(ruleTypes) == 0:
		return "string"
	case terminals == 0 && len(ruleTypes) == 1:
		return ruleTypes[0]
	}
	m.fail(e.line, "label '%s' mixes types; give its type in @ast", e.label)
	return ""
}

func (m *model) buildFields(n *node) {
	declared := map[string]*astField{}
	block := m.astByName[n.name]
	if block != nil {
		for _, f := range block.fields {
			declared[f.name] = f
		}
	}
	byName := map[string]*field{}
	m.walkElems(n.alts, func(e *elem) {
		if e.label == "" {
			return
		}
		name := capitalize(e.label)
		if _, inBase := m.baseField(n, name); inBase {
			return
		}
		t := m.labelType(e)
		if d := declared[name]; d != nil {
			t = d.typ
		}
		if f := byName[name]; f != nil {
			if f.typ != t {
				m.fail(e.line, "label '%s' of %s is %s here and %s elsewhere; give its type in @ast::%s", e.label, n.name, t, f.typ, n.name)
			}
			f.elems = append(f.elems, e)
			return
		}
		f := &field{name: name, typ: t, elems: []*elem{e}}
		if d := declared[name]; d != nil {
			f.init = d.init
		}
		byName[name] = f
		n.fields = append(n.fields, f)
	})
	if block == nil {
		return
	}
	first := true
	for _, d := range block.fields {
		if byName[d.name] != nil {
			continue
		}
		if _, inBase := m.baseField(n, d.name); inBase {
			m.fail(d.line, "%s already has '%s' from its set's base", n.name, d.name)
		}
		f := &field{name: d.name, typ: d.typ, init: d.init, gap: d.gap || (first && len(n.fields) > 0), line: d.line}
		first = false
		byName[d.name] = f
		n.fields = append(n.fields, f)
	}
}

// fieldOf finds the field a label assigns, on the node or inherited from its set's base.
func (m *model) fieldOf(n *node, label string, line int) *field {
	name := capitalize(label)
	for _, f := range n.fields {
		if f.name == name {
			return f
		}
	}
	if f, ok := m.baseField(n, name); ok {
		return f
	}
	m.fail(line, "%s has no field '%s'", n.name, name)
	return nil
}

func (m *model) checkAstBlocks() {
	for _, b := range m.g.astBlocks {
		if m.nodeByName[b.name] != nil {
			continue
		}
		found := false
		for _, s := range m.sets {
			found = found || s.name+"Base" == b.name
		}
		if !found {
			m.fail(b.line, "@ast::%s names no node and no set base", b.name)
		}
	}
}

// terminal is the Go name of a token kind, as the generated parser and lexer use it.
func (m *model) terminal(e *elem) string {
	switch e.kind {
	case ekLiteral:
		if t := m.punctuation[e.text]; t != nil {
			return "T" + t.name
		}
		return keywordConst(e.text)
	case ekToken:
		if e.text == "EOF" {
			return eofConst
		}
		return "T" + e.text
	}
	panic(fmt.Sprintf("grammargen: element kind %d is not a terminal", e.kind))
}

const eofConst = "TEof"

const keywordPrefix = "TKw_"

func keywordConst(word string) string { return keywordPrefix + word }

// describeTerminal names a token kind in a syntax error.
func (m *model) describeTerminal(id string) string {
	if id == eofConst {
		return "the end of the file"
	}
	if after, ok := strings.CutPrefix(id, keywordPrefix); ok {
		return "'" + after + "'"
	}
	t := m.tokenByName[strings.TrimPrefix(id, "T")]
	switch {
	case t.desc != "":
		return t.desc
	case t.literal != "":
		return "'" + t.literal + "'"
	}
	return t.name
}

// nameKeywords are the keywords that are names wherever a name is required, so a syntax error that asks for a name does
// not list them.
var nameKeywords = []string{"from", "to"}

// describeTerminals names the token kinds that a syntax error expects, leaving out the keyword that a name already
// stands for.
func (m *model) describeTerminals(ids []string) []string {
	namesAllowed := slices.Contains(ids, "TName")
	var parts []string
	for _, id := range ids {
		if namesAllowed && slices.ContainsFunc(nameKeywords, func(word string) bool { return id == keywordConst(word) }) {
			continue
		}
		parts = append(parts, m.describeTerminal(id))
	}
	return parts
}

func joinOr(parts []string) string {
	switch len(parts) {
	case 0:
		return "nothing"
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}

// attachBuilders makes each rule that builds another rule's node parse into it, so the node's fields come from the
// alternatives of every rule that writes it.
func (m *model) attachBuilders() {
	for _, r := range m.g.rules {
		if r.builds == "" {
			continue
		}
		n := m.nodeByName[r.builds]
		if n == nil || n.rule == nil {
			m.fail(r.line, "rule '%s' builds '%s', which no rule defines", r.name, r.builds)
			continue
		}
		n.alts = append(n.alts, r.alts...)
		m.nodeOfRule[r] = n
	}
}

// keywordSet is the placeholder `Keyword`, which stands in a group for every keyword of the grammar, so that a position
// that takes any word (an argument label) is not a list of keywords that goes stale when the grammar gains one.
const keywordSet = "Keyword"

// expandKeywordSets replaces the placeholder in each group by one alternative per keyword the group does not name.
func (m *model) expandKeywordSets() {
	for _, r := range m.g.rules {
		m.walkElems(r.alts, func(e *elem) {
			if e.kind != ekGroup {
				return
			}
			named := map[string]bool{}
			for _, a := range e.group {
				if len(a.elems) == 1 && a.elems[0].kind == ekLiteral {
					named[a.elems[0].text] = true
				}
			}
			var alts []*alt
			for _, a := range e.group {
				if len(a.elems) != 1 || a.elems[0].kind != ekToken || a.elems[0].text != keywordSet {
					alts = append(alts, a)
					continue
				}
				e.anyKeyword = true
				for _, k := range m.keywords {
					if !named[k] {
						alts = append(alts, &alt{elems: []*elem{{kind: ekLiteral, text: k, line: a.line}}, line: a.line})
					}
				}
			}
			e.group = alts
		})
	}
	for _, r := range m.g.rules {
		m.walkElems(r.alts, func(e *elem) {
			if e.kind == ekToken && e.text == keywordSet {
				m.fail(e.line, "%s stands for every keyword, so it belongs in a group of alternatives such as (Name | %s)", keywordSet, keywordSet)
			}
		})
	}
}
