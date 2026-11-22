// Command rulegen generates, from the rewrite rules in generic.rules and pressure.rules, the matcher of self/opt that
// rewrites an IR instruction by the first rule that matches it and the instructions that define its operands (the rules
// of pressure.rules only where the caller says they may raise register pressure), and from x64.rules the
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
	genericRules  = "generic.rules"
	pressureRules = "pressure.rules"
	genericFile   = "../../self/opt/rewrite_gen.maxon"
	x64Rules      = "x64.rules"
	machineFile   = "../../self/backend/lower_x64_gen.maxon"
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
	var sources []ruleSource
	for _, s := range []struct {
		file   string
		raises bool
	}{{genericRules, false}, {pressureRules, true}} {
		src, err := os.ReadFile(s.file)
		if err != nil {
			return nil, err
		}
		sources = append(sources, ruleSource{file: s.file, src: string(src), raises: s.raises})
	}
	code, err := generateRuleSets(sources)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(x64Rules)
	if err != nil {
		return nil, err
	}
	machine, err := generateMachine(x64Rules, string(src))
	if err != nil {
		return nil, err
	}
	return []output{{genericFile, []byte(code)}, {machineFile, []byte(machine)}}, nil
}

// ruleSource is a file of generic rules: its name, its text, and whether its rules may raise register pressure, which
// the matcher applies only where its caller allows them.
type ruleSource struct {
	file   string
	src    string
	raises bool
}

// generateGeneric is the Maxon of the matcher of one file of generic rules that never raise register pressure.
func generateGeneric(file, src string) (string, error) {
	return generateRuleSets([]ruleSource{{file: file, src: src}})
}

// generateRuleSets is the Maxon of the matcher of the files of generic rules, whose rules of one operation it tries in
// the order of the files.
func generateRuleSets(sources []ruleSource) (code string, err error) {
	defer recoverRuleError(&err)
	var sets []ruleSet
	for _, s := range sources {
		sets = append(sets, ruleSet{file: s.file, raises: s.raises, rules: checkRules(s.file, parseRules(s.file, s.src), genericTarget)})
	}
	return emitGeneric(sets), nil
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
