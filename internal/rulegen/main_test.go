package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"minimaxon/internal/irgen/irtable"
	"minimaxon/internal/x64gen/x64table"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	outs, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what the rules generate; run `go generate ./internal/rulegen`", o.path)
		}
	}
}

// Each of these is refused at its rule.
var refused = []struct{ rules, err string }{
	{"(bin.add x (const [0])) x", "a rule is a pattern, => and what it is rewritten to"},
	{"(binary.add x y) => x", "there is no IR operation binary"},
	{"(bin.plus x y) => x", "BinOp has no case plus"},
	{"(bin.add.sub x y) => x", "bin names two cases of BinOp"},
	{"(bin.add.!wide x y) => x", "bin has no flag wide"},
	{"(bin.add.unsigned.!unsigned x y) => x", "names the flag unsigned twice"},
	{"(bin.add x y [f] z) => x", "bin has 3 operands after its case and flags (left right fail), and the rule gives it 4"},
	{"(bin.add x [y]) => x", "bin's operand Right is written a register's term"},
	{"(bin.add x (const y)) => x", "const's operand Value is written [...]"},
	{"(bin.add x y [f]) => x", "only a bin with .checked has the block fail"},
	{"(bin.add x (storeSlot [_] y)) => x", "storeSlot gives no register for an operand to be"},
	{"(bin.add x y) => z", "z is no variable of the pattern"},
	{"(bin.add x (const [c])) && c == d => x", "d is no variable of the pattern"},
	{"(bin.add x (const [c])) => (const [c + k])", "k is no variable of the pattern"},
	{"(bin.add x (const [c])) => c", "c is no register"},
	{"(bin.add x y) => (bin.shl x)", "bin is given no right"},
	{"(bin.add x y) => (bin x y)", "bin names no case of BinOp"},
	{"(bin.add x y) => (bin.shl.!unsigned x y)", "a result leaves out the flags that are clear"},
	{"(bin.add x y) => (bin.shl.checked x y)", "a result gives bin its operand fail, which says it is .checked, rather than the flag"},
	{"(bin.add x y) => (bin.shl x _)", "a result has no _"},
	{"(bin.add x y) => (storeSlot [0] x)", "bin and storeSlot do not both give a register"},
	{"(bin.add x y) => (call [0] [_])", "a result cannot build call, whose operand Args is a list"},
	{"(bin.add x y) => (bin.sub h:(bin.mul x y) x)", "h: binds a register in a result that nothing after it reads"},
	{"(bin.add x y) => (bin.sub h (bin.add h:(bin.mul x y) y))", "h is no variable of the pattern, nor a register the result binds before it"},
	{"(jump [b]) => x", "a generic rule rewrites an instruction, and jump is a terminator"},
	{"(bin.add X y) => y", "X is no variable"},
	{"(bin.add x y) => x\n(bin.add x (const [0])) => x", "can never fire: the rule at line 1, (bin.add x y) => x, matches everything it matches"},
	{"(bin x y) => x\n(bin.mul.checked x y [f]) => y", "can never fire: the rule at line 1"},
	{"(bin.add x (const [0])) => x\n(bin.add (const [0]) y) => y", "can never fire: the rule at line 1"},
}

func TestRulesAreChecked(t *testing.T) {
	for _, c := range refused {
		_, err := generateGeneric("test.rules", c.rules)
		if err == nil {
			t.Errorf("%q is not refused", c.rules)
			continue
		}
		if !strings.Contains(err.Error(), c.err) {
			t.Errorf("%q is refused with %q, which does not say %q", c.rules, err, c.err)
		}
	}
}

// These may all fire: a condition, a literal or a variable named twice narrows what an earlier rule matches.
func TestRulesThatFire(t *testing.T) {
	rules := strings.Join([]string{
		"(bin.add x (const [c])) && c == 0 => x",
		"(bin.add x x) => x",
		"(bin.add x (const [1])) => x",
		"(bin.add.checked x (const [2]) [f]) => (neg x [f])",
		"d:(bin.or x (const [0])) => d",
		"(bin.add x y)",
		"  => y",
	}, "\n")
	if _, err := generateGeneric("test.rules", rules); err != nil {
		t.Fatal(err)
	}
}

// selectionRules are instruction selection rules: some that select by the operands, and then one per IR operation that
// matches all of it (the rule of loadField already does).
func selectionRules(leaveOut string) string {
	lines := []string{
		"(bin.add x (const [c])) && fitsImm32(c) => (AddRegImm x [c])",
		"(branch (cmp.lt.unsigned x y) [yes] [no]) => (CmpRegReg x y) (JccRel32 [MCond.below] [yes]) (JmpRel32 [no])",
		"d:(loadField obj [off]) => (LoadRegBaseDisp d obj [off])",
		"(loadRaw.8 a) => (MovzxRegByteBaseDisp r a [0])",
		"(loadRaw a) => (LoadRegBaseDisp r a [0])",
	}
	for _, o := range irtable.Ops {
		if o.Name != leaveOut && o.Name != "LoadField" && o.Name != "LoadRaw" {
			lines = append(lines, "("+lowerFirst(o.Name)+") => (helper)")
		}
	}
	return strings.Join(lines, "\n")
}

func TestMachineRulesAreChecked(t *testing.T) {
	if _, err := generateMachine("x64.rules", selectionRules(""), x64table.Table); err != nil {
		t.Fatal(err)
	}
	refusedMachine := []struct{ rules, err string }{
		{selectionRules("Bin"), "x64.rules: no rule matches every bin"},
		{"(bin.add x y) => (AddRegRegReg x y)\n" + selectionRules(""), "there is no x64 form or choice AddRegRegReg"},
		{"(bin.add x y) => (AddRegReg x)\n" + selectionRules(""), "AddRegReg takes 2 operands (dest src), and the rule gives it 1"},
		{"(bin.add x y) => (AddRegReg x z)\n" + selectionRules(""), "AddRegReg's src reads z, which nothing defines before it"},
		{"(bin.add x y) => (AddRegReg.wide x y)\n" + selectionRules(""), "an x64 form has no binding and no suffixes"},
		{"(cmp x y) => (CmpRegReg x y) (SetccRax [MCond.equal])\n" + selectionRules(""), "there is no x64 form or choice SetccRax"},
		{"(bin x y) => (MovRegReg x y)\n" + selectionRules(""), "MovRegReg's dest defines x, a register of the pattern"},
		{"d:(bin x y) => (AddRegReg x y)\n" + selectionRules(""), "no form defines d, the result"},
		{"(bin x y) => (CmpRegReg x y)\n" + selectionRules(""), "bin gives a register, and no form defines one"},
		{"(bin rax y) => (AddRegReg rax y)\n" + selectionRules(""), "rax is a register or a name of the selector's"},
		{"(bin x y) => (helper) (Ud2)\n" + selectionRules(""), "a helper is the only result of its rule"},
		{strings.Replace(selectionRules(""), "(loadRaw a)", "(loadRaw.16 a)", 1), "no rule matches every loadRaw.32"},
	}
	for _, c := range refusedMachine {
		_, err := generateMachine("x64.rules", c.rules, x64table.Table)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%.60q is refused with %v, which does not say %q", c.rules, err, c.err)
		}
	}
}
