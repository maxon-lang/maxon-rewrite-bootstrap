// Command rulegen generates, from the rewrite rules in generic.rules, the matcher of self/opt that rewrites an IR
// instruction by the first rule that matches it and the instructions that define its operands, and from x64.rules the
// instruction selection of self/backend, which writes the machine instructions of each IR instruction. `go generate
// ./internal/rulegen` runs it. README "Generated code", "The rewrite rules", describes the rule language.
//
// The operations and operands a rule names are checked against the IR table (internal/irgen/irtable), and those of an
// instruction selection rule against the x64 instruction table (internal/x64gen/x64table), so a rule that names what
// the tables do not have fails here, at the rule, rather than in the Maxon compile of what it generates.
package main

//go:generate go run .

import (
	"fmt"
	"os"
)

const (
	genericRules = "generic.rules"
	genericFile  = "../../self/opt/rewrite_gen.maxon"
	x64Rules     = "x64.rules"
	machineFile  = "../../self/backend/lower_x64_gen.maxon"
)

type output struct {
	path string
	code []byte
}

func main() {
	outs, err := generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rulegen:", err)
		os.Exit(1)
	}
	for _, o := range outs {
		if err := os.WriteFile(o.path, o.code, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "rulegen:", err)
			os.Exit(1)
		}
	}
}

func generate() ([]output, error) {
	src, err := os.ReadFile(genericRules)
	if err != nil {
		return nil, err
	}
	code, err := generateGeneric(genericRules, string(src))
	if err != nil {
		return nil, err
	}
	src, err = os.ReadFile(x64Rules)
	if err != nil {
		return nil, err
	}
	machine, err := generateMachine(x64Rules, string(src))
	if err != nil {
		return nil, err
	}
	return []output{{genericFile, []byte(code)}, {machineFile, []byte(machine)}}, nil
}

// generateGeneric is the Maxon of the matcher of a file of generic rules.
func generateGeneric(file, src string) (code string, err error) {
	defer recoverRuleError(&err)
	return emitGeneric(file, checkRules(file, parseRules(file, src), genericTarget)), nil
}

// generateMachine is the Maxon of instruction selection from a file of rules for x64.
func generateMachine(file, src string) (code string, err error) {
	defer recoverRuleError(&err)
	return emitMachine(file, checkRules(file, parseRules(file, src), x64Target)), nil
}

// ruleError is a rule that the generator cannot make code of.
type ruleError struct{ msg string }

func (e *ruleError) Error() string { return e.msg }

func recoverRuleError(err *error) {
	if r := recover(); r != nil {
		re, ok := r.(*ruleError)
		if !ok {
			panic(r)
		}
		*err = re
	}
}

// failAt refuses the rule of a file at a line.
func failAt(file string, line int, text, format string, args ...any) {
	panic(&ruleError{fmt.Sprintf("%s:%d: %s: %s", file, line, text, fmt.Sprintf(format, args...))})
}

func (r *rule) fail(format string, args ...any) { failAt(r.file, r.line, r.text, format, args...) }
