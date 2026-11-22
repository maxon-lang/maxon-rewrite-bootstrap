// Command rulegen generates, from the rewrite rules in generic.rules and pressure.rules, the matcher of self/opt that
// rewrites an IR instruction by the first rule that matches it and the instructions that define its operands (the rules
// of pressure.rules only where the caller says they may raise register pressure), and from the rules of each
// instruction set (x64.rules) its instruction selection in self/backend (lower_x64_gen.maxon), which writes the machine
// instructions of each IR instruction. `go generate ./internal/rulegen` runs it. README "Generated code", "The rewrite
// rules", describes the rule language.
//
// The operations and operands a rule names are checked against the IR table (internal/irgen/irtable), and those of an
// instruction selection rule against its instruction set (internal/isa, which internal/machines lists), so a rule that
// names what the tables do not have fails here, at the rule, rather than in the Maxon compile of what it generates.
package main

//go:generate go run .

import (
	"fmt"
	"os"

	"minimaxon/internal/isa"
	"minimaxon/internal/machines"
)

const (
	genericRules  = "generic.rules"
	pressureRules = "pressure.rules"
	genericFile   = "../../self/opt/rewrite_gen.maxon"
)

// machineRules is the file of an instruction set's selection rules, and machineFile the selection it generates.
func machineRules(m isa.ISA) string { return m.Name() + ".rules" }

func machineFile(m isa.ISA) string { return "../../self/backend/lower_" + m.Name() + "_gen.maxon" }

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
	outs := []output{{genericFile, []byte(code)}}
	for _, m := range machines.All {
		src, err := os.ReadFile(machineRules(m))
		if err != nil {
			return nil, err
		}
		machine, err := generateMachine(machineRules(m), string(src), m)
		if err != nil {
			return nil, err
		}
		outs = append(outs, output{machineFile(m), []byte(machine)})
	}
	return outs, nil
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

// generateMachine is the Maxon of instruction selection from a file of rules for an instruction set.
func generateMachine(file, src string, m isa.ISA) (code string, err error) {
	defer recoverRuleError(&err)
	tg := machineTarget(m)
	return emitMachine(file, checkRules(file, parseRules(file, src), tg), tg), nil
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
