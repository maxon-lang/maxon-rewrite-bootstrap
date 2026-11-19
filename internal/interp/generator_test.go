package interp_test

import (
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
	// recv is "", "inout" or "sink" for a method; self marks a method.
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
				conv = "sink"
			}
			r.make.params = append(r.make.params, param{name: fmt.Sprintf("p%d", i), t: f.t, conv: conv})
		}
	}
	meter := g.intern(&ty{kind: kRecord, name: "Meter"})
	g.records = append(g.records, &record{t: meter, fields: []field{{name: "n", t: tCount, isVar: true}},
		make: &fn{name: "make", owner: meter, ret: meter, params: []param{{name: "n", t: tCount}}},
		methods: []*fn{
			{name: "score", method: true, owner: meter, ret: tCount},
			{name: "grow", method: true, recv: "inout", owner: meter, params: []param{{name: "by", t: tCount}}},
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
		g.line("type %s", r.t.name)
		g.indent++
		for _, f := range r.fields {
			g.line("export %s %s as %s", map[bool]string{true: "var", false: "let"}[f.isVar], f.name, f.t.src())
		}
		g.methods(r)
		g.indent--
		g.line("end '%s'", r.t.name)
		g.blank()
	}
	for _, u := range g.unions {
		g.line("union %s", u.t.name)
		g.indent++
		for _, c := range u.cases {
			g.line("%s", caseSignature(c))
		}
		g.indent--
		g.line("end '%s'", u.t.name)
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
			conv = " sink"
		}
		params[i] = fmt.Sprintf("p%d%s %s", i, conv, f.t.src())
		inits[i] = fmt.Sprintf("%s: p%d", f.name, i)
	}
	g.line("static function make(%s) returns %s", strings.Join(params, ", "), r.t.name)
	g.indent++
	g.line("return %s{%s}", r.t.name, strings.Join(inits, ", "))
	g.indent--
	g.line("end 'make'")

	specs := []struct {
		name   string
		recv   string
		params []param
		ret    *ty
	}{
		{"read", "", nil, tCount},
		{"bump", "inout", []param{{name: "by", t: tCount}}, nil},
		{"finish", "sink", nil, tCount},
		{"absorb", "inout", []param{{name: "other", t: r.t}}, nil},
	}
	for _, spec := range specs {
		m := &fn{name: spec.name, method: true, recv: spec.recv, owner: r.t, params: spec.params, ret: spec.ret}
		g.method(r, m)
		r.methods = append(r.methods, m)
	}
}

// method writes a method whose body is generated like any other function's, with self a variable that the statements
// use. A method may call only the methods written before it, so no call can recurse.
func (g *generator) method(r *record, m *fn) {
	var parts []string
	if m.recv != "" {
		parts = append(parts, m.recv+" self")
	}
	for _, p := range m.params {
		parts = append(parts, p.name+" "+p.t.src())
	}
	header := fmt.Sprintf("function %s(%s)", m.name, strings.Join(parts, ", "))
	if m.ret != nil {
		header += " returns " + m.ret.src()
	}
	g.line("%s", header)
	savedScopes, savedRet, savedLoop, savedBudget := g.scopes, g.ret, g.loop, g.budget
	g.scopes = nil
	g.push()
	g.declare("self", r.t, m.recv == "sink", m.recv == "inout")
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
		g.line("return %s", g.owned(m.ret, 2))
	}
	if len(g.out) == start {
		g.line("print(\"m\\n\")")
	}
	g.pop()
	g.indent--
	g.line("end '%s'", m.name)
	g.scopes, g.ret, g.loop, g.budget = savedScopes, savedRet, savedLoop, savedBudget
}

// library is the generic code every program has: functions generic over any type, a bounded one, and a generic record.
const library = `interface Scorable
	function score() returns Count
end 'Scorable'

interface Growable
	function grow(inout self, by Count)
end 'Growable'

type Meter is Scorable and Growable
	var n as Count
	static function make(n Count) returns Meter
		return Meter{n: n}
	end 'make'
	function score() returns Count
		return n
	end 'score'
	function grow(inout self, by Count)
		n = n + by
	end 'grow'
end 'Meter'

type Box[T]
	var item as T
	static function make(item sink T) returns Self
		return Self{item: item}
	end 'make'
	function peek() returns T
		return item.copy()
	end 'peek'
	function replace(inout self, next sink T)
		item = next
	end 'replace'
	function take(sink self) returns T
		return item.copy()
	end 'take'
end 'Box'

function dup[T](x T) returns T
	return x.copy()
end 'dup'

function put[T](items inout array[T], x sink T)
	items.append(x)
end 'put'

function firstOf[T](items array[T]) returns Option[T]
	if items.isEmpty() 'e'
		return Option[T].none
	end 'e'
	return Option.some(items[0].copy())
end 'firstOf'

function overwrite[T](target inout T, with T)
	target = with.copy()
end 'overwrite'

function total[T is Scorable](x T) returns Count
	return x.score()
end 'total'

function totalFrozen[T is Scorable](x frozen[T]) returns Count
	return x.score()
end 'totalFrozen'

function growTwice[T is Growable](x inout T)
	x.grow(1)
	x.grow(2)
end 'growTwice'

function keep[T](x sink T) returns T
	return x
end 'keep'

function pass2[T](a sink T, b sink T, first bool) returns T
	if first 'f'
		return a
	end 'f'
	return b
end 'pass2'

function both[A, B](a A, b inout B)
	b = b.copy()
	let c = a.copy()
end 'both'

function consume[T](x sink T)
	let kept = x
end 'consume'

function both2[A, B](a A, b sink B)
	consume[B](b)
	let c = a.copy()
end 'both2'

function fill[T](out set T, from T)
	out = from.copy()
end 'fill'

function churn[T](items inout array[T]) returns Count
	let last = items.pop()
	return items.count()
end 'churn'

function churnList[T](items inout list[T]) returns Count
	let first = items.popFront()
	return items.count()
end 'churnList'

function churnMap[V](m inout map[String, V]) returns Count
	let gone = m.remove("k0")
	return m.count()
end 'churnMap'

function churnPool[T](p inout pool[T], x sink T) returns Count
	let h = p.insert(x)
	return p.count()
end 'churnPool'

function shrink(items inout array[Count]) returns Count
	let last = items.pop()
	return items.count()
end 'shrink'

function bump(x inout Count)
	x = x + 1
end 'bump'

function mint[T](x sink T) returns Handle[T]
	var p = pool[T].new()
	return p.insert(x)
end 'mint'


`
