package main

import (
	"slices"
	"strings"

	"minimaxon/internal/irgen/irtable"
)

// self/ir/evaluate_gen.maxon is what each operation of the table computes on the words of its operands, written from the
// semantics the table gives (irtable/semantics.go). The algorithms those name are self/ir/evaluate.maxon's.

type (
	eval       = irtable.Eval
	caseEval   = irtable.CaseEval
	order      = irtable.Order
	evalParams = []evalParam
)

// evalParam is a parameter of an operation's evaluator.
type evalParam struct {
	name, typ string
	// operand is the table operand it comes from.
	operand operand
}

// shape is what an evaluator gives.
type shape int

const (
	shapeOutcome shape = iota // an Outcome, and the value in a `sets` parameter
	shapeFlag                 // whether something holds
	shapeWord                 // a word
)

const (
	maxonOutcome     = "Outcome"
	outcomeValue     = "Outcome.outcomeValue"
	outcomeFails     = "Outcome.outcomeFails"
	outcomeUndefined = "Outcome.outcomeUndefined"
)

// evaluated are the operations that compute a value of their operands, which have an evaluator.
func evaluated() []op {
	var out []op
	for _, o := range ops {
		if o.Eval.Form == irtable.EvalByCase || o.Eval.Form == irtable.EvalCall {
			out = append(out, o)
		}
	}
	return out
}

func maxonEvaluator(o op) string { return "eval" + o.Name }

// evalParamsOf are the parameters of the evaluator of an operation: the enum that says what it computes, then its
// registers as the words a and b, its other words, and its flags by name. A failure block that a flag makes there is the
// parameter `checked`.
func evalParamsOf(o op) evalParams {
	var enumOp, regs, words, flagsOf evalParams
	letters := []string{"a", "b"}
	for _, x := range o.Operands {
		switch {
		case x.Kind == kEnum && x.Name == o.Eval.Of:
			enumOp = append(enumOp, evalParam{lowerFirst(x.Name), x.Enum, x})
		case x.Kind == kReg:
			if len(regs) == len(letters) {
				fail("%s has more than two registers, which an evaluator names a and b", o.Name)
			}
			regs = append(regs, evalParam{letters[len(regs)], "Word", x})
		case x.Kind == kWord:
			words = append(words, evalParam{lowerFirst(x.Name), "Word", x})
		case x.Kind == kFlag:
			flagsOf = append(flagsOf, evalParam{lowerFirst(x.Flag), "bool", x})
		case x.Kind == kFail && x.Flag != "":
			flagsOf = append(flagsOf, evalParam{lowerFirst(x.Flag), "bool", x})
		case x.Kind == kFail:
		default:
			fail("%s.%s is computed from, but is neither a register, a word, a flag, nor the enum of its cases", o.Name, x.Name)
		}
	}
	return slices.Concat(enumOp, regs, words, flagsOf)
}

func hasParam(ps evalParams, name string) bool {
	return slices.ContainsFunc(ps, func(p evalParam) bool { return p.name == name })
}

// shapeOf is what the evaluator of an operation gives.
func shapeOf(o op) shape {
	if o.Eval.Form == irtable.EvalCall {
		if o.Eval.Fits != "" {
			return shapeOutcome
		}
		if isCheck(o) {
			return shapeFlag
		}
		return shapeWord
	}
	cases := enumOf(operandOf(o, o.Eval.Of).Enum).Cases
	switch cases[0].Eval.Form {
	case irtable.CaseWrap, irtable.CaseDivide, irtable.CaseBitwise, irtable.CaseShift, irtable.CaseHigh:
		return shapeOutcome
	case irtable.CaseOrder, irtable.CaseRange:
		return shapeFlag
	case irtable.CaseDouble:
		return shapeWord
	case irtable.CaseNone:
	}
	fail("%s: the cases of %s compute nothing", o.Name, o.Eval.Of)
	return shapeWord
}

// isCheck is whether an operation is a check: it gives no value and always has a failure block, so what it computes is
// whether it passes.
func isCheck(o op) bool {
	return o.Result == noResult && slices.ContainsFunc(o.Operands, func(x operand) bool { return x.Kind == kFail && x.Flag == "" })
}

// checkEval refuses a table whose operations do not all say what they compute, or say it inconsistently.
func checkEval() {
	dispatched := map[string]bool{}
	for _, o := range ops {
		switch o.Eval.Form {
		case irtable.EvalUnset:
			fail("%s does not say what it computes: Eval is noEval for an operation that is no function of its operand words", o.Name)
		case irtable.EvalNone:
		case irtable.EvalCall:
			if o.Eval.Func == "" {
				fail("%s computes by a function that has no name", o.Name)
			}
			if (o.Eval.Fits != "") != slices.ContainsFunc(o.Operands, func(x operand) bool { return x.Kind == kFail && x.Flag == "Checked" }) {
				fail("%s: it has a function that says whether its value fits if and only if it can be Checked", o.Name)
			}
			evalParamsOf(o)
		case irtable.EvalByCase:
			x := operandOf(o, o.Eval.Of)
			if x.Kind != kEnum {
				fail("%s computes by the cases of %s, which is no enum", o.Name, o.Eval.Of)
			}
			dispatched[x.Enum] = true
			evalParamsOf(o)
			checkCases(o, shapeOf(o))
		}
	}
	for _, e := range enums {
		if dispatched[e.Name] {
			continue
		}
		for _, c := range e.Cases {
			if c.Eval.Form != irtable.CaseNone {
				fail("%s.%s computes something, but no operation computes by the cases of %s", e.Name, c.Name, e.Name)
			}
		}
	}
}

// checkCases refuses a case that does not compute what its operation gives or reads what the operation does not have.
func checkCases(o op, want shape) {
	params := evalParamsOf(o)
	for _, c := range enumOf(operandOf(o, o.Eval.Of).Enum).Cases {
		name := o.Name + "." + c.Name
		ev := c.Eval
		if ev.Form == irtable.CaseNone {
			fail("%s computes nothing, and %s computes by its cases", name, o.Name)
		}
		var got shape
		switch ev.Form {
		case irtable.CaseWrap, irtable.CaseDivide, irtable.CaseBitwise, irtable.CaseShift, irtable.CaseHigh:
			got = shapeOutcome
			if !hasParam(params, "unsigned") || !hasParam(params, "checked") {
				fail("%s needs the flags Unsigned and Checked of its operation", name)
			}
		case irtable.CaseOrder, irtable.CaseRange:
			got = shapeFlag
		case irtable.CaseDouble:
			got = shapeWord
			if o.Eval.Over != irtable.OverDoubles {
				fail("%s is a function of doubles, which its operation does not compute over", name)
			}
		case irtable.CaseNone:
		}
		if got != want {
			fail("%s gives something other than the other cases of %s", name, o.Eval.Of)
		}
		if ev.Form == irtable.CaseWrap && (ev.Wrap == "" || ev.FitsUnsigned == "" || ev.FitsSigned == "") {
			fail("%s has no function for its wrap or for whether it fits", name)
		}
		if (ev.Form == irtable.CaseDouble || ev.Form == irtable.CaseHigh) && ev.Func == "" {
			fail("%s has no function", name)
		}
		if ev.Form == irtable.CaseOrder || ev.Form == irtable.CaseRange {
			if len(ev.Holds) == 0 {
				fail("%s holds for no order", name)
			}
			if o.Eval.Over == irtable.OverWords {
				if _, ok := comparisonOperator(ev.Holds); !ok {
					fail("%s holds for orders that no comparison operator says", name)
				}
			}
			if o.Eval.Over == irtable.OverDoubles && o.Eval.Func == "" {
				fail("%s compares doubles by no function", o.Name)
			}
		}
		if ev.Form == irtable.CaseRange && ev.Zero && ev.Unsigned {
			fail("%s is an unsigned comparison to 0, which a signed one says too", name)
		}
	}
}

// comparisonOperator is the Maxon operator that says a number is in one of the orders; an order that two integers cannot
// be in, unordered, is no part of it.
func comparisonOperator(holds []order) (string, bool) {
	has := func(o order) bool { return slices.Contains(holds, o) }
	less, equal, greater := has(irtable.OrdLess), has(irtable.OrdEqual), has(irtable.OrdGreater)
	switch {
	case equal && !less && !greater:
		return "==", true
	case less && greater && !equal:
		return "!=", true
	case less && !equal && !greater:
		return "<", true
	case less && equal && !greater:
		return "<=", true
	case greater && !less && !equal:
		return ">", true
	case greater && equal && !less:
		return ">=", true
	}
	return "", false
}

var floatOrderCase = map[order]string{
	irtable.OrdLess: "less", irtable.OrdEqual: "equal", irtable.OrdGreater: "greater", irtable.OrdUnordered: "unordered",
}

// doubleOrderTest is the Maxon test that the order of two doubles, the variable order, is one of holds.
func doubleOrderTest(holds []order) string {
	const orders = 4
	var tests []string
	if len(holds) == orders-1 {
		for o := range order(orders) {
			if !slices.Contains(holds, o) {
				return "order != ast.FloatOrder." + floatOrderCase[o]
			}
		}
	}
	for _, o := range holds {
		tests = append(tests, "order == ast.FloatOrder."+floatOrderCase[o])
	}
	return strings.Join(tests, " or ")
}

var bitKeyword = map[irtable.BitOp]string{irtable.BitAnd: "and", irtable.BitOr: "or", irtable.BitXor: "xor"}

func emitEval() string {
	w := &writer{}
	w.p("%s", header)
	w.p("// What each operation of the IR table computes, from the semantics of its row. The algorithms those name are in\n// evaluate.maxon.\n\n")
	for _, o := range evaluated() {
		emitEvaluator(w, o)
	}
	return strings.TrimRight(w.b.String(), "\n") + "\n"
}

func emitEvaluator(w *writer, o op) {
	params := evalParamsOf(o)
	sig := make([]string, len(params))
	for i, p := range params {
		sig[i] = p.name + " " + p.typ
	}
	sh := shapeOf(o)
	returns := "Word"
	switch sh {
	case shapeOutcome:
		sig = append(sig, "sets value Word")
		returns = maxonOutcome
	case shapeFlag:
		returns = "bool"
	case shapeWord:
	}
	name := maxonEvaluator(o)
	if o.Eval.Form == irtable.EvalCall {
		w.p("// %s is what the operation %s computes, by %s.\n", name, o.Name, o.Eval.Func)
	} else {
		w.p("// %s is what the operation %s computes: the case of its %s says how.\n", name, o.Name, o.Eval.Of)
	}
	w.p("export function %s(%s) returns %s\n", name, strings.Join(sig, ", "), returns)
	if o.Eval.Form == irtable.EvalCall {
		emitCallBody(w, o, params)
	} else {
		emitCaseBody(w, o, params)
	}
	w.p("end '%s'\n\n", name)
}

// callArgs are the arguments that pass the operation's words and flags to a function: the first plain, the others named.
func callArgs(params evalParams) string {
	var args []string
	for _, p := range params {
		if p.operand.Kind == kFail {
			continue
		}
		if len(args) == 0 {
			args = append(args, p.name)
		} else {
			args = append(args, p.name+": "+p.name)
		}
	}
	return strings.Join(args, ", ")
}

func emitCallBody(w *writer, o op, params evalParams) {
	args := callArgs(params)
	if o.Eval.Fits == "" {
		w.p("\treturn %s(%s)\n", o.Eval.Func, args)
		return
	}
	w.p("\tvalue = %s(%s)\n", o.Eval.Func, args)
	w.p("\tif checked and not %s(%s) 'failed'\n\t\treturn %s\n\tend 'failed'\n\treturn %s\n", o.Eval.Fits, args, outcomeFails, outcomeValue)
}

func emitCaseBody(w *writer, o op, params evalParams) {
	enumOp := operandOf(o, o.Eval.Of)
	e := enumOf(enumOp.Enum)
	sel := lowerFirst(enumOp.Name)
	switch shapeOf(o) {
	case shapeOutcome:
		for _, c := range e.Cases {
			label := lowerFirst(c.Name)
			w.p("\tif %s == %s.%s '%s'\n", sel, e.Name, maxonEnumCase(*e, c), label)
			emitOutcomeCase(w, c)
			w.p("\tend '%s'\n", label)
		}
		w.p("\tpanic(\"ir: no meaning for a %s\")\n", e.Name)
	case shapeFlag:
		emitFlagCases(w, o, *e, sel, params)
	case shapeWord:
		emitDoubleCases(w, o, *e, sel, params)
	}
}

// emitOutcomeCase writes what one case computes, which sets value before it returns.
func emitOutcomeCase(w *writer, c enumCase) {
	ev := c.Eval
	switch ev.Form {
	case irtable.CaseWrap:
		w.p("\t\tvalue = wordOfBits(%s(bitsOfWord(a), b: bitsOfWord(b)))\n", ev.Wrap)
		w.p("\t\tif checked 'checked'\n")
		w.p("\t\t\tlet fits = %s(a, b: b, result: value) if unsigned else %s(a, b: b, result: value)\n", ev.FitsUnsigned, ev.FitsSigned)
		w.p("\t\t\tif not fits 'failed'\n\t\t\t\treturn %s\n\t\t\tend 'failed'\n", outcomeFails)
		w.p("\t\tend 'checked'\n")
		w.p("\t\treturn %s\n", outcomeValue)
	case irtable.CaseDivide:
		emitDivide(w, ev.Remainder)
	case irtable.CaseBitwise:
		w.p("\t\tvalue = wordOfBits(bitsOfWord(a) %s bitsOfWord(b))\n\t\treturn %s\n", bitKeyword[ev.Bits], outcomeValue)
	case irtable.CaseShift:
		emitShift(w, ev.Left)
	case irtable.CaseHigh:
		w.p("\t\tvalue = %s(a, b: b, unsigned: unsigned)\n\t\treturn %s\n", ev.Func, outcomeValue)
	case irtable.CaseOrder, irtable.CaseDouble, irtable.CaseRange, irtable.CaseNone:
		fail("%s is not a case that gives an outcome", c.Name)
	}
}

// emitDivide writes a quotient or a remainder, truncated toward zero. A division by 0 and an unchecked i64.min / -1 have
// no meaning, and a checked one fails.
func emitDivide(w *writer, remainder bool) {
	operator, what := "/", "a signed division that overflows past its tests"
	if remainder {
		operator, what = "mod", "a signed remainder that fails past its tests"
	}
	w.p("\t\tvalue = 0\n")
	w.p("\t\tif b == 0 'zero'\n\t\t\treturn %s\n\t\tend 'zero'\n", outcomeUndefined)
	w.p("\t\tif unsigned 'unsigned'\n")
	w.p("\t\t\tvalue = wordOfBits(try bitsOfWord(a) %s bitsOfWord(b) otherwise panic(\"ir: a division by 0 past its test\"))\n", operator)
	w.p("\t\t\treturn %s\n\t\tend 'unsigned'\n", outcomeValue)
	w.p("\t\tif b == -1 'minusOne'\n")
	if remainder {
		w.p("\t\t\treturn %s\n", outcomeValue)
	} else {
		w.p("\t\t\tif a == minWord 'overflow'\n")
		w.p("\t\t\t\tif not checked 'unchecked'\n\t\t\t\t\treturn %s\n\t\t\t\tend 'unchecked'\n", outcomeUndefined)
		w.p("\t\t\t\treturn %s\n\t\t\tend 'overflow'\n", outcomeFails)
		w.p("\t\t\tvalue = 0 - a\n\t\t\treturn %s\n", outcomeValue)
	}
	w.p("\t\tend 'minusOne'\n")
	w.p("\t\tvalue = try a %s b otherwise panic(%q)\n\t\treturn %s\n", operator, "ir: "+what, outcomeValue)
}

// emitShift writes a shift by a count from 0 on. A negative count has no meaning; a checked shift left fails where the
// bits shifted out are not the ones shifted back in.
func emitShift(w *writer, left bool) {
	w.p("\t\tif b < 0 'negative'\n\t\t\tvalue = 0\n\t\t\treturn %s\n\t\tend 'negative'\n", outcomeUndefined)
	w.p("\t\tvalue = shiftWord(%t, unsigned: unsigned, x: a, count: b)\n", left)
	if left {
		w.p("\t\tif checked 'checked'\n")
		w.p("\t\t\tlet fits = a == 0 if b >= wordBits else shiftWord(false, unsigned: unsigned, x: value, count: b) == a\n")
		w.p("\t\t\tif not fits 'failed'\n\t\t\t\treturn %s\n\t\t\tend 'failed'\n", outcomeFails)
		w.p("\t\tend 'checked'\n")
	}
	w.p("\t\treturn %s\n", outcomeValue)
}

// emitFlagCases writes whether the operation holds, a match on its cases.
func emitFlagCases(w *writer, o op, e enum, sel string, params evalParams) {
	if o.Eval.Over == irtable.OverDoubles {
		w.p("\tlet order = %s(bitsOfWord(a), b: bitsOfWord(b))\n", o.Eval.Func)
		emitFlagMatch(w, e, sel, "", func(c enumCase) string { return doubleOrderTest(c.Eval.Holds) })
		return
	}
	if e.Cases[0].Eval.Form == irtable.CaseOrder {
		if !hasParam(params, "unsigned") {
			fail("%s compares integers with no flag that says whether they are unsigned", o.Name)
		}
		w.p("\tif unsigned 'unsigned'\n\t\tlet x = bitsOfWord(a)\n\t\tlet y = bitsOfWord(b)\n")
		emitFlagMatch(w, e, sel, "\t", func(c enumCase) string { return wordTest(c, "x", "y") })
		w.p("\tend 'unsigned'\n")
		emitFlagMatch(w, e, sel, "", func(c enumCase) string { return wordTest(c, "a", "b") })
		return
	}
	emitFlagMatch(w, e, sel, "", func(c enumCase) string { return rangeTest(c) })
}

func wordTest(c enumCase, left, right string) string {
	operator, _ := comparisonOperator(c.Eval.Holds)
	return left + " " + operator + " " + right
}

// rangeTest is whether a is in the range the case says, of its bound, or of 0.
func rangeTest(c enumCase) string {
	operator, _ := comparisonOperator(c.Eval.Holds)
	left, right := "a", "bound"
	if c.Eval.Zero {
		right = "0"
	}
	if c.Eval.Unsigned {
		left, right = "bitsOfWord(a)", "bitsOfWord(bound)"
	}
	return left + " " + operator + " " + right
}

// emitFlagMatch writes the match on the cases of an enum that gives a bool, as the statement `return`, one block in when
// indent says so. Its label is the selector's name, which no other match of the function has.
func emitFlagMatch(w *writer, e enum, sel, indent string, test func(enumCase) string) {
	w.p("%s\treturn match %s '%s'\n", indent, sel, sel)
	for _, c := range e.Cases {
		w.p("%s\t\t%s gives %s\n", indent, maxonEnumCase(e, c), test(c))
	}
	w.p("%s\tend '%s'\n", indent, sel)
}

// emitDoubleCases writes the doubles that an operation of doubles gives, as the word of their bits.
func emitDoubleCases(w *writer, o op, e enum, sel string, params evalParams) {
	w.p("\tlet x = bitsOfWord(a)\n")
	args := "x"
	if hasParam(params, "b") {
		w.p("\tlet y = bitsOfWord(b)\n")
		args = "x, b: y"
	}
	w.p("\tlet r = match %s 'op'\n", sel)
	for _, c := range e.Cases {
		w.p("\t\t%s gives %s(%s)\n", maxonEnumCase(e, c), c.Eval.Func, args)
	}
	w.p("\tend 'op'\n\treturn wordOfBits(r)\n")
}
