package interp_test

import (
	_ "embed"
	"fmt"
	"math/rand"
	"strings"
)

// The program generator writes random minimaxon programs over the constructs the memory model's rules cover: records,
// unions, strings, let and var, assignment, moves, copy(), the four conventions, calls whose arguments overlap or are
// disjoint, match bindings, loops, generics with bounds, and arrays, maps, lists and pools with handles. A program is
// well formed, and the types are consistent, but nothing in it tries to obey the ownership rules: the checker decides
// which programs are accepted, and every one it accepts must run without a violation. The generator only leans toward
// programs that pass, so that most of what it writes reaches the interpreter, and leans toward the edges of the rules
// where a hole would be: arguments that overlap, values moved out of branches and loops, borrows held across changes.

type kind int

const (
	kCount kind = iota
	kBool
	kString
	kRecord
	kUnion
	kArray
	kPool
	kList
	kMap
	kHandle
	kOption
	kFrozen
	kCown
)

type ty struct {
	kind kind
	name string
	args []*ty
}

func (t *ty) src() string {
	switch t.kind {
	case kCount:
		return "Count"
	case kBool:
		return "bool"
	case kString:
		return "String"
	case kRecord, kUnion:
		return t.name
	}
	names := map[kind]string{kArray: "array", kPool: "pool", kList: "list", kMap: "map", kHandle: "Handle", kOption: "Option", kFrozen: "frozen", kCown: "cown"}
	parts := make([]string, len(t.args))
	for i, a := range t.args {
		parts[i] = a.src()
	}
	return names[t.kind] + "[" + strings.Join(parts, ", ") + "]"
}

func (t *ty) managed() bool { return t.kind != kCount && t.kind != kBool && t.kind != kHandle }

func (t *ty) scalar() bool { return !t.managed() }

type field struct {
	name  string
	t     *ty
	isVar bool
}

type caseDecl struct {
	name    string
	payload []field
}

type param struct {
	name string
	t    *ty
	conv string
}

type fn struct {
	name   string
	params []param
	ret    *ty
	// recv is "", "mutable" or "sink" for a method; self marks a method.
	recv   string
	method bool
	owner  *ty
}

type record struct {
	t       *ty
	fields  []field
	methods []*fn
	make    *fn
}

type union struct {
	t     *ty
	cases []caseDecl
}

type variable struct {
	name    string
	t       *ty
	owned   bool
	mutable bool
	moved   bool
	// loop is the loop depth where the variable was declared.
	loop int
	// frozen is how many enclosing match arms borrow from the variable.
	frozen int
	// setParam marks a set parameter, which a function owns once it has initialised it but which a behaviour cannot capture:
	// the caller's variable receives it.
	setParam bool
}

type generator struct {
	r       *rand.Rand
	records []*record
	unions  []*union
	fns     []*fn
	types   map[string]*ty
	out     []string
	indent  int
	labels  int
	names   int

	scopes   [][]*variable
	ret      *ty
	loop     int
	loopTags []string
	budget   int
	guards   []string
	// pendingInit are variables that the statement being written gives a value, through a set parameter.
	pendingInit []*variable
	// carelessPercent is how often this program breaks a rule of ownership on purpose.
	carelessPercent int
	// arraysOnly keeps a map, a list and a pool out of the program, so that it also compiles to an executable.
	arraysOnly bool
	// sharing lets the program freeze values, make cowns and schedule behaviours, none of which has an executable yet.
	sharing bool
	// behaviours is how many behaviours enclose the statement being written.
	behaviours int
	// sinsLeft is how many rules of sharing a targeted program still breaks on purpose. A careless program breaks them
	// throughout and is nearly always refused for the first of many, so a program with a single sin is the one in which
	// a rule that the checker lacks shows.
	sinsLeft int
	recent   place
	recentOK bool
}

var (
	tCount  = &ty{kind: kCount}
	tBool   = &ty{kind: kBool}
	tString = &ty{kind: kString}
)

func newGenerator(seed int64) *generator {
	return &generator{r: rand.New(rand.NewSource(seed)), types: map[string]*ty{}}
}

func (g *generator) intern(t *ty) *ty {
	key := t.src()
	if old, ok := g.types[key]; ok {
		return old
	}
	g.types[key] = t
	return t
}

func (g *generator) array(e *ty) *ty  { return g.intern(&ty{kind: kArray, args: []*ty{e}}) }
func (g *generator) pool(e *ty) *ty   { return g.intern(&ty{kind: kPool, args: []*ty{e}}) }
func (g *generator) list(e *ty) *ty   { return g.intern(&ty{kind: kList, args: []*ty{e}}) }
func (g *generator) handle(e *ty) *ty { return g.intern(&ty{kind: kHandle, args: []*ty{e}}) }
func (g *generator) option(e *ty) *ty { return g.intern(&ty{kind: kOption, args: []*ty{e}}) }
func (g *generator) mapOf(k, v *ty) *ty {
	return g.intern(&ty{kind: kMap, args: []*ty{k, v}})
}

func (g *generator) chance(percent int) bool { return g.r.Intn(100) < percent }

func (g *generator) pick(n int) int { return g.r.Intn(n) }

func (g *generator) fresh(prefix string) string {
	g.names++
	return fmt.Sprintf("%s%d", prefix, g.names)
}

func (g *generator) label() string {
	g.labels++
	return fmt.Sprintf("'l%d'", g.labels)
}

func (g *generator) line(format string, args ...any) {
	g.out = append(g.out, strings.Repeat("\t", g.indent)+fmt.Sprintf(format, args...))
}

func (g *generator) blank() { g.out = append(g.out, "") }

// Types

// typeUniverse is every type the program's declarations name, so a value of any of them can be made.
func (g *generator) managedTypes() []*ty {
	var out []*ty
	out = append(out, tString)
	for _, r := range g.records {
		out = append(out, r.t)
	}
	for _, u := range g.unions {
		out = append(out, u.t)
	}
	return out
}

func (g *generator) anyType() *ty {
	if g.sharing && g.chance(14) {
		return g.sharedType()
	}
	scalars := []*ty{tCount, tCount, tBool}
	managed := g.managedTypes()
	switch n := g.pick(10); {
	case n < 2:
		return scalars[g.pick(len(scalars))]
	case n < 6:
		return managed[g.pick(len(managed))]
	case n < 7:
		return g.array(g.elementType())
	case n < 8:
		return g.option(g.elementType())
	case n < 9 && !g.arraysOnly:
		return g.pool(g.elementType())
	case g.arraysOnly:
		return g.array(g.elementType())
	}
	return g.list(g.elementType())
}

// elementType is a type an array, a pool or an option holds.
func (g *generator) elementType() *ty {
	if g.sharing && g.chance(7) {
		return g.sharedType()
	}
	switch n := g.pick(10); {
	case n < 3:
		return tCount
	case n < 5:
		return tString
	}
	managed := g.managedTypes()
	return managed[g.pick(len(managed))]
}

func (g *generator) keyType() *ty {
	if g.chance(50) {
		return tCount
	}
	return tString
}

func (g *generator) recordOf(t *ty) *record {
	for _, r := range g.records {
		if r.t == t {
			return r
		}
	}
	return nil
}

func (g *generator) unionOf(t *ty) *union {
	for _, u := range g.unions {
		if u.t == t {
			return u
		}
	}
	return nil
}

// Declarations

func (g *generator) declareTypes() {
	records := 2 + g.pick(3)
	for i := range records {
		t := g.intern(&ty{kind: kRecord, name: fmt.Sprintf("R%d", i)})
		r := &record{t: t}
		fields := 1 + g.pick(4)
		for j := range fields {
			r.fields = append(r.fields, field{name: fmt.Sprintf("f%d", j), t: g.fieldType(), isVar: g.chance(85)})
		}
		g.records = append(g.records, r)
		if i == 0 || g.chance(40) {
			g.unions = append(g.unions, g.newUnion(len(g.unions)))
		}
	}
	for _, name := range []string{"Box[Count]", "Box[String]"} {
		t := g.intern(&ty{kind: kRecord, name: name})
		item := g.boxed(name)
		g.records = append(g.records, &record{t: t, fields: []field{{name: "item", t: item, isVar: true}},
			methods: []*fn{{name: "peek", method: true, owner: t, ret: item}}})
	}
	for _, r := range g.records {
		r.make = &fn{name: "make", owner: r.t, ret: r.t}
		for i, f := range r.fields {
			conv := ""
			if f.t.managed() {
				conv = kwSink
			}
			r.make.params = append(r.make.params, param{name: fmt.Sprintf("p%d", i), t: f.t, conv: conv})
		}
	}
	meter := g.intern(&ty{kind: kRecord, name: "Meter"})
	g.records = append(g.records, &record{t: meter, fields: []field{{name: "n", t: tCount, isVar: true}},
		make: &fn{name: "make", owner: meter, ret: meter, params: []param{{name: "n", t: tCount}}},
		methods: []*fn{
			{name: "score", method: true, owner: meter, ret: tCount},
			{name: "grow", method: true, recv: kwMutable, owner: meter, params: []param{{name: "by", t: tCount}}},
		}})
}

func (g *generator) boxed(name string) *ty {
	if strings.Contains(name, "String") {
		return tString
	}
	return tCount
}

// fieldType is the type of a field: only types declared before it, so records never contain themselves.
func (g *generator) fieldType() *ty {
	if g.sharing && g.chance(9) {
		return g.sharedTypeBefore()
	}
	switch n := g.pick(14); {
	case n < 4:
		return tCount
	case n < 5:
		return tBool
	case n < 8:
		return tString
	case n < 10:
		return g.array(g.elementTypeBefore())
	case n < 11:
		return g.option(g.elementTypeBefore())
	case n < 12 && !g.arraysOnly:
		return g.handle(g.elementTypeBefore())
	}
	return g.elementTypeBefore()
}

func (g *generator) elementTypeBefore() *ty {
	switch n := g.pick(10); {
	case n < 4:
		return tCount
	case n < 6:
		return tString
	}
	var declared []*ty
	for _, r := range g.records {
		if !strings.HasPrefix(r.t.name, "Box") {
			declared = append(declared, r.t)
		}
	}
	if len(declared) == 0 {
		return tString
	}
	return declared[g.pick(len(declared))]
}

func (g *generator) newUnion(i int) *union {
	t := g.intern(&ty{kind: kUnion, name: fmt.Sprintf("U%d", i)})
	u := &union{t: t}
	cases := 2 + g.pick(2)
	for c := range cases {
		d := caseDecl{name: fmt.Sprintf("c%d", c)}
		for p := g.pick(3); p > 0; p-- {
			d.payload = append(d.payload, field{name: fmt.Sprintf("x%d", len(d.payload)), t: g.fieldType()})
		}
		u.cases = append(u.cases, d)
	}
	return u
}

func (g *generator) writeTypes() {
	for _, r := range g.records {
		if r.t.name == "Meter" || strings.HasPrefix(r.t.name, "Box") {
			continue
		}
		g.line(kwType+" %s", r.t.name)
		g.indent++
		for _, f := range r.fields {
			g.line(kwExport+" %s %s "+kwAs+" %s", map[bool]string{true: kwVar, false: kwLet}[f.isVar], f.name, f.t.src())
		}
		g.methods(r)
		g.indent--
		g.line(kwEnd+" '%s'", r.t.name)
		g.blank()
	}
	for _, u := range g.unions {
		g.line(kwUnion+" %s", u.t.name)
		g.indent++
		for _, c := range u.cases {
			g.line("%s", caseSignature(c))
		}
		g.indent--
		g.line(kwEnd+" '%s'", u.t.name)
		g.blank()
	}
}

func caseSignature(c caseDecl) string {
	if len(c.payload) == 0 {
		return c.name
	}
	parts := make([]string, len(c.payload))
	for i, p := range c.payload {
		parts[i] = p.name + " " + p.t.src()
	}
	return c.name + "(" + strings.Join(parts, ", ") + ")"
}

// methods writes the factory and three methods of a record: one that reads, one that changes and one that consumes it.
func (g *generator) methods(r *record) {
	params := make([]string, len(r.fields))
	inits := make([]string, len(r.fields))
	for i, f := range r.fields {
		conv := ""
		if f.t.managed() {
			conv = " " + kwSink
		}
		params[i] = fmt.Sprintf("p%d%s %s", i, conv, f.t.src())
		inits[i] = fmt.Sprintf("%s: p%d", f.name, i)
	}
	// A function in a declaration has a blank line before it, unless it is the body's first line.
	if len(r.fields) > 0 {
		g.blank()
	}
	g.line(kwStatic+" "+kwFunction+" make(%s) "+kwReturns+" %s", strings.Join(params, ", "), r.t.name)
	g.indent++
	g.line(kwReturn+" %s{%s}", r.t.name, strings.Join(inits, ", "))
	g.indent--
	g.line(kwEnd + " 'make'")

	specs := []struct {
		name   string
		recv   string
		params []param
		ret    *ty
	}{
		{"read", "", nil, tCount},
		{"bump", kwMutable, []param{{name: "by", t: tCount}}, nil},
		{"finish", kwSink, nil, tCount},
		{"absorb", kwMutable, []param{{name: "other", t: r.t}}, nil},
	}
	for _, spec := range specs {
		m := &fn{name: spec.name, method: true, recv: spec.recv, owner: r.t, params: spec.params, ret: spec.ret}
		g.blank()
		g.method(r, m)
		r.methods = append(r.methods, m)
	}
}

// method writes a method whose body is generated like any other function's, with self a variable that the statements
// use. A method may call only the methods written before it, so no call can recurse.
func (g *generator) method(r *record, m *fn) {
	var parts []string
	if m.recv != "" {
		parts = append(parts, m.recv+" "+kwSelf)
	}
	for _, p := range m.params {
		parts = append(parts, p.name+" "+p.t.src())
	}
	header := fmt.Sprintf(kwFunction+" %s(%s)", m.name, strings.Join(parts, ", "))
	if m.ret != nil {
		header += " " + kwReturns + " " + m.ret.src()
	}
	g.line("%s", header)
	savedScopes, savedRet, savedLoop, savedBudget := g.scopes, g.ret, g.loop, g.budget
	g.scopes = nil
	g.push()
	g.declare(kwSelf, r.t, m.recv == kwSink, m.recv == kwMutable)
	for _, p := range m.params {
		g.declare(p.name, p.t, false, false)
	}
	g.ret, g.loop, g.budget = m.ret, 0, 4+g.pick(4)
	g.indent++
	g.push()
	start := len(g.out)
	for g.budget > 0 {
		g.stmt(2)
	}
	if m.ret != nil {
		g.line(kwReturn+" %s", g.owned(m.ret, 2))
	}
	if len(g.out) == start {
		g.line("print(\"m\\n\")")
	}
	g.pop()
	g.indent--
	g.line(kwEnd+" '%s'", m.name)
	g.scopes, g.ret, g.loop, g.budget = savedScopes, savedRet, savedLoop, savedBudget
}

// library is the generic code every program has: functions generic over any type, a bounded one, and a generic record.
//
//go:embed testdata/library.maxon
var library string
