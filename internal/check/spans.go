package check

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// span is the values an integer can have where the code reaches: lo to hi, which may lie past the 64 bits of a word while
// an operation's exact result is being judged.
type span struct {
	lo, hi ast.I128
	// nonZero says that 0 is not among the values although it lies between lo and hi: what 'b != 0' proves of a b that
	// may be negative.
	nonZero bool
}

// The words an operation computes in: a signed word for a signed operand, an unsigned one for an operand of an unsigned
// alias.
var (
	signedWord   = span{lo: ast.I64Min, hi: ast.I64Max}
	unsignedWord = span{lo: ast.Zero, hi: ast.U64Max}
)

func exactly(v ast.I128) span { return span{lo: v, hi: v} }

func (s span) excludesZero() bool { return s.nonZero || s.lo.Gt(ast.Zero) || s.hi.Lt(ast.Zero) }

func (s span) has(v ast.I128) bool {
	return s.lo.Le(v) && s.hi.Ge(v) && !(v.IsZero() && s.nonZero)
}

func (s span) empty() bool { return s.lo.Gt(s.hi) }

func (s span) within(o span) bool { return s.lo.Ge(o.lo) && s.hi.Le(o.hi) }

func (s span) union(o span) span {
	return span{lo: ast.MinI(s.lo, o.lo), hi: ast.MaxI(s.hi, o.hi), nonZero: s.excludesZero() && o.excludesZero()}
}

func (s span) clamp(o span) span {
	return span{lo: ast.MaxI(s.lo, o.lo), hi: ast.MinI(s.hi, o.hi), nonZero: s.nonZero}
}

// typeSpan is the values of an integer type.
func typeSpan(t ast.Ty) span {
	switch x := t.(type) {
	case *ast.IntTy:
		return span{lo: x.Min, hi: x.Max}
	case *ast.ConstIntTy:
		return exactly(x.Value)
	}
	panic(diag.Internal("ranges: %s is not an integer", t.Name()))
}

func isIntTy(t ast.Ty) bool {
	switch t.(type) {
	case *ast.IntTy, *ast.ConstIntTy:
		return true
	}
	return false
}

// arithmetic is the span of the exact result of an operator on operands of spans l and r, and how it fails beyond the
// result's range: a divisor that may be 0, or a shift count that may be negative. word is the word it computes in.
func arithmetic(op string, l, r span, word span) (span, ast.Fails) {
	var fails ast.Fails
	switch op {
	case "+":
		return span{lo: l.lo.Add(r.lo), hi: l.hi.Add(r.hi)}, 0
	case "-":
		return span{lo: l.lo.Sub(r.hi), hi: l.hi.Sub(r.lo)}, 0
	case "*":
		return corners(l, r, ast.MulWide), 0
	case "/", "mod":
		if r.has(ast.Zero) {
			fails |= ast.FailZero
		}
		var out span
		found := false
		for _, part := range nonZeroParts(r) {
			var s span
			if op == "/" {
				s = corners(l, part, ast.QuoTrunc)
			} else {
				s = remainders(l, part)
			}
			if found {
				out = out.union(s)
			} else {
				out, found = s, true
			}
		}
		if !found {
			return span{lo: ast.One, hi: ast.Zero}, fails
		}
		return out, fails
	case "shl", "shr":
		if r.lo.IsNeg() {
			fails |= ast.FailShift
		}
		return shifted(op == "shl", l, r), fails
	case "and", "or", "xor":
		if l.lo.IsNeg() || r.lo.IsNeg() {
			return word, 0
		}
		if op == "and" {
			return span{lo: ast.Zero, hi: ast.MinI(l.hi, r.hi)}, 0
		}
		return span{lo: ast.Zero, hi: allOnesCovering(ast.MaxI(l.hi, r.hi))}, 0
	}
	panic(diag.Internal("ranges: unknown operator '%s'", op))
}

// corners is the span of f over two spans, for an f that is monotonic in each argument wherever neither changes sign:
// its extremes are at the corners.
func corners(l, r span, f func(a, b ast.I128) ast.I128) span {
	a, b, c, d := f(l.lo, r.lo), f(l.lo, r.hi), f(l.hi, r.lo), f(l.hi, r.hi)
	return span{lo: ast.MinI(ast.MinI(a, b), ast.MinI(c, d)), hi: ast.MaxI(ast.MaxI(a, b), ast.MaxI(c, d))}
}

// nonZeroParts splits a divisor's span into its negative and its positive values.
func nonZeroParts(s span) []span {
	var parts []span
	if s.lo.IsNeg() {
		parts = append(parts, span{lo: s.lo, hi: ast.MinI(s.hi, ast.I64(-1))})
	}
	if s.hi.Gt(ast.Zero) {
		parts = append(parts, span{lo: ast.MaxI(s.lo, ast.One), hi: s.hi})
	}
	return parts
}

// remainders is the span of l mod d for a d of one sign: the remainder has the dividend's sign and is smaller than the
// divisor.
func remainders(l, d span) span {
	m := ast.MaxI(absI(d.lo), absI(d.hi)).Sub(ast.One)
	out := span{lo: ast.Zero, hi: ast.Zero}
	if l.hi.Gt(ast.Zero) {
		out.hi = ast.MinI(l.hi, m)
	}
	if l.lo.IsNeg() {
		out.lo = ast.MaxI(l.lo, m.Neg())
	}
	return out
}

func absI(v ast.I128) ast.I128 {
	if v.IsNeg() {
		return v.Neg()
	}
	return v
}

// allOnesCovering is the smallest 2^k - 1 at or above v, for a v that is not negative: the largest value an 'or' or an
// 'xor' of values up to v gives.
func allOnesCovering(v ast.I128) ast.I128 {
	out := ast.Zero
	for out.Lt(v) {
		out = out.Add(out).Add(ast.One)
	}
	return out
}

// shifted is the span of the exact result of a shift by the counts that are not negative: a left shift multiplies by a
// power of two, which past the word saturates as a product does, and a right shift moves toward 0.
func shifted(left bool, l, count span) span {
	if !left {
		return span{lo: ast.MinI(l.lo, ast.Zero), hi: ast.MaxI(l.hi, ast.Zero)}
	}
	lo, hi := ast.MaxI(count.lo, ast.Zero), ast.MinI(count.hi, ast.I64(maxExactShift))
	if hi.Lt(lo) {
		lo = hi
	}
	exact := corners(l, span{lo: pow2(lo), hi: pow2(hi)}, ast.MulWide)
	if count.hi.Gt(ast.I64(maxExactShift)) {
		// A count past the word moves any bit that is not 0 out of it.
		if l.hi.Gt(ast.Zero) {
			exact.hi = ast.MaxI(exact.hi, saturated)
		}
		if l.lo.IsNeg() {
			exact.lo = ast.MinI(exact.lo, saturated.Neg())
		}
	}
	return exact
}

// maxExactShift is the largest count whose power of two a word's magnitude holds, which MulWide can multiply by.
const maxExactShift = 63

// saturated stands for a value past every word, as MulWide saturates.
var saturated = ast.MulWide(ast.U64Max, ast.U64Max)

func pow2(n ast.I128) ast.I128 {
	out := ast.One
	for i := int64(0); i < n.Bits(); i++ {
		out = out.Add(out)
	}
	return out
}
