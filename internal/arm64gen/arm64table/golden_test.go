package arm64table

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// captureVariable names the variable that asks TestCaptureGolden to write the golden file: the Go toolchain's arm64
// assembler assembles every golden case, and the test writes the words it gave. No other run needs the toolchain.
const captureVariable = "ARM64_CAPTURE_GOLDEN"

// goSyntax is each form in the Go assembler's syntax, whose operands are the form's parameters by name: a register is
// R<n>, RSP or ZR, or F<n> (V<n> with :v), an immediate $<n>, and a condition its name. The Go assembler writes the
// destination last.
var goSyntax = map[string]string{
	"AddRegReg": "ADD {right}, {left}, {dest}", "SubRegReg": "SUB {right}, {left}, {dest}",
	"AddsRegReg": "ADDS {right}, {left}, {dest}", "SubsRegReg": "SUBS {right}, {left}, {dest}",
	"CmpRegReg": "CMP {right}, {left}", "NegReg": "NEG {src}, {dest}",
	"AndRegReg": "AND {right}, {left}, {dest}", "OrrRegReg": "ORR {right}, {left}, {dest}",
	"EorRegReg": "EOR {right}, {left}, {dest}", "AndsRegReg": "ANDS {right}, {left}, {dest}",
	"TstRegReg": "TST {right}, {left}", "MovRegReg": "MOVD {src}, {dest}", "MvnRegReg": "MVN {src}, {dest}",
	"MulRegReg": "MUL {right}, {left}, {dest}", "SmulhRegReg": "SMULH {right}, {left}, {dest}",
	"UmulhRegReg": "UMULH {right}, {left}, {dest}", "SdivRegReg": "SDIV {right}, {left}, {dest}",
	"UdivRegReg": "UDIV {right}, {left}, {dest}", "LslvRegReg": "LSL {right}, {left}, {dest}",
	"LsrvRegReg": "LSR {right}, {left}, {dest}", "AsrvRegReg": "ASR {right}, {left}, {dest}",
	"RorvRegReg": "ROR {right}, {left}, {dest}", "RbitRegReg": "RBIT {src}, {dest}", "ClzRegReg": "CLZ {src}, {dest}",
	"MaddRegRegRegReg": "MADD {right}, {addend}, {left}, {dest}", "MsubRegRegRegReg": "MSUB {right}, {minuend}, {left}, {dest}",
	"AddRegImm": "ADD {imm}, {src}, {dest}", "AddRegImmHigh": "ADD {imm:high}, {src}, {dest}",
	"SubRegImm": "SUB {imm}, {src}, {dest}", "SubRegImmHigh": "SUB {imm:high}, {src}, {dest}",
	"AddsRegImm": "ADDS {imm}, {src}, {dest}", "SubsRegImm": "SUBS {imm}, {src}, {dest}",
	"CmpRegImm": "CMP {imm}, {left}", "CmnRegImm": "CMN {imm}, {left}",
	"AndRegMask": "AND {imm}, {src}, {dest}", "OrrRegMask": "ORR {imm}, {src}, {dest}",
	"EorRegMask": "EOR {imm}, {src}, {dest}", "AndsRegMask": "ANDS {imm}, {src}, {dest}",
	"TstRegMask": "TST {imm}, {left}", "MovRegMask": "ORR {imm}, ZR, {dest}",
	"MovzRegImm": "MOVZ {imm:wide}, {dest}", "MovnRegImm": "MOVN {imm:wide}, {dest}", "MovkRegImm": "MOVK {imm:wide}, {dest}",
	"UbfmRegReg": "UBFM {immr}, {src}, {imms}, {dest}", "SbfmRegReg": "SBFM {immr}, {src}, {imms}, {dest}",
	"CselRegReg": "CSEL {cond}, {left}, {right}, {dest}", "CsincRegReg": "CSINC {cond}, {left}, {right}, {dest}",
	"CsetReg":  "CSET {cond}, {dest}",
	"Ldr64Off": "MOVD {disp}({base}), {dest}", "Str64Off": "MOVD {src}, {disp}({base})",
	"Ldr64Unscaled": "MOVD {disp}({base}), {dest}", "Str64Unscaled": "MOVD {src}, {disp}({base})",
	"Ldr64Idx": "MOVD ({base})({index}<<3), {dest}", "Str64Idx": "MOVD {src}, ({base})({index}<<3)",
	"Ldr32Off": "MOVWU {disp}({base}), {dest}", "Str32Off": "MOVW {src}, {disp}({base})",
	"Ldr32Unscaled": "MOVWU {disp}({base}), {dest}", "Str32Unscaled": "MOVW {src}, {disp}({base})",
	"Ldr32Idx": "MOVWU ({base})({index}<<2), {dest}", "Str32Idx": "MOVW {src}, ({base})({index}<<2)",
	"LdrhOff": "MOVHU {disp}({base}), {dest}", "StrhOff": "MOVH {src}, {disp}({base})",
	"LdrhUnscaled": "MOVHU {disp}({base}), {dest}", "StrhUnscaled": "MOVH {src}, {disp}({base})",
	"LdrhIdx": "MOVHU ({base})({index}<<1), {dest}", "StrhIdx": "MOVH {src}, ({base})({index}<<1)",
	"LdrbOff": "MOVBU {disp}({base}), {dest}", "StrbOff": "MOVB {src}, {disp}({base})",
	"LdrbUnscaled": "MOVBU {disp}({base}), {dest}", "StrbUnscaled": "MOVB {src}, {disp}({base})",
	"LdrbIdx": "MOVBU ({base})({index}), {dest}", "StrbIdx": "MOVB {src}, ({base})({index})",
	"LdrDOff": "FMOVD {disp}({base}), {dest}", "StrDOff": "FMOVD {src}, {disp}({base})",
	"LdrDUnscaled": "FMOVD {disp}({base}), {dest}", "StrDUnscaled": "FMOVD {src}, {disp}({base})",
	"LdrDIdx": "FMOVD ({base})({index}<<3), {dest}", "StrDIdx": "FMOVD {src}, ({base})({index}<<3)",
	"LdrswOff": "MOVW {disp}({base}), {dest}", "LdrshOff": "MOVH {disp}({base}), {dest}", "LdrsbOff": "MOVB {disp}({base}), {dest}",
	"Ldr64Idx1": "MOVD ({base})({index}), {dest}", "Str64Idx1": "MOVD {src}, ({base})({index})",
	"Stp64": "STP ({first}, {second}), {disp}({base})", "Stp64Pre": "STP.W ({first}, {second}), {disp}({base})",
	"Stp64Post": "STP.P ({first}, {second}), {disp}({base})", "Ldp64": "LDP {disp}({base}), ({first}, {second})",
	"Ldp64Pre": "LDP.W {disp}({base}), ({first}, {second})", "Ldp64Post": "LDP.P {disp}({base}), ({first}, {second})",
	"StpD": "FSTPD ({first}, {second}), {disp}({base})", "StpDPre": "FSTPD.W ({first}, {second}), {disp}({base})",
	"StpDPost": "FSTPD.P ({first}, {second}), {disp}({base})", "LdpD": "FLDPD {disp}({base}), ({first}, {second})",
	"LdpDPre": "FLDPD.W {disp}({base}), ({first}, {second})", "LdpDPost": "FLDPD.P {disp}({base}), ({first}, {second})",
	"AddPageOff": "ADD {offset}, {src}, {dest}", "Ldr64PageOff": "MOVD ({base}), {dest}",
	"Str64PageOff": "MOVD {src}, ({base})", "LdrDPageOff": "FMOVD ({base}), {dest}",
	"B": "B {rel}", "Bl": "BL {rel}", "BCond": "B{cond} {rel}", "Cbz": "CBZ {src}, {rel}", "Cbnz": "CBNZ {src}, {rel}",
	"Br": "B ({target})", "Blr": "BL ({target})", "Ret": "RET",
	"Fadd": "FADDD {right}, {left}, {dest}", "Fsub": "FSUBD {right}, {left}, {dest}",
	"Fmul": "FMULD {right}, {left}, {dest}", "Fdiv": "FDIVD {right}, {left}, {dest}",
	"Fsqrt": "FSQRTD {src}, {dest}", "Fneg": "FNEGD {src}, {dest}", "Fabs": "FABSD {src}, {dest}",
	"FmovDD": "FMOVD {src}, {dest}", "Frintn": "FRINTND {src}, {dest}", "Frintm": "FRINTMD {src}, {dest}",
	"Frintp": "FRINTPD {src}, {dest}", "Frintz": "FRINTZD {src}, {dest}",
	"Fcmp": "FCMPD {right}, {left}", "FcmpZero": "FCMPD $(0.0), {left}",
	"Fcsel":  "FCSELD {cond}, {left}, {right}, {dest}",
	"Fcvtzs": "FCVTZSD {src}, {dest}", "Scvtf": "SCVTFD {src}, {dest}", "Ucvtf": "UCVTFD {src}, {dest}",
	"FmovDX": "FMOVD {src}, {dest}", "FmovXD": "FMOVD {src}, {dest}",
	"EorVec": "VEOR {right:v}.B8, {left:v}.B8, {dest:v}.B8",
	"Ldar":   "LDAR ({base}), {dest}", "Stlr": "STLR {src}, ({base})", "Ldaxr": "LDAXR ({base}), {dest}",
	"Stlxr": "STLXR {src}, ({base}), {status}", "CasalX0": "CASALD R0, ({base}), {src}",
	"Ldaddal": "LDADDALD {src}, ({base}), {dest}", "Swpal": "SWPALD {src}, ({base}), {dest}",
	"DmbIsh": "DMB $0xb", "Isb": "ISB $15", "Yield": "YIELD", "Nop": "NOOP",
	"Brk": "BRK {imm}", "Svc": "SVC $0",
	"MrsTpidr": "MRS TPIDR_EL0, {dest}", "MrsTpidrro": "MRS TPIDRRO_EL0, {dest}", "MsrTpidr": "MSR {src}, TPIDR_EL0",
	"MrsNzcv": "MRS NZCV, {dest}", "MsrNzcv": "MSR {src}, NZCV",
}

// goConditions are the Go assembler's names of the condition codes, by code.
var goConditions = []string{"EQ", "NE", "HS", "LO", "MI", "PL", "VS", "VC", "HI", "LS", "GE", "LT", "GT", "LE"}

var placeholder = regexp.MustCompile(`\{(\w+)(?::(\w+))?\}`)

// goText is a golden case in the Go assembler's syntax; a branch's distance is the label it goes back to.
func goText(c goldenCase) string {
	tmpl, ok := goSyntax[c.form.name]
	if !ok {
		panic("no Go syntax for " + c.form.name)
	}
	return placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		parts := placeholder.FindStringSubmatch(m)
		at := -1
		for i, o := range c.form.operands {
			if o.name == parts[1] {
				at = i
			}
		}
		if at < 0 {
			panic(c.form.name + " has no operand " + parts[1])
		}
		o, v := c.form.operands[at], c.vals[at]
		switch o.kind {
		case kindReg:
			switch {
			case parts[2] == "v":
				return fmt.Sprintf("V%d", v)
			case o.vector:
				return fmt.Sprintf("F%d", v)
			case v == zeroRegister && o.stack:
				return "RSP"
			case v == zeroRegister:
				return "ZR"
			}
			return fmt.Sprintf("R%d", v)
		case kindImm:
			switch parts[2] {
			case "high":
				return fmt.Sprintf("$%d", v<<imm12Bits)
			case "wide":
				return fmt.Sprintf("$(%d<<%d)", v, c.vals[at+1]*imm16Bits)
			}
			return fmt.Sprintf("$%d", v)
		case kindMask, kindPageOffset:
			return fmt.Sprintf("$%d", v)
		case kindDisp:
			return strconv.FormatInt(v, 10)
		case kindCond:
			return goConditions[conditions[v].code]
		case kindRel:
			return "back"
		default:
			panic(fmt.Sprintf("%s: no Go syntax for operand kind %d", c.form.name, o.kind))
		}
	})
}

// padOf is the words a branch case goes back over to its label.
func padOf(c goldenCase) int {
	if !isBranch(c.form) {
		return 0
	}
	return int(-c.vals[len(c.vals)-1] / wordBytes)
}

var listingSymbol = regexp.MustCompile(`^main\.c(\d+) STEXT .* size=(\d+)`)
var listingBytes = regexp.MustCompile(`^\t0x[0-9a-f]{4} ((?:[0-9a-f]{2} )+)`)

// TestCaptureGolden writes the golden file from what the Go toolchain's arm64 assembler gives for each case: one
// function per case, its label and pads before the case's instruction, whose four bytes after the pads are the case's
// word.
func TestCaptureGolden(t *testing.T) {
	if os.Getenv(captureVariable) == "" {
		t.Skip("set " + captureVariable + " to write " + goldenFile + " with the Go toolchain's assembler")
	}
	cases := goldenCases()
	var src strings.Builder
	src.WriteString("#include \"textflag.h\"\n")
	for i, c := range cases {
		fmt.Fprintf(&src, "TEXT ·c%d(SB),NOSPLIT|NOFRAME,$0\nback:\n", i)
		for range padOf(c) {
			src.WriteString("\tNOOP\n")
		}
		fmt.Fprintf(&src, "\t%s\n", goText(c))
	}
	dir := t.TempDir()
	asm := filepath.Join(dir, "golden.s")
	if err := os.WriteFile(asm, []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "tool", "asm", "-S", "-p", "main", "-I", filepath.Join(strings.TrimSpace(string(root)), "pkg", "include"), "-o", filepath.Join(dir, "golden.o"), asm)
	cmd.Env = append(os.Environ(), "GOARCH=arm64", "GOOS=linux")
	listing, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, listing)
	}
	code := map[int][]byte{}
	current := -1
	scanner := bufio.NewScanner(bytes.NewReader(listing))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if m := listingSymbol.FindStringSubmatch(line); m != nil {
			current, _ = strconv.Atoi(m[1])
			continue
		}
		if m := listingBytes.FindStringSubmatch(line); m != nil && current >= 0 {
			b, err := hex.DecodeString(strings.ReplaceAll(m[1], " ", ""))
			if err != nil {
				t.Fatal(err)
			}
			code[current] = append(code[current], b...)
		}
	}
	var out strings.Builder
	for i, c := range cases {
		// A function is padded with zeros to its alignment.
		b, end := code[i], (padOf(c)+1)*wordBytes
		if len(b) < end || strings.Trim(hex.EncodeToString(b[end:]), "0") != "" {
			t.Fatalf("%s: %s assembled to %x, not one word after its pads", c.key, goText(c), b)
		}
		fmt.Fprintf(&out, "%s = %x\n", c.key, b[end-wordBytes:end])
	}
	if err := os.WriteFile(goldenFile, []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
