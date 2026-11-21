package main

import (
	"slices"
	"sort"
	"strings"
)

// A lookahead string is up to two terminals joined by a space; "" is the empty string. At the end of a rule only
// one token of what follows is known, so a one-terminal string there means "this token, then anything".
type laSet map[string]bool

const lookahead = 2

func concat(a, b laSet) laSet {
	out := laSet{}
	for x := range a {
		if count(x) >= lookahead {
			out[x] = true
			continue
		}
		for y := range b {
			out[truncate(x, y)] = true
		}
	}
	return out
}

func count(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, " ") + 1
}

func truncate(x, y string) string {
	switch {
	case x == "":
		return y
	case y == "":
		return x
	}
	parts := append(strings.Fields(x), strings.Fields(y)...)
	return strings.Join(parts[:min(len(parts), lookahead)], " ")
}

func union(a, b laSet) laSet {
	out := laSet{}
	for x := range a {
		out[x] = true
	}
	for x := range b {
		out[x] = true
	}
	return out
}

func firstTerminals(s laSet) []string {
	seen := map[string]bool{}
	var out []string
	for x := range s {
		if x == "" {
			continue
		}
		t := strings.Fields(x)[0]
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

type analysis struct {
	m      *model
	first  map[*rule]laSet
	follow map[*rule]map[string]bool
	body   map[*rule][]*alt
}

// analysisBody is what a rule matches, with a left-recursive rule rewritten as its operands followed by any
// number of operator tails, so the lookahead computation needs no special case.
func (a *analysis) analysisBody(r *rule) []*alt {
	if !a.m.leftRec[r] {
		return r.alts
	}
	if b := a.body[r]; b != nil {
		return b
	}
	var operands, tails []*alt
	for _, x := range r.alts {
		if a.m.isLeftRecursive(r, x) {
			tails = append(tails, &alt{elems: x.elems[1:]})
		} else {
			operands = append(operands, x)
		}
	}
	b := []*alt{{elems: []*elem{
		{kind: ekGroup, group: operands},
		{kind: ekGroup, group: tails, suffix: '*'},
	}}}
	a.body[r] = b
	return b
}

func (m *model) isLeftRecursive(r *rule, a *alt) bool {
	return len(a.elems) > 0 && a.elems[0].kind == ekRule && a.elems[0].text == r.name && a.elems[0].suffix == 0
}

func analyze(m *model) *analysis {
	a := &analysis{m: m, first: map[*rule]laSet{}, follow: map[*rule]map[string]bool{}, body: map[*rule][]*alt{}}
	for _, r := range m.g.rules {
		a.first[r] = laSet{}
		a.follow[r] = map[string]bool{}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range m.g.rules {
			f := laSet{}
			for _, x := range a.analysisBody(r) {
				f = union(f, a.seqFirst(x.elems))
			}
			if len(f) != len(a.first[r]) {
				a.first[r] = f
				changed = true
			}
		}
	}
	referenced := map[*rule]bool{}
	for _, r := range m.g.rules {
		m.walkElems(r.alts, func(e *elem) {
			if e.kind == ekRule && e.text != r.name {
				referenced[m.ruleByName[e.text]] = true
			}
		})
	}
	// A rule no other rule references is where hand-written code starts parsing; the grammar cannot say what
	// follows it there, so the end of input stands for it.
	for _, r := range m.g.rules {
		if !referenced[r] {
			a.follow[r][eofConst] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range m.g.rules {
			for _, x := range a.analysisBody(r) {
				changed = a.walkFollow(x.elems, a.follow[r]) || changed
			}
		}
	}
	return a
}

func (a *analysis) seqFirst(elems []*elem) laSet {
	f := laSet{"": true}
	for _, e := range elems {
		f = concat(f, a.elemFirst(e))
	}
	return f
}

func (a *analysis) elemFirst(e *elem) laSet {
	var f laSet
	switch e.kind {
	case ekLiteral, ekToken:
		f = laSet{a.m.terminal(e): true}
	case ekRule:
		f = a.first[a.m.ruleByName[e.text]]
	case ekPredicate:
		f = laSet{"": true}
	case ekGroup:
		f = laSet{}
		for _, x := range e.group {
			f = union(f, a.seqFirst(x.elems))
		}
	}
	switch e.suffix {
	case '?':
		return union(f, laSet{"": true})
	case '*':
		return union(laSet{"": true}, union(f, concat(f, f)))
	case '+':
		return union(f, concat(f, f))
	}
	return f
}

func (a *analysis) firstOne(e *elem) (map[string]bool, bool) {
	f := a.elemFirst(e)
	out := map[string]bool{}
	for _, t := range firstTerminals(f) {
		out[t] = true
	}
	return out, f[""]
}

// walkFollow adds what can follow each rule reference in elems, given what follows elems, and reports a change.
func (a *analysis) walkFollow(elems []*elem, after map[string]bool) bool {
	changed := false
	cur := after
	for _, e := range slices.Backward(elems) {

		inner := cur
		if e.suffix == '*' || e.suffix == '+' {
			own, _ := a.firstOne(e)
			inner = mergeTerms(own, cur)
		}
		switch e.kind {
		case ekRule:
			f := a.follow[a.m.ruleByName[e.text]]
			for t := range inner {
				if !f[t] {
					f[t] = true
					changed = true
				}
			}
		case ekGroup:
			for _, x := range e.group {
				changed = a.walkFollow(x.elems, inner) || changed
			}
		}
		own, nullable := a.firstOne(e)
		if nullable {
			cur = mergeTerms(own, cur)
		} else {
			cur = own
		}
	}
	return changed
}

func mergeTerms(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for t := range a {
		out[t] = true
	}
	for t := range b {
		out[t] = true
	}
	return out
}

// cont is what follows a point inside a rule: the rest of each enclosing sequence, innermost first, then the
// rule's follow set.
type cont struct {
	rule  *rule
	parts [][]*elem
}

func (c cont) then(rest []*elem) cont {
	parts := make([][]*elem, 0, len(c.parts)+1)
	parts = append(parts, rest)
	return cont{rule: c.rule, parts: append(parts, c.parts...)}
}

func (a *analysis) contFirst(c cont) laSet {
	f := laSet{"": true}
	for _, part := range c.parts {
		f = concat(f, a.seqFirst(part))
	}
	follow := laSet{}
	for t := range a.follow[c.rule] {
		follow[t] = true
	}
	return concat(f, follow)
}

func (a *analysis) lookahead(elems []*elem, c cont) laSet {
	return concat(a.seqFirst(elems), a.contFirst(c))
}
