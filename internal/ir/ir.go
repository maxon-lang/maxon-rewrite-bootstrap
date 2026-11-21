package ir

//go:generate go run minimaxon/internal/irgen

import (
	"fmt"
	"minimaxon/internal/ast"
	"strconv"
	"strings"
)

type IrKind uint8

const (
	KindScalar IrKind = iota
	KindManaged
)

// V is a virtual register: an index into IrFunction.Regs.
type V int32

// NoReg is the destination of an instruction that gives no value.
const NoReg V = -1

func (v V) String() string { return "%" + strconv.Itoa(int(v)) }

// BlockID is a basic block: an index into IrFunction.Blocks.
type BlockID int32

// NoBlock is the failure target of an operation that cannot fail.
const NoBlock BlockID = -1

// NoRoutine is the routine index of a CallRuntime that stores no routine.
const NoRoutine = -1

// SwitchCase is a value of a switch and the block it goes to.
type SwitchCase struct {
	Value  int64
	Target BlockID
}

// Block is a run of instructions, Instrs[First:End] of its function, and how it ends.
type Block struct {
	First, End int32
	Term       Term
}

// Refusal is a place in the source that lowered to something the backend cannot run yet. Lowering still completes, so
// the IR stays whole for the interpreter, and the driver turns the first refusal of each function into an error before
// any code is generated.
type Refusal struct {
	Site    ast.Pos
	Code    string
	Message string
}

// IrFunction is a function's IR: the instructions of all its blocks in one slice, block after block, and the pools that the
// instructions index. Its slices are views of an Arena, so a function is valid until its arena is reset.
type IrFunction struct {
	Decl *ast.FuncDecl
	// Name is what the function is called when that is not its declaration's symbol: a behaviour body is named for the
	// function it is in, and an instance of a generic function that holds one has one for each instance.
	Name       string
	ParamCount int
	Slots      []IrKind
	Regs       []IrKind
	Instrs     []Instr
	Blocks     []Block
	// The pools, which the C of an instruction and a range of its A and B index.
	Args    []V
	Types   []ast.Ty
	Callees []*ast.FuncDecl
	Strings []string
	Cases   []SwitchCase
	// Refusals are the places in the source that cannot be compiled yet, in source order.
	Refusals []Refusal
	// UsesArrays is whether the function calls an array entry, which the backend then links the array runtime for.
	UsesArrays bool
	// UsesBytes is whether the function calls a byte array entry, which the backend then links the byte array runtime for.
	UsesBytes bool
	// UsesOs is whether the function calls an OS entry, which the backend then links the OS runtime for.
	UsesOs bool
	// Behaviours are the bodies of the `when` statements in the function, in source order. A `when` in one of them has its
	// body in that one.
	Behaviours []*IrFunction
}

// Symbol is the name of the function in the IR text and in a report.
func (f *IrFunction) Symbol() string {
	if f.Name != "" {
		return f.Name
	}
	return f.Decl.Symbol()
}

func (f *IrFunction) NewReg(kind IrKind) V {
	f.Regs = append(f.Regs, kind)
	return V(len(f.Regs) - 1)
}

func (f *IrFunction) NewSlot(kind IrKind) int {
	f.Slots = append(f.Slots, kind)
	return len(f.Slots) - 1
}

// BlockInstrs are the instructions of a block, not counting how it ends.
func (f *IrFunction) BlockInstrs(b int) []Instr {
	blk := &f.Blocks[b]
	return f.Instrs[blk.First:blk.End]
}

func kindName(k IrKind) string {
	if k == KindManaged {
		return "managed"
	}
	return "scalar"
}

// The texts of the printer's parts, which the generated InstrString and TermString put together.

func regList(regs []V) string {
	parts := make([]string, len(regs))
	for i, r := range regs {
		parts[i] = r.String()
	}
	return strings.Join(parts, ", ")
}

func quote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(s) + "\""
}

// resultText is what comes before an operation that gives a value, and nothing for a call whose value nothing reads.
func resultText(d V) string {
	if d == NoReg {
		return ""
	}
	return d.String() + " = "
}

func slotText(slot int) string { return "s" + strconv.Itoa(slot) }

func blockText(b BlockID) string { return "b" + strconv.Itoa(int(b)) }

func caseList(cases []SwitchCase) string {
	parts := make([]string, len(cases))
	for i, c := range cases {
		parts[i] = strconv.FormatInt(c.Value, 10) + ": " + blockText(c.Target)
	}
	return strings.Join(parts, ", ")
}

func PrintIr(f *IrFunction, sb *strings.Builder) {
	fmt.Fprintf(sb, "function %s params %d", f.Symbol(), f.ParamCount)
	if len(f.Slots) > 0 {
		parts := make([]string, len(f.Slots))
		for i, k := range f.Slots {
			parts[i] = fmt.Sprintf("s%d %s", i, kindName(k))
		}
		sb.WriteString(" slots [" + strings.Join(parts, ", ") + "]")
	}
	sb.WriteByte('\n')
	for b := range f.Blocks {
		fmt.Fprintf(sb, "b%d:\n", b)
		for _, i := range f.BlockInstrs(b) {
			sb.WriteString("  " + f.InstrString(i) + "\n")
		}
		sb.WriteString("  " + f.TermString(f.Blocks[b].Term) + "\n")
	}
	sb.WriteByte('\n')
	for _, body := range f.Behaviours {
		PrintIr(body, sb)
	}
}
