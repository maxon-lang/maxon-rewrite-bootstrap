package main

import (
	"fmt"
	"math/big"
	"strings"
)

// kind is how the build cache stores a value of a type.
type kind int

const (
	scalarKind kind = iota
	boolKind
	stringKind
	bytesKind
	arrayKind
	frozenKind
	recordKind
	unionKind
	enumKind
	intMapKind
	stringMapKind
)

// readMode is how a word read back becomes an integer of its type.
type readMode int

const (
	// wordRead is a type that holds every word, as it is read: Count and the 64-bit patterns.
	wordRead readMode = iota
	// signedRead is FullRangeInt, which a word is cast to by its bits.
	signedRead
	// checkedRead is a narrower unsigned range, which the word is cast to with a check.
	checkedRead
	// signedCheckedRead is a range with negative values, which the word is cast to by its bits and then checked.
	signedCheckedRead
)

// rtype is a type as the generator stores it, resolved through every alias to what it is made of.
type rtype struct {
	kind kind
	// module and name name a scalar's alias (or the built-in, with no module), a record, a union or an enum.
	module, name string
	// width is a scalar's or an enum's tag's bytes, and read how a scalar is read back.
	width int
	read  readMode
	elem  *rtype
	args  []*rtype
	d     *decl
}

// layers are the modules the generator reads, each after the modules it may name.
var layers = []string{"diag", "table", "ast", "ir", "opt", "check"}

func layerOf(module string) int {
	for i, m := range layers {
		if m == module {
			return i
		}
	}
	panic("cachegen: a module outside the layers: " + module)
}

// builtinScalars are the built-in integer types a field may have, with their widths and how they are read back.
var builtinScalars = map[string]rtype{
	"Count":        {kind: scalarKind, name: "Count", width: 8, read: wordRead},
	"FullRangeInt": {kind: scalarKind, name: "FullRangeInt", width: 8, read: signedRead},
	"Byte":         {kind: scalarKind, name: "Byte", width: 1, read: checkedRead},
}

// world holds the declarations of every module the generator reads.
type world struct {
	decls map[string]map[string]*decl
}

// resolve is the type that text names in module, where the type parameters of a generic stand for subst.
func (w *world) resolve(module, text string, subst map[string]*rtype, where string) (*rtype, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") && closes(text) {
		return w.resolve(module, text[1:len(text)-1], subst, where)
	}
	if rest, ok := strings.CutPrefix(text, "frozen with "); ok {
		e, err := w.resolve(module, rest, subst, where)
		if err != nil {
			return nil, err
		}
		return &rtype{kind: frozenKind, elem: e}, nil
	}
	if rest, ok := strings.CutPrefix(text, "Array with "); ok {
		e, err := w.resolve(module, rest, subst, where)
		if err != nil {
			return nil, err
		}
		if e.kind == scalarKind && e.module == "" && e.name == "Byte" {
			return &rtype{kind: bytesKind}, nil
		}
		return &rtype{kind: arrayKind, elem: e}, nil
	}
	if strings.HasPrefix(text, "int(") || strings.HasPrefix(text, "bits(") {
		return rangeType(text, where)
	}
	name, argText, generic := strings.Cut(text, " with ")
	var args []*rtype
	if generic {
		for _, a := range splitTop(argText) {
			at, err := w.resolve(module, a, subst, where)
			if err != nil {
				return nil, err
			}
			args = append(args, at)
		}
	}
	if !strings.Contains(name, ".") {
		if t, ok := subst[name]; ok && !generic {
			return t, nil
		}
		if d, ok := w.decls[module][name]; ok {
			return w.named(d, args, where)
		}
		switch name {
		case "bool", "Bool":
			return &rtype{kind: boolKind}, nil
		case "String":
			return &rtype{kind: stringKind}, nil
		case "ByteArray":
			return &rtype{kind: bytesKind}, nil
		}
		if s, ok := builtinScalars[name]; ok {
			t := s
			return &t, nil
		}
		return nil, fmt.Errorf("%s: the type %s, which the build cache does not know how to store", where, text)
	}
	mod, local, _ := strings.Cut(name, ".")
	ds, ok := w.decls[mod]
	if !ok {
		return nil, fmt.Errorf("%s: the type %s is of module %s, which the generator does not read", where, text, mod)
	}
	d, ok := ds[local]
	if !ok {
		return nil, fmt.Errorf("%s: module %s declares no type %s", where, mod, local)
	}
	return w.named(d, args, where)
}

// named is the type that the declaration d names with args.
func (w *world) named(d *decl, args []*rtype, where string) (*rtype, error) {
	if len(args) != len(d.params) {
		return nil, fmt.Errorf("%s: %s.%s takes %d type arguments, not %d", where, d.module, d.name, len(d.params), len(args))
	}
	switch d.kind {
	case aliasDecl:
		t, err := w.resolve(d.module, d.target, nil, where+" (typealias "+d.module+"."+d.name+")")
		if err != nil {
			return nil, err
		}
		if t.kind == scalarKind {
			named := *t
			named.module, named.name = d.module, d.name
			return &named, nil
		}
		return t, nil
	case recordDecl:
		if d.module == "table" && (d.name == "IntMap" || d.name == "StringMap") {
			k := intMapKind
			if d.name == "StringMap" {
				k = stringMapKind
			}
			return &rtype{kind: k, elem: args[0]}, nil
		}
		return &rtype{kind: recordKind, module: d.module, name: d.name, args: args, d: d}, nil
	case unionDecl:
		return &rtype{kind: unionKind, module: d.module, name: d.name, args: args, d: d}, nil
	case enumDecl:
		width := 1
		if len(d.cases) > 256 {
			width = 2
		}
		return &rtype{kind: enumKind, module: d.module, name: d.name, width: width, d: d}, nil
	}
	panic("cachegen: a declaration of no kind")
}

// closes is whether the parenthesis that opens text closes at its end.
func closes(text string) bool {
	depth := 0
	for i, c := range text {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i == len(text)-1
			}
		}
	}
	return false
}

// bound is the value of one end of a range as the source writes it.
func bound(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	one := big.NewInt(1)
	switch s {
	case "i64.min":
		return new(big.Int).Neg(new(big.Int).Lsh(one, 63)), nil
	case "i64.max":
		return new(big.Int).Sub(new(big.Int).Lsh(one, 63), one), nil
	case "u64.max":
		return new(big.Int).Sub(new(big.Int).Lsh(one, 64), one), nil
	case "i32.max":
		return big.NewInt(1<<31 - 1), nil
	case "u32.max":
		return big.NewInt(1<<32 - 1), nil
	}
	v, ok := new(big.Int).SetString(s, 0)
	if !ok {
		return nil, fmt.Errorf("the bound %q", s)
	}
	return v, nil
}

// rangeType is the scalar of an `int(lo to hi)` or a `bits(n)`.
func rangeType(text, where string) (*rtype, error) {
	if rest, ok := strings.CutPrefix(text, "bits("); ok {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSuffix(rest, ")"), "%d", &n); err != nil {
			return nil, fmt.Errorf("%s: the pattern %s", where, text)
		}
		read := checkedRead
		if n == 64 {
			read = wordRead
		}
		return &rtype{kind: scalarKind, width: widthOf(big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(n)), big.NewInt(1))), read: read}, nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(text, "int("), ")")
	lo, hi, ok := strings.Cut(inner, " to ")
	if !ok {
		return nil, fmt.Errorf("%s: the range %s", where, text)
	}
	l, err := bound(lo)
	if err != nil {
		return nil, fmt.Errorf("%s: %v in %s", where, err, text)
	}
	h, err := bound(hi)
	if err != nil {
		return nil, fmt.Errorf("%s: %v in %s", where, err, text)
	}
	t := &rtype{kind: scalarKind, width: widthOf(l, h), read: checkedRead}
	minI64 := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	maxU64 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	switch {
	case l.Sign() < 0 && l.Cmp(minI64) == 0 && h.Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))) == 0:
		t.read = signedRead
	case l.Sign() < 0:
		t.read = signedCheckedRead
	case l.Sign() == 0 && h.Cmp(maxU64) == 0:
		t.read = wordRead
	}
	return t, nil
}

// widthOf is how many bytes a value of the range lo to hi takes: a signed range takes a word, written by its bits.
func widthOf(lo, hi *big.Int) int {
	if lo.Sign() < 0 {
		return 8
	}
	for _, w := range []int{1, 2, 4} {
		if hi.Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(8*w)), big.NewInt(1))) <= 0 {
			return w
		}
	}
	return 8
}
