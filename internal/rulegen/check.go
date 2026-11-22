package main

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"minimaxon/internal/irgen/irtable"
	"minimaxon/internal/x64gen/x64table"
)

// target is what the rules of a file rewrite to.
type target int

const (
	// genericTarget rules rewrite an IR instruction to a register or to IR instructions, and an instruction no rule
	// matches stays as it is.
	genericTarget target = iota
	// x64Target rules select the x64 forms of an IR operation, so every operation needs a rule that matches all of it.
	x64Target
)

// checked is a rule that the generator makes code of: the rule, and its patterns, the rule's own and those that swap the
// operands of a commutative operation, without one that an earlier one matches everything of.
type checked struct {
	r        *rule
	variants []*term
}

// binding is a variable of a rule: whether it is a register, and how often the pattern names it.
type binding struct {
	reg   bool
	count int
	// operand is the operand of the pattern that a variable that is no register is bound to.
	operand irtable.Operand
}

// checkRules resolves the rules against the tables and refuses the first that names what they do not have, uses a
// variable it does not bind, or can never fire.
func checkRules(file string, rules []*rule, tg target) []checked {
	var out []checked
	for _, r := range rules {
		vars := map[string]*binding{}
		checkMatch(r, r.match, vars, true, tg)
		if tg == x64Target {
			checkMachineNames(r, vars)
		}
		if r.cond != "" {
			rewriteExpr(r, r.cond, vars, nil, tg == x64Target)
		}
		switch tg {
		case genericTarget:
			checkGenericResult(r, vars)
		case x64Target:
			checkMachineResults(r, vars)
		default:
			panic(fmt.Sprintf("rulegen: target %d", tg))
		}
		c := checked{r: r}
		for _, v := range variants(r.match) {
			if !slices.ContainsFunc(c.variants, func(u *term) bool { return covers(u, v) }) {
				c.variants = append(c.variants, v)
			}
		}
		if prev := neverFires(out, c); prev != nil {
			r.fail("can never fire: the rule at line %d, %s, matches everything it matches", prev.line, prev.text)
		}
		out = append(out, c)
	}
	if tg == x64Target {
		checkTotal(file, rules)
	}
	return out
}

// neverFires is the earlier rule with no condition that matches one of the variants of c, if one does for each.
func neverFires(earlier []checked, c checked) *rule {
	var first *rule
	for _, v := range c.variants {
		var by *rule
		for _, e := range earlier {
			if e.r.cond == "" && slices.ContainsFunc(e.variants, func(u *term) bool { return covers(u, v) }) {
				by = e.r
				break
			}
		}
		if by == nil {
			return nil
		}
		if first == nil {
			first = by
		}
	}
	return first
}

func opNamed(name string) (irtable.Op, bool) {
	for _, o := range irtable.Ops {
		if lowerFirst(o.Name) == name {
			return o, true
		}
	}
	return irtable.Op{}, false
}

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func upperFirst(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// positionals are the operands of an operation that a term gives in order: all but its enum case and flags, which are
// suffixes of its name, and the arguments of its edges, which no pattern can read and a rule for a terminator keeps with
// the edge.
func positionals(o irtable.Op) []irtable.Operand {
	var out []irtable.Operand
	for _, x := range o.Operands {
		if x.Kind != irtable.KEnum && x.Kind != irtable.KFlag && x.Kind != irtable.KEdgeArgs && x.Kind != irtable.KMoreEdgeArgs {
			out = append(out, x)
		}
	}
	return out
}

func enumOf(name string) irtable.Enum {
	for _, e := range irtable.Enums {
		if e.Name == name {
			return e
		}
	}
	panic("rulegen: the IR table has no enum " + name)
}

func enumOperand(o irtable.Op) (irtable.Operand, bool) {
	for _, x := range o.Operands {
		if x.Kind == irtable.KEnum {
			return x, true
		}
	}
	return irtable.Operand{}, false
}

// flagOperand is the operand of o that the flag of a suffix (`unsigned`, `checked`) says is there or set.
func flagOperand(o irtable.Op, suffix string) (irtable.Operand, bool) {
	for _, x := range o.Operands {
		if x.Flag != "" && lowerFirst(x.Flag) == suffix {
			return x, true
		}
	}
	return irtable.Operand{}, false
}

// optional is whether an operand may be absent, as irgen says.
func optional(x irtable.Operand) bool {
	return x.Kind == irtable.KRoutine || x.Kind == irtable.KErrorReg || (x.Kind == irtable.KFail && x.Flag != "")
}

func isList(x irtable.Operand) bool {
	return x.Kind == irtable.KRegs || x.Kind == irtable.KMoreRegs || x.Kind == irtable.KCases
}

// resolveOp sets what a termOp of the IR names, and refuses a suffix or a number of operands the operation does not have.
func resolveOp(r *rule, t *term) {
	o, ok := opNamed(t.name)
	if !ok {
		r.fail("there is no IR operation %s", t.name)
	}
	t.op, t.cas, t.flags = o, "", map[string]bool{}
	for _, s := range t.suffixes {
		name, negated := strings.CutPrefix(s, "!")
		if x, ok := flagOperand(o, name); ok {
			if _, twice := t.flags[x.Flag]; twice {
				r.fail("%s names the flag %s twice", t.name, name)
			}
			t.flags[x.Flag] = !negated
			continue
		}
		x, ok := enumOperand(o)
		if negated || !ok {
			r.fail("%s has no flag %s", t.name, name)
		}
		e := enumOf(x.Enum)
		i := slices.IndexFunc(e.Cases, func(c irtable.EnumCase) bool { return c.Text == name })
		if i < 0 {
			r.fail("%s has no flag %s, and %s has no case %s", t.name, name, x.Enum, name)
		}
		if t.cas != "" {
			r.fail("%s names two cases of %s", t.name, x.Enum)
		}
		t.cas = e.Cases[i].Name
	}
	pos := positionals(o)
	if len(t.args) > len(pos) {
		r.fail("%s has %d operands after its case and flags (%s), and the rule gives it %d", t.name, len(pos), operandNames(pos), len(t.args))
	}
	for i, a := range t.args {
		x := pos[i]
		if (x.Kind == irtable.KReg) != (a.kind != termAux) {
			form := "[...]"
			if x.Kind == irtable.KReg {
				form = "a register's term: a variable, _ or an operation"
			}
			r.fail("%s's operand %s is written %s", t.name, x.Name, form)
		}
	}
}

func operandNames(xs []irtable.Operand) string {
	names := make([]string, len(xs))
	for i, x := range xs {
		names[i] = lowerFirst(x.Name)
	}
	return strings.Join(names, " ")
}

func declare(r *rule, vars map[string]*binding, name string, reg bool) {
	b, ok := vars[name]
	if !ok {
		vars[name] = &binding{reg: reg, count: 1}
		return
	}
	if b.reg != reg {
		r.fail("%s names a register in one place and a value that is no register in another", name)
	}
	b.count++
}

var number = regexp.MustCompile(`^-?[0-9]+$`)

// matchable are the operand kinds whose value a pattern can read: those it can compare with a number, and those it can
// only bind.
var (
	numeric  = []irtable.Kind{irtable.KInt, irtable.KSlot, irtable.KWord}
	bindable = []irtable.Kind{irtable.KInt, irtable.KSlot, irtable.KWord, irtable.KSymbol, irtable.KBody, irtable.KDatum, irtable.KFrameBox, irtable.KBlock, irtable.KFail,
		irtable.KText, irtable.KType, irtable.KCallee, irtable.KTable}
)

func checkMatch(r *rule, t *term, vars map[string]*binding, root bool, tg target) {
	resolveOp(r, t)
	o := t.op
	if root && o.Term && tg == genericTarget {
		r.fail("a generic rule rewrites an instruction, and %s is a terminator", t.name)
	}
	if (!root || t.bind != "") && o.Result == irtable.NoResult {
		r.fail("%s gives no register for an operand to be", t.name)
	}
	if t.bind != "" {
		declare(r, vars, t.bind, true)
	}
	pos := positionals(o)
	for i, a := range t.args {
		x := pos[i]
		switch a.kind {
		case termWild:
		case termVar:
			declare(r, vars, a.name, true)
		case termOp:
			checkMatch(r, a, vars, false, tg)
		case termAux:
			switch {
			case a.text == "_":
			case isList(x) || !slices.Contains(bindable, x.Kind):
				r.fail("%s's operand %s can only be matched by [_]", t.name, x.Name)
			case number.MatchString(a.text):
				if !slices.Contains(numeric, x.Kind) {
					r.fail("%s's operand %s is no number", t.name, x.Name)
				}
			default:
				(&termParser{r: r}).variable(a.text)
				if x.Kind == irtable.KFail && x.Flag != "" && !t.flags[x.Flag] {
					r.fail("only a %s with .%s has the block %s, which the pattern binds", t.name, lowerFirst(x.Flag), lowerFirst(x.Name))
				}
				declare(r, vars, a.text, false)
				vars[a.text].operand = x
			}
		default:
			panic(fmt.Sprintf("rulegen: a term of kind %d", a.kind))
		}
	}
}

func checkGenericResult(r *rule, vars map[string]*binding) {
	if len(r.results) != 1 {
		r.fail("a generic rule rewrites to one register or one instruction, and this one gives %d", len(r.results))
	}
	res := r.results[0]
	switch res.kind {
	case termVar:
		b, ok := vars[res.name]
		if !ok {
			r.fail("%s is no variable of the pattern", res.name)
		}
		if !b.reg {
			r.fail("%s is no register", res.name)
		}
		if r.match.op.Result == irtable.NoResult {
			r.fail("%s gives no register for %s to replace", r.match.name, res.name)
		}
	case termOp:
		if res.bind != "" {
			r.fail("%s: binds the register of the rule's own instruction, which replaces the one it rewrites", res.bind)
		}
		made := map[string]int{}
		checkResult(r, res, vars, made, true)
		for _, name := range slices.Sorted(maps.Keys(made)) {
			if made[name] == 0 {
				r.fail("%s: binds a register in a result that nothing after it reads", name)
			}
		}
		if (r.match.op.Result == irtable.NoResult) != (res.op.Result == irtable.NoResult) {
			r.fail("%s and %s do not both give a register", r.match.name, res.name)
		}
	case termWild, termAux:
		r.fail("a generic rule rewrites to a register of the pattern or to an instruction")
	default:
		panic(fmt.Sprintf("rulegen: a term of kind %d", res.kind))
	}
}

// checkResult refuses an IR operation of a result that the generated code cannot build. A nested operation that name:
// binds defines a register that the operands after it, in the order the instructions are built (operands before the
// operation, left to right), may read again; made counts the reads of each.
func checkResult(r *rule, t *term, vars map[string]*binding, made map[string]int, root bool) {
	resolveOp(r, t)
	o := t.op
	if o.Term {
		r.fail("a generic rule rewrites to instructions, and %s is a terminator", t.name)
	}
	if !root && o.Result != irtable.ValueResult {
		r.fail("%s gives no register for an operand to be", t.name)
	}
	for _, s := range t.suffixes {
		name, negated := strings.CutPrefix(s, "!")
		if negated {
			r.fail("a result leaves out the flags that are clear, and names %s", s)
		}
		if x, ok := flagOperand(o, name); ok && x.Kind != irtable.KFlag {
			r.fail("a result gives %s its operand %s, which says it is .%s, rather than the flag", t.name, lowerFirst(x.Name), name)
		}
	}
	if x, ok := enumOperand(o); ok && t.cas == "" {
		r.fail("%s names no case of %s", t.name, x.Enum)
	}
	pos := positionals(o)
	for i, x := range pos {
		if isList(x) {
			r.fail("a result cannot build %s, whose operand %s is a list", t.name, x.Name)
		}
		if i >= len(t.args) {
			if !optional(x) {
				r.fail("%s is given no %s", t.name, lowerFirst(x.Name))
			}
			continue
		}
		a := t.args[i]
		switch a.kind {
		case termWild:
			r.fail("a result has no _: it gives %s's %s", t.name, lowerFirst(x.Name))
		case termVar:
			if n, ok := made[a.name]; ok {
				made[a.name] = n + 1
				continue
			}
			b, ok := vars[a.name]
			if !ok {
				r.fail("%s is no variable of the pattern, nor a register the result binds before it", a.name)
			}
			if !b.reg {
				r.fail("%s is no register", a.name)
			}
		case termOp:
			checkResult(r, a, vars, made, false)
			if a.bind != "" {
				if _, ok := vars[a.bind]; ok {
					r.fail("%s: binds a register in a result that the pattern names already", a.bind)
				}
				if _, ok := made[a.bind]; ok {
					r.fail("%s: binds two registers in a result", a.bind)
				}
				made[a.bind] = 0
			}
		case termAux:
			if a.text == "_" {
				r.fail("a result has no _: it gives %s's %s", t.name, lowerFirst(x.Name))
			}
			rewriteExpr(r, a.text, vars, nil, false)
		default:
			panic(fmt.Sprintf("rulegen: a term of kind %d", a.kind))
		}
	}
}

// machineContext are the names that the conditions and expressions of an instruction selection rule may name besides
// its variables: the selector, the IR function and the instruction being selected.
var machineContext = []string{"sel", "fn", "ins"}

// copyForm is the copy of the machine IR, which a rule names as it names a form, (Copy dst src): it is how a value is
// put in a fixed register and taken out of one.
const copyForm = "Copy"

// shapeOf is the shape of what a result names: a form, a choice or the copy.
func shapeOf(r *rule, res *term) x64table.Shape {
	if res.name == copyForm {
		return x64table.Shape{Form: copyForm, Params: []x64table.ShapeParam{
			{Name: "dst", Role: x64table.RoleDef, Letter: "a"}, {Name: "src", Role: x64table.RoleUse, Letter: "b"}}}
	}
	sh, ok := x64table.ShapeOf(res.name)
	if !ok {
		r.fail("there is no x64 form or choice %s", res.name)
	}
	return sh
}

// isHelper is whether a result is a call of a hand-written selection helper: a name that starts with a lowercase
// letter, which no form has.
func isHelper(res *term) bool {
	return res.kind == termOp && res.name[0] >= 'a' && res.name[0] <= 'z'
}

func isRegisterName(name string) bool { return slices.Contains(x64table.Registers(), name) }

// checkMachineNames refuses a variable of a pattern that is named like a register or a name of the context, which the
// results and conditions would read as that instead.
func checkMachineNames(r *rule, vars map[string]*binding) {
	for name := range vars {
		if isRegisterName(name) || slices.Contains(machineContext, name) {
			r.fail("%s is a register or a name of the selector's, and no variable", name)
		}
	}
}

// checkMachineResults refuses results that the selection code cannot build: an x64 form that the table does not have,
// one given the wrong number or kind of operands, a register read before it is defined or defined twice, or a rule
// that gives the operation no result.
func checkMachineResults(r *rule, vars map[string]*binding) {
	if len(r.results) == 1 && isHelper(r.results[0]) {
		h := r.results[0]
		if h.bind != "" || len(h.suffixes) > 0 || len(h.args) > 0 {
			r.fail("%s: a helper is called with the selector, the function and the instruction, and no operands", h)
		}
		return
	}
	root := r.match
	defined := map[string]bool{}
	defs := 0
	use := func(res *term, p x64table.ShapeParam, a *term) {
		if a.kind != termVar {
			r.fail("%s's %s is a register: a variable or a register's name", res.name, p.Name)
		}
		if isRegisterName(a.name) || defined[a.name] {
			return
		}
		b, ok := vars[a.name]
		switch {
		case !ok:
			r.fail("%s's %s reads %s, which nothing defines before it", res.name, p.Name, a.name)
		case !b.reg:
			r.fail("%s is no register", a.name)
		case a.name == root.bind:
			r.fail("%s's %s reads %s, the result, before a form defines it", res.name, p.Name, a.name)
		}
	}
	for _, res := range r.results {
		if res.kind != termOp {
			r.fail("an instruction selection rule rewrites to x64 forms, and %s is none", res)
		}
		if isHelper(res) {
			r.fail("%s: a helper is the only result of its rule", res)
		}
		if res.bind != "" || len(res.suffixes) > 0 {
			r.fail("%s: an x64 form has no binding and no suffixes", res)
		}
		sh := shapeOf(r, res)
		if len(res.args) != len(sh.Params) {
			names := make([]string, len(sh.Params))
			for i, p := range sh.Params {
				names[i] = p.Name
			}
			r.fail("%s takes %d operands (%s), and the rule gives it %d", res.name, len(sh.Params), strings.Join(names, " "), len(res.args))
		}
		for i, a := range res.args {
			p := sh.Params[i]
			switch p.Role {
			case x64table.RoleUse:
				use(res, p, a)
			case x64table.RoleReuse:
				use(res, p, a)
				if !isRegisterName(a.name) {
					defs++
					if a.name == root.bind {
						defined[a.name] = true
					}
				}
			case x64table.RoleDef:
				if a.kind != termVar {
					r.fail("%s's %s is a register: a variable or a register's name", res.name, p.Name)
				}
				if isRegisterName(a.name) {
					continue
				}
				if slices.Contains(machineContext, a.name) {
					r.fail("%s is a name of the selector's, and no variable", a.name)
				}
				if _, ok := vars[a.name]; ok && a.name != root.bind {
					r.fail("%s's %s defines %s, a register of the pattern", res.name, p.Name, a.name)
				}
				if defined[a.name] {
					r.fail("%s's %s defines %s again: a two-address form redefines the register it reads", res.name, p.Name, a.name)
				}
				defined[a.name] = true
				defs++
			case x64table.RoleRel:
				if a.kind != termAux {
					r.fail("%s's %s is a [value]", res.name, p.Name)
				}
				if isEdgeVar(vars, a.text) {
					continue
				}
				rewriteExpr(r, a.text, vars, nil, true)
			case x64table.RoleDisp, x64table.RoleImm, x64table.RoleCond, x64table.RoleRip:
				if a.kind != termAux {
					r.fail("%s's %s is a [value]", res.name, p.Name)
				}
				rewriteExpr(r, a.text, vars, nil, true)
			default:
				panic(fmt.Sprintf("rulegen: role %d", p.Role))
			}
		}
	}
	if root.op.Result != irtable.NoResult && defs == 0 {
		r.fail("%s gives a register, and no form defines one", root.name)
	}
	if root.bind != "" && !defined[root.bind] {
		r.fail("no form defines %s, the result", root.bind)
	}
}

// isEdgeVar is whether a variable is bound to a block that the instruction branches to, which a rel32 then goes to.
func isEdgeVar(vars map[string]*binding, name string) bool {
	b, ok := vars[name]
	return ok && !b.reg && (b.operand.Kind == irtable.KBlock || b.operand.Kind == irtable.KFail)
}

// checkTotal refuses rules that leave an IR operation, of some case of its enum and some value of its flags, with no
// rule that matches all of it, which instruction selection would have no forms for.
func checkTotal(file string, rules []*rule) {
	for _, o := range irtable.Ops {
		for _, whole := range wholes(o) {
			if !slices.ContainsFunc(rules, func(r *rule) bool { return r.cond == "" && covers(r.match, whole) }) {
				panic(&ruleError{fmt.Sprintf("%s: no rule matches every %s: the last rule of an operation has no condition and matches any operands", file, strings.Trim(whole.String(), "()"))})
			}
		}
	}
}

// wholes are the patterns of an operation with each case of its enum and each value of its flags, and nothing else.
func wholes(o irtable.Op) []*term {
	out := []*term{{kind: termOp, name: lowerFirst(o.Name), op: o, flags: map[string]bool{}}}
	if x, ok := enumOperand(o); ok {
		var next []*term
		for _, c := range enumOf(x.Enum).Cases {
			n := *out[0]
			n.cas, n.suffixes = c.Name, []string{c.Text}
			n.flags = map[string]bool{}
			next = append(next, &n)
		}
		out = next
	}
	for _, x := range o.Operands {
		if x.Flag == "" {
			continue
		}
		var next []*term
		for _, t := range out {
			for _, set := range []bool{false, true} {
				n := *t
				n.flags = maps.Clone(t.flags)
				n.flags[x.Flag] = set
				suffix := lowerFirst(x.Flag)
				if !set {
					suffix = "!" + suffix
				}
				n.suffixes = append(slices.Clone(t.suffixes), suffix)
				next = append(next, &n)
			}
		}
		out = next
	}
	return out
}

// commutative are the cases of the operations whose two register operands can be swapped.
var commutative = map[string][]string{
	"Bin": {"Add", "Mul", "And", "Or", "Xor"},
	"Cmp": {"Eq", "Ne"},
}

// variants are the patterns that t and every swap of the operands of its commutative operations make.
func variants(t *term) []*term {
	if t.kind != termOp {
		return []*term{t}
	}
	out := []*term{{}}
	for _, a := range t.args {
		var next []*term
		for _, prefix := range out {
			for _, v := range variants(a) {
				n := *t
				n.args = append(slices.Clone(prefix.args), v)
				next = append(next, &n)
			}
		}
		out = next
	}
	if len(t.args) == 0 {
		out = []*term{t}
	}
	if !slices.Contains(commutative[t.op.Name], t.cas) || len(t.args) < 2 {
		return out
	}
	var swapped []*term
	for _, v := range out {
		n := *v
		n.args = slices.Clone(v.args)
		n.args[0], n.args[1] = n.args[1], n.args[0]
		swapped = append(swapped, &n)
	}
	return append(out, swapped...)
}

// covers is whether every operation that q matches p matches too. A pattern that names a variable twice asks for equal
// values, which covers counts only where q is the same pattern.
func covers(p, q *term) bool {
	if p.String() == q.String() {
		return true
	}
	if repeats(p, map[string]bool{}) {
		return false
	}
	return coversTerm(p, q)
}

func repeats(t *term, seen map[string]bool) bool {
	name := ""
	switch t.kind {
	case termVar:
		name = t.name
	case termAux:
		if t.text != "_" && !number.MatchString(t.text) {
			name = t.text
		}
	case termOp:
		if t.bind != "" && seen[t.bind] {
			return true
		}
		seen[t.bind] = true
		for _, a := range t.args {
			if repeats(a, seen) {
				return true
			}
		}
		return false
	case termWild:
	}
	if name == "" {
		return false
	}
	if seen[name] {
		return true
	}
	seen[name] = true
	return false
}

func coversTerm(p, q *term) bool {
	switch p.kind {
	case termWild, termVar:
		return true
	case termAux:
		if p.text == "_" || !number.MatchString(p.text) {
			return true
		}
		return q.kind == termAux && q.text == p.text
	case termOp:
		if q.kind != termOp || p.op.Name != q.op.Name || p.cas != "" && p.cas != q.cas {
			return false
		}
		for f, set := range p.flags {
			if qs, ok := q.flags[f]; !ok || qs != set {
				return false
			}
		}
		for i, a := range p.args {
			b := &term{kind: termWild}
			if i < len(q.args) {
				b = q.args[i]
			}
			if !coversTerm(a, b) {
				return false
			}
		}
		return true
	}
	panic(fmt.Sprintf("rulegen: a term of kind %d", p.kind))
}

var keywords = []string{"and", "or", "not", "true", "false"}

// rewriteExpr is a Maxon expression of a rule with each variable written as locals names it, and refuses a name that is
// none of the rule's variables. A name that a call, a label or a qualified name has is Maxon's, which its compile
// checks, and so, when open, is every other name: an instruction selection rule names the selector, the function and
// the instruction (machineContext) and the backend's constants.
func rewriteExpr(r *rule, expr string, vars map[string]*binding, locals func(string) string, open bool) string {
	var b strings.Builder
	for i := 0; i < len(expr); {
		c := expr[i]
		switch {
		case c == '"':
			j := strings.IndexByte(expr[i+1:], '"')
			if j < 0 {
				r.fail("a string in %q is not closed", expr)
			}
			b.WriteString(expr[i : i+j+2])
			i += j + 2
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(expr) && (expr[j] == '_' || expr[j] >= 'a' && expr[j] <= 'z' || expr[j] >= 'A' && expr[j] <= 'Z' || expr[j] >= '0' && expr[j] <= '9') {
				j++
			}
			name := expr[i:j]
			rest := strings.TrimLeft(expr[j:], " ")
			qualified := i > 0 && expr[i-1] == '.' || strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "(") || strings.HasPrefix(rest, ":")
			switch {
			case qualified || slices.Contains(keywords, name):
				b.WriteString(name)
			case vars[name] != nil:
				if locals != nil {
					b.WriteString(locals(name))
				} else {
					b.WriteString(name)
				}
			case open:
				b.WriteString(name)
			default:
				r.fail("%s is no variable of the pattern", name)
			}
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(expr) && (expr[j] >= '0' && expr[j] <= '9' || expr[j] == 'x' || expr[j] >= 'a' && expr[j] <= 'f' || expr[j] >= 'A' && expr[j] <= 'F' || expr[j] == '_') {
				j++
			}
			b.WriteString(expr[i:j])
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
