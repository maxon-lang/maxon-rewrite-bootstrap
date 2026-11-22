package main

import (
	"fmt"
	"sort"
	"strings"
)

// spec is a pair of functions the generator writes: the store and the load of one type, in the module that hosts them.
type spec struct {
	host string
	t    *rtype
	name string
}

// gen collects the functions of every module and writes them.
type gen struct {
	w        *world
	specs    map[string]map[string]*spec
	order    map[string][]string
	exported map[string]map[string]bool
	queue    []*spec
	errs     []string
}

func newGen(w *world) *gen {
	g := &gen{w: w, specs: map[string]map[string]*spec{}, order: map[string][]string{}, exported: map[string]map[string]bool{}}
	for _, m := range layers {
		g.specs[m] = map[string]*spec{}
		g.exported[m] = map[string]bool{}
	}
	return g
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// qual is how module mod names a declaration of module.
func qual(module, mod string) string {
	if module == "" || module == mod {
		return ""
	}
	return module + "."
}

// paren wraps a type that is an instance, so that it can stand as the argument of another.
func paren(s string) string {
	if strings.Contains(s, " with ") {
		return "(" + s + ")"
	}
	return s
}

// render is the type t as module mod writes it.
func (g *gen) render(t *rtype, mod string) string {
	switch t.kind {
	case scalarKind:
		if t.name == "" {
			panic("cachegen: a range with no name has no text")
		}
		return qual(t.module, mod) + t.name
	case boolKind:
		return "bool"
	case stringKind:
		return "String"
	case bytesKind:
		return "ByteArray"
	case arrayKind:
		return "Array with " + paren(g.render(t.elem, mod))
	case frozenKind:
		return "frozen with " + paren(g.render(t.elem, mod))
	case recordKind, unionKind, enumKind:
		s := qual(t.module, mod) + t.name
		if len(t.args) > 0 {
			var as []string
			for _, a := range t.args {
				as = append(as, paren(g.render(a, mod)))
			}
			s += " with " + strings.Join(as, ", ")
		}
		return s
	case intMapKind:
		return qual("table", mod) + "IntMap with " + paren(g.render(t.elem, mod))
	case stringMapKind:
		return qual("table", mod) + "StringMap with " + paren(g.render(t.elem, mod))
	}
	panic("cachegen: a type of no kind")
}

// mangle is the part of the names of t's functions after store and load, in module mod.
func (g *gen) mangle(t *rtype, mod string) string {
	switch t.kind {
	case scalarKind:
		return upper(strings.ReplaceAll(qual(t.module, mod), ".", "")) + upper(t.name)
	case boolKind:
		return "Bool"
	case stringKind:
		return "String"
	case bytesKind:
		return "Bytes"
	case arrayKind:
		return "ArrayOf" + g.mangle(t.elem, mod)
	case frozenKind:
		return "Frozen" + g.mangle(t.elem, mod)
	case recordKind, unionKind, enumKind:
		var s strings.Builder
		s.WriteString(upper(strings.ReplaceAll(qual(t.module, mod), ".", "")) + t.name)
		for i, a := range t.args {
			if i == 0 {
				s.WriteString("Of")
			} else {
				s.WriteString("And")
			}
			s.WriteString(g.mangle(a, mod))
		}
		return s.String()
	case intMapKind:
		return "IntMapOf" + g.mangle(t.elem, mod)
	case stringMapKind:
		return "StringMapOf" + g.mangle(t.elem, mod)
	}
	panic("cachegen: a type of no kind")
}

// host is the module whose generated file holds the functions of t, where from needs them: a record's, a union's or an
// enum's own, which alone may write their private fields, and for an instance the highest module that may name all its
// parts; an array's or a map's, which another module may store as well, the one that needs it.
func (g *gen) host(t *rtype, from string) string {
	switch t.kind {
	case recordKind, unionKind:
		h := t.module
		for _, a := range t.args {
			if ah := g.partHost(a); ah != "" && layerOf(ah) > layerOf(h) {
				h = ah
			}
		}
		return h
	case arrayKind, intMapKind, stringMapKind:
		return from
	}
	panic("cachegen: a type with no functions of its own")
}

// partHost is the module a part of an instance belongs to, or none for a part every module may name.
func (g *gen) partHost(t *rtype) string {
	switch t.kind {
	case scalarKind, enumKind:
		return t.module
	case boolKind, stringKind, bytesKind:
		return ""
	case arrayKind, frozenKind, intMapKind, stringMapKind:
		return g.partHost(t.elem)
	case recordKind, unionKind:
		return g.host(t, "")
	}
	panic("cachegen: a type of no kind")
}

// need is the name, as module from calls it, of the store function of t, whose load is the same with load for store.
func (g *gen) need(t *rtype, from string) string {
	h := g.host(t, from)
	name := g.mangle(t, h)
	if _, ok := g.specs[h][name]; !ok {
		s := &spec{host: h, t: t, name: name}
		g.specs[h][name] = s
		g.order[h] = append(g.order[h], name)
		g.queue = append(g.queue, s)
	}
	if h != from {
		if layerOf(h) > layerOf(from) {
			g.errs = append(g.errs, fmt.Sprintf("module %s needs the functions of %s, which only %s may name", from, g.render(t, h), h))
		}
		g.exported[h][name] = true
	}
	return qual(h, from) + "%s" + name
}

func (g *gen) word(mod string) string { return qual("table", mod) + "Word64" }

// storeStmt is the statement that stores the value at e, of type t, in module mod.
func (g *gen) storeStmt(t *rtype, e, mod string) string {
	switch t.kind {
	case scalarKind:
		return fmt.Sprintf("w.word%d(%s as %s)", 8*t.width, e, g.word(mod))
	case boolKind:
		return fmt.Sprintf("w.flag(%s)", e)
	case stringKind:
		return fmt.Sprintf("w.text(%s)", e)
	case bytesKind:
		return fmt.Sprintf("w.run(%s)", e)
	case enumKind:
		return fmt.Sprintf("w.word%d(%s.ordinal as %s)", 8*t.width, e, g.word(mod))
	case frozenKind:
		return g.storeStmt(t.elem, e, mod)
	case arrayKind, recordKind, unionKind, intMapKind, stringMapKind:
		return fmt.Sprintf(g.need(t, mod), "store") + "(w, v: " + e + ")"
	}
	panic("cachegen: a type of no kind")
}

// loadExpr is the expression that loads a value of type t in module mod.
func (g *gen) loadExpr(t *rtype, mod string) string {
	switch t.kind {
	case scalarKind:
		read := fmt.Sprintf("r.word%d()", 8*t.width)
		name := g.render(t, mod)
		switch t.read {
		case wordRead:
			if t.name == "Count" && t.module == "" {
				return read + " as Count"
			}
			return read + " as " + name
		case signedRead:
			return read + " as " + name
		case checkedRead:
			return fmt.Sprintf("try (%s as %s) otherwise panic(\"cache: a stored %s is out of its range\")", read, name, name)
		case signedCheckedRead:
			return fmt.Sprintf("try (r.int%d() as %s) otherwise panic(\"cache: a stored %s is out of its range\")", 8*t.width, name, name)
		}
		panic("cachegen: a scalar of no read")
	case boolKind:
		return "r.flag()"
	case stringKind:
		return "r.text()"
	case bytesKind:
		return "r.run()"
	case enumKind:
		return fmt.Sprintf("%s.allCases[r.word%d() as Count]", g.render(t, mod), 8*t.width)
	case frozenKind:
		return "freeze(" + g.loadExpr(t.elem, mod) + ")"
	case recordKind:
		g.need(t, mod)
		return g.render(t, mod) + "." + recordLoad + "(r)"
	case arrayKind, unionKind, intMapKind, stringMapKind:
		return fmt.Sprintf(g.need(t, mod), "load") + "(r)"
	}
	panic("cachegen: a type of no kind")
}

// recordLoad is the static function that an extension of a record adds to load it, as only the record's own methods may
// make one of its values.
const recordLoad = "cacheLoad"

// local is the name of a local that holds the field named name, so that no field's name meets the parameters'.
func local(name string) string { return "the" + upper(name) }

// body writes the functions of s.
func (g *gen) body(s *spec) string {
	mod := s.host
	t := s.t
	writer := qual("table", mod) + "CacheWriter"
	reader := qual("table", mod) + "CacheReader"
	ty := g.render(t, mod)
	store := "store" + s.name
	load := "load" + s.name
	if t.kind == recordKind {
		load = recordLoad
	}
	exp := ""
	if g.exported[mod][s.name] {
		exp = "export "
	}
	var b strings.Builder
	switch t.kind {
	case arrayKind:
		fmt.Fprintf(&b, "%sfunction %s(mutates w %s, v %s)\n", exp, store, writer, ty)
		fmt.Fprintf(&b, "\tw.word64(v.count() as %s)\n", g.word(mod))
		fmt.Fprintf(&b, "\tfor i in 0 upto v.count() 'items'\n\t\t%s\n\tend 'items'\nend '%s'\n\n", g.storeStmt(t.elem, "v[i]", mod), store)
		fmt.Fprintf(&b, "%sfunction %s(mutates r %s) returns %s\n", exp, load, reader, ty)
		fmt.Fprintf(&b, "\tlet n = r.items()\n\tvar a as %s\n\ta = Array.create(0)\n", ty)
		fmt.Fprintf(&b, "\tfor _ in 0 upto n 'items'\n\t\ta.push(%s)\n\tend 'items'\n\treturn a\nend '%s'\n", g.loadExpr(t.elem, mod), load)
	case intMapKind, stringMapKind:
		keyStore, keyLoad, keyNone, keys := "w.word64(v.keyAt(i) as "+g.word(mod)+")", "r.word64() as Count", "0", "Array with Count"
		if t.kind == stringMapKind {
			keyStore, keyLoad, keyNone, keys = "w.text(v.keyAt(i))", "r.text()", "\"\"", "Array with String"
		}
		mapType := qual("table", mod) + "IntMap"
		if t.kind == stringMapKind {
			mapType = qual("table", mod) + "StringMap"
		}
		occupied := qual("table", mod) + "mapSlotOccupied"
		option := &rtype{kind: unionKind, module: "table", name: "Option", args: []*rtype{t.elem}, d: g.w.decls["table"]["Option"]}
		fmt.Fprintf(&b, "%sfunction %s(mutates w %s, v %s)\n", exp, store, writer, ty)
		fmt.Fprintf(&b, "\tlet slots = v.capacity()\n\tw.word64(slots as %s)\n", g.word(mod))
		fmt.Fprintf(&b, "\tfor i in 0 upto slots 'states'\n\t\tw.word8(v.stateAt(i) as %s)\n\tend 'states'\n", g.word(mod))
		fmt.Fprintf(&b, "\tfor i in 0 upto slots 'entries'\n\t\tif %s(v.stateAt(i)) 'occupied'\n\t\t\t%s\n\t\t\t%s\n\t\tend 'occupied'\n\tend 'entries'\n", occupied, keyStore, g.storeStmt(option, "v.valueAt(i)", mod))
		fmt.Fprintf(&b, "\tw.word64(v.count() as %s)\n\tw.word64(v.usedSlots() as %s)\nend '%s'\n\n", g.word(mod), g.word(mod), store)
		fmt.Fprintf(&b, "%sfunction %s(mutates r %s) returns %s\n", exp, load, reader, ty)
		fmt.Fprintf(&b, "\tlet slots = r.items()\n\tvar states = ByteArray.create(0)\n")
		fmt.Fprintf(&b, "\tfor _ in 0 upto slots 'states'\n\t\tstates.push(try (r.word8() as Byte) otherwise panic(\"cache: a stored state of a map is no byte\"))\n\tend 'states'\n")
		fmt.Fprintf(&b, "\tvar keys as %s\n\tkeys = Array.create(0)\n", keys)
		fmt.Fprintf(&b, "\tvar values as Array with %s\n\tvalues = Array.create(0)\n", paren(g.render(option, mod)))
		fmt.Fprintf(&b, "\tfor i in 0 upto slots 'entries'\n\t\tif %s(states[i] as Count) 'occupied'\n\t\t\tkeys.push(%s)\n\t\t\tvalues.push(%s)\n", occupied, keyLoad, g.loadExpr(option, mod))
		fmt.Fprintf(&b, "\t\tend 'occupied' else 'free'\n\t\t\tkeys.push(%s)\n\t\t\tvalues.push(%sOption.none)\n\t\tend 'free'\n\tend 'entries'\n", keyNone, qual("table", mod))
		fmt.Fprintf(&b, "\tlet length = r.word64() as Count\n\tlet used = r.word64() as Count\n")
		fmt.Fprintf(&b, "\treturn %s.restored(states, keys: keys, values: values, length: length, used: used)\nend '%s'\n", mapType, load)
	case recordKind:
		subst := g.subst(t)
		fmt.Fprintf(&b, "%sfunction %s(mutates w %s, v %s)\n", exp, store, writer, ty)
		var fields []*rtype
		for _, f := range t.d.fields {
			if _, ok := derived[t.d.module+"."+t.d.name+"."+f.name]; ok {
				fields = append(fields, nil)
				continue
			}
			ft, err := g.w.resolve(t.d.module, f.typ, subst, t.d.module+"."+t.d.name+"."+f.name)
			if err != nil {
				g.errs = append(g.errs, err.Error())
				return ""
			}
			fields = append(fields, ft)
			fmt.Fprintf(&b, "\t%s\n", g.storeStmt(ft, "v."+f.name, mod))
		}
		fmt.Fprintf(&b, "end '%s'\n\n", store)
		fmt.Fprintf(&b, "extension %s\n\t%sstatic function %s(mutates r %s) returns %s\n", t.name, exp, load, reader, ty)
		var inits []string
		for i, f := range t.d.fields {
			if fields[i] == nil {
				fmt.Fprintf(&b, "\t\tlet %s = %s\n", local(f.name), derived[t.d.module+"."+t.d.name+"."+f.name])
			} else {
				fmt.Fprintf(&b, "\t\tlet %s = %s\n", local(f.name), g.loadExpr(fields[i], mod))
			}
			inits = append(inits, f.name+": "+local(f.name))
		}
		fmt.Fprintf(&b, "\t\treturn %s{%s}\n\tend '%s'\nend '%s'\n", ty, strings.Join(inits, ", "), load, t.name)
	case unionKind:
		subst := g.subst(t)
		var cases [][]*rtype
		for _, c := range t.d.cases {
			var ps []*rtype
			for _, p := range c.payload {
				pt, err := g.w.resolve(t.d.module, p.typ, subst, t.d.module+"."+t.d.name+"."+c.name+"."+p.name)
				if err != nil {
					g.errs = append(g.errs, err.Error())
					return ""
				}
				ps = append(ps, pt)
			}
			cases = append(cases, ps)
		}
		tag := 1
		if len(t.d.cases) > 256 {
			tag = 2
		}
		fmt.Fprintf(&b, "%sfunction %s(mutates w %s, v %s)\n\tmatch v 'v'\n", exp, store, writer, ty)
		for i, c := range t.d.cases {
			if len(c.payload) == 0 {
				fmt.Fprintf(&b, "\t\t%s then w.word%d(%d as %s)\n", c.name, 8*tag, i, g.word(mod))
				continue
			}
			var names, args []string
			for j := range c.payload {
				names = append(names, fmt.Sprintf("p%d", j))
				args = append(args, fmt.Sprintf("p%d: p%d", j, j))
			}
			fmt.Fprintf(&b, "\t\t%s(%s) then %s%s(w, %s)\n", c.name, strings.Join(names, ", "), store, upper(c.name), strings.Join(args, ", "))
		}
		fmt.Fprintf(&b, "\tend 'v'\nend '%s'\n\n", store)
		for i, c := range t.d.cases {
			if len(c.payload) == 0 {
				continue
			}
			var params []string
			for j := range c.payload {
				params = append(params, fmt.Sprintf("p%d %s", j, g.render(cases[i][j], mod)))
			}
			fmt.Fprintf(&b, "function %s%s(mutates w %s, %s)\n\tw.word%d(%d as %s)\n", store, upper(c.name), writer, strings.Join(params, ", "), 8*tag, i, g.word(mod))
			for j := range c.payload {
				fmt.Fprintf(&b, "\t%s\n", g.storeStmt(cases[i][j], fmt.Sprintf("p%d", j), mod))
			}
			fmt.Fprintf(&b, "end '%s%s'\n\n", store, upper(c.name))
		}
		fmt.Fprintf(&b, "%sfunction %s(mutates r %s) returns %s\n\tlet tag = r.word%d()\n", exp, load, reader, ty, 8*tag)
		cons := qual(t.module, mod) + t.name
		for i, c := range t.d.cases {
			fmt.Fprintf(&b, "\tif tag == %d '%s'\n", i, c.name)
			if len(c.payload) == 0 {
				fmt.Fprintf(&b, "\t\treturn %s.%s\n", cons, c.name)
			} else {
				var args []string
				for j, p := range c.payload {
					fmt.Fprintf(&b, "\t\tlet %s = %s\n", local(p.name), g.loadExpr(cases[i][j], mod))
					if j == 0 {
						args = append(args, local(p.name))
					} else {
						args = append(args, p.name+": "+local(p.name))
					}
				}
				fmt.Fprintf(&b, "\t\treturn %s.%s(%s)\n", cons, c.name, strings.Join(args, ", "))
			}
			fmt.Fprintf(&b, "\tend '%s'\n", c.name)
		}
		fmt.Fprintf(&b, "\tpanic(\"cache: a stored %s has no case {tag}\")\nend '%s'\n", t.name, load)
	default:
		panic("cachegen: functions of a type that has none")
	}
	return b.String()
}

// subst is what the type parameters of t's declaration stand for.
func (g *gen) subst(t *rtype) map[string]*rtype {
	m := map[string]*rtype{}
	for i, p := range t.d.params {
		m[p] = t.args[i]
	}
	return m
}

// run makes the functions of the roots and of every type they reach, and gives each module's file.
func (g *gen) run(roots map[string][]string) (map[string]string, error) {
	for _, m := range layers {
		for _, name := range roots[m] {
			d, ok := g.w.decls[m][name]
			if !ok {
				return nil, fmt.Errorf("the root %s.%s is not declared", m, name)
			}
			t, err := g.w.named(d, nil, "root "+m+"."+name)
			if err != nil {
				return nil, err
			}
			g.need(t, m)
			g.exported[m][g.mangle(t, m)] = true
		}
	}
	bodies := map[string]string{}
	for len(g.queue) > 0 {
		s := g.queue[0]
		g.queue = g.queue[1:]
		bodies[s.host+"\x00"+s.name] = ""
		_ = g.body(s)
	}
	if len(g.errs) > 0 {
		sort.Strings(g.errs)
		return nil, fmt.Errorf("%s", strings.Join(g.errs, "\n"))
	}
	files := map[string]string{}
	for _, m := range layers {
		if len(g.order[m]) == 0 {
			continue
		}
		var b strings.Builder
		b.WriteString("// " + header + "\n")
		names := append([]string(nil), g.order[m]...)
		sort.Strings(names)
		for _, name := range names {
			b.WriteString("\n")
			b.WriteString(g.body(g.specs[m][name]))
		}
		files[m] = b.String()
	}
	return files, nil
}
