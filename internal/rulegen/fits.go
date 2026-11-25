package main

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"minimaxon/internal/irgen/irtable"
)

// The Maxon that a rule's expressions call is in self/backend: constants, functions, and the types of the fields they
// become. A conversion of an expression to a field's type is a try only where the expression can be out of the type's range,
// since the compiler refuses a try that covers nothing that can fail (README, "Checked arithmetic"); rulegen knows the
// types of what it can see and no more, so an expression it cannot place keeps its try.

// supportDir is where the Maxon of the backend lives, from which the declarations that rules name are read.
const supportDir = "../../self/backend"

// generatedPrefix starts the name of the files rulegen itself writes, which are no source of types.
const generatedPrefix = "lower_"

// intRange is the values of an integer type.
type intRange struct{ lo, hi *big.Int }

// within is whether every value of r is a value of o.
func (r intRange) within(o intRange) bool { return r.lo.Cmp(o.lo) >= 0 && r.hi.Cmp(o.hi) <= 0 }

func exactly(v *big.Int) intRange { return intRange{v, v} }

// support is what the backend's source declares about the names that rules use.
type support struct {
	types     map[string]intRange
	constants map[string]intRange
	functions map[string]string
}

var (
	i64Range   = intRange{big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)}
	countRange = intRange{big.NewInt(0), new(big.Int).SetUint64(math.MaxUint64)}

	typealiasLine = regexp.MustCompile(`^(?:export )?typealias (\w+) = int\((-?\d+) to (-?\d+)\)`)
	constantLine  = regexp.MustCompile(`^(?:export )?let (\w+) = (-?(?:0x[0-9a-fA-F_]+|\d+))(?: as ([\w.]+))?$`)
	functionLine  = regexp.MustCompile(`^(?:export )?function (\w+)\(.*\) returns ([\w.]+)`)
	wholeName     = regexp.MustCompile(`^\w+$`)
	wholeCall     = regexp.MustCompile(`^(\w+)\(.*\)$`)

	loadSupport = sync.OnceValue(readSupport)
)

// readSupport reads the typealiases, the constants of a literal and the return types of the functions of the backend's
// hand-written files.
func readSupport() support {
	s := support{types: map[string]intRange{"FullRangeInt": i64Range, "Word": i64Range, "Count": countRange}, constants: map[string]intRange{}, functions: map[string]string{}}
	files, err := filepath.Glob(filepath.Join(supportDir, "*.maxon"))
	if err != nil {
		panic(fmt.Sprintf("the backend's source: %v", err))
	}
	var lines []string
	for _, file := range files {
		if strings.HasPrefix(filepath.Base(file), generatedPrefix) {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			panic(fmt.Sprintf("the backend's source: %v", err))
		}
		lines = append(lines, strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")...)
	}
	// A constant names its type, which may be declared in a file that comes after its own.
	for _, line := range lines {
		s.readType(line)
	}
	for _, line := range lines {
		s.read(line)
	}
	return s
}

// readType takes in one line of source that declares a type.
func (s *support) readType(line string) {
	if m := typealiasLine.FindStringSubmatch(line); m != nil {
		s.types[m[1]] = intRange{parseInt(m[2]), parseInt(m[3])}
	}
}

// read takes in one line of source that declares a function or a constant.
func (s *support) read(line string) {
	if m := functionLine.FindStringSubmatch(line); m != nil {
		s.functions[m[1]] = m[2]
	} else if m := constantLine.FindStringSubmatch(line); m != nil {
		v := exactly(parseInt(m[2]))
		if m[3] != "" {
			t, ok := s.types[strings.TrimPrefix(m[3], "ir.")]
			if !ok {
				return
			}
			v = t
		}
		s.constants[m[1]] = v
	}
}

func parseInt(text string) *big.Int {
	v, ok := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 0)
	if !ok {
		panic(fmt.Sprintf("%q is no integer", text))
	}
	return v
}

// fitsType is whether the expression a rule writes is always a value of the type ty: a constant inside it, a call of a
// function that returns a type inside it, or the constant operand of the pattern, a word, where ty holds every word.
func fitsType(text, ty string, vars map[string]*binding) bool {
	s := loadSupport()
	target, ok := s.types[ty]
	if !ok {
		return false
	}
	text = strings.TrimSpace(text)
	for strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") && balanced(text[1:len(text)-1]) {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	if wholeName.MatchString(text) {
		if b := vars[text]; b != nil {
			return !b.reg && b.operand.Kind == irtable.KWord && i64Range.within(target)
		}
		c, known := s.constants[text]
		return known && c.within(target)
	}
	if m := wholeCall.FindStringSubmatch(text); m != nil && balanced(text[len(m[1]):]) {
		returned, known := s.functions[m[1]]
		if !known {
			return false
		}
		r, known := s.types[strings.TrimPrefix(returned, "ir.")]
		return known && r.within(target)
	}
	return false
}

// balanced is whether every parenthesis of text closes and none closes before it opens.
func balanced(text string) bool {
	depth := 0
	for _, c := range text {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}
