package ir

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
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

type BinOp uint8

const (
	BinAdd BinOp = iota
	BinSub
	BinMul
	BinDiv
	BinMod
	BinAnd
	BinOr
	BinXor
	BinShl
	BinShr
)

var binOpNames = [...]string{"add", "sub", "mul", "div", "mod", "and", "or", "xor", "shl", "shr"}

type CmpOp uint8

const (
	CmpEq CmpOp = iota
	CmpNe
	CmpLt
	CmpLe
	CmpGt
	CmpGe
)

var cmpOpNames = [...]string{"eq", "ne", "lt", "le", "gt", "ge"}

type GuardKind uint8

const (
	GuardNonNegative GuardKind = iota
	GuardSignedAtLeast
	GuardSignedAtMost
	GuardUnsignedAtLeast
	GuardUnsignedAtMost
)

var guardNames = [...]string{"NonNegative", "SignedAtLeast", "SignedAtMost", "UnsignedAtLeast", "UnsignedAtMost"}

// Opcode says what an Instr does, which is also what each of its fields means (below). The set is closed, and a pass
// that handles every opcode ends in a panic for one it does not know.
type Opcode uint8

const (
	opInvalid Opcode = iota
	OpConst
	OpStr
	OpLoadSlot
	OpStoreSlot
	OpSlotAddr
	OpLoadParam
	OpLoadField
	OpStoreField
	OpFieldAddr
	OpLoadAt
	OpStoreAt
	OpLoadGlobal
	OpBin
	OpCmp
	OpNeg
	OpNot
	OpGuard
	OpAlloc
	OpCall
	OpCallRuntime
	OpWhen
	OpDrop
	OpDropSlot
	OpDropAt
	OpMoveSlot
	OpMoveAt
	OpCopy
)

// InstrFlags are the booleans of an instruction.
type InstrFlags uint8

const (
	// FlagBorrow is on a StoreSlot whose slot only names a part of the match subject for the arm, and owns nothing.
	FlagBorrow InstrFlags = 1 << iota
	// FlagUnsigned is on a Bin or a Cmp that treats its operands as unsigned.
	FlagUnsigned
	// FlagChecked is on a Bin that traps on overflow.
	FlagChecked
)

// NoRoutine is the C of a CallRuntime that stores no routine.
const NoRoutine = -1

// Instr is one instruction: a record of fixed size, so a function's instructions are one slice, read in order. The fields
// mean what the opcode says; the ones that name a register are always V, and an operand that is a number lives in Imm.
// What does not fit in a record (a type, a callee, a string, a list of arguments) is in the function's pools, which C and
// a range of A and B index.
//
//	OpConst        Dst = Imm
//	OpStr          Dst = the string Strings[C]
//	OpLoadSlot     Dst = slot Imm
//	OpStoreSlot    slot Imm = A (FlagBorrow: the slot owns nothing)
//	OpSlotAddr     Dst = address of slot Imm
//	OpLoadParam    Dst = parameter Imm
//	OpLoadField    Dst = word at A + Imm
//	OpStoreField   word at A + Imm = B
//	OpFieldAddr    Dst = A + Imm
//	OpLoadAt       Dst = word at A
//	OpStoreAt      word at A = B
//	OpLoadGlobal   Dst = the global Imm (a Rt)
//	OpBin          Dst = A op Sub B (FlagUnsigned, FlagChecked)
//	OpCmp          Dst = A cmp Sub B (FlagUnsigned)
//	OpNeg, OpNot   Dst = op A
//	OpGuard        traps unless A is in range Sub of bound Imm
//	OpAlloc        Dst = a new box of Imm bytes
//	OpCall         Dst (or NoReg) = Callees[C] applied to the arguments Args[A : A+B]
//	OpCallRuntime  Dst (or NoReg) = the entry Imm (a Rt) applied to Args[A : A+B]; C is the index in Callees of the routine
//	               the entry stores to call later (the destructor of a cown), or NoRoutine
//	OpWhen         schedules the behaviour Behaviours[Imm] on the cowns Args[A : A+B], capturing Args[A+B : A+B+C]
//	OpDrop         drops the value in A, of type Types[C]
//	OpDropSlot     drops what slot Imm owns, of type Types[C]
//	OpDropAt       drops what the address A holds, of type Types[C]
//	OpMoveSlot     Dst = slot Imm, which is left empty
//	OpMoveAt       Dst = the value at A, which is left empty
//	OpCopy         Dst = a deep copy of A, of type Types[C]
type Instr struct {
	Op    Opcode
	Sub   uint8
	Flags InstrFlags
	Dst   V
	A     V
	B     V
	C     int32
	Imm   int64
}

func (i Instr) Has(flag InstrFlags) bool { return i.Flags&flag != 0 }

func flags(borrow, unsigned, checked bool) InstrFlags {
	var f InstrFlags
	if borrow {
		f |= FlagBorrow
	}
	if unsigned {
		f |= FlagUnsigned
	}
	if checked {
		f |= FlagChecked
	}
	return f
}

func Const(dst V, value int64) Instr { return Instr{Op: OpConst, Dst: dst, Imm: value} }

func LoadSlot(dst V, slot int) Instr { return Instr{Op: OpLoadSlot, Dst: dst, Imm: int64(slot)} }

// StoreSlot puts a value in a slot. A slot owns what it holds unless borrow is set, which a match binding of a payload
// uses: the slot names a part of the match subject for the arm, and nothing drops it.
func StoreSlot(slot int, src V, borrow bool) Instr {
	return Instr{Op: OpStoreSlot, A: src, Imm: int64(slot), Flags: flags(borrow, false, false)}
}

func SlotAddr(dst V, slot int) Instr { return Instr{Op: OpSlotAddr, Dst: dst, Imm: int64(slot)} }

func LoadParam(dst V, param int) Instr { return Instr{Op: OpLoadParam, Dst: dst, Imm: int64(param)} }

func LoadField(dst, obj V, offset int) Instr {
	return Instr{Op: OpLoadField, Dst: dst, A: obj, Imm: int64(offset)}
}

func StoreField(obj V, offset int, src V) Instr {
	return Instr{Op: OpStoreField, A: obj, B: src, Imm: int64(offset)}
}

func FieldAddr(dst, obj V, offset int) Instr {
	return Instr{Op: OpFieldAddr, Dst: dst, A: obj, Imm: int64(offset)}
}

func LoadAt(dst, addr V) Instr { return Instr{Op: OpLoadAt, Dst: dst, A: addr} }

func StoreAt(addr, src V) Instr { return Instr{Op: OpStoreAt, A: addr, B: src} }

func LoadGlobal(dst V, symbol Rt) Instr { return Instr{Op: OpLoadGlobal, Dst: dst, Imm: int64(symbol)} }

func Bin(dst V, op BinOp, a, b V, unsigned, checked bool) Instr {
	return Instr{Op: OpBin, Dst: dst, A: a, B: b, Sub: uint8(op), Flags: flags(false, unsigned, checked)}
}

func Cmp(dst V, op CmpOp, a, b V, unsigned bool) Instr {
	return Instr{Op: OpCmp, Dst: dst, A: a, B: b, Sub: uint8(op), Flags: flags(false, unsigned, false)}
}

func Neg(dst, src V) Instr { return Instr{Op: OpNeg, Dst: dst, A: src} }

func Not(dst, src V) Instr { return Instr{Op: OpNot, Dst: dst, A: src} }

func Guard(src V, kind GuardKind, bound int64) Instr {
	return Instr{Op: OpGuard, A: src, Sub: uint8(kind), Imm: bound}
}

func Alloc(dst V, bytes int) Instr { return Instr{Op: OpAlloc, Dst: dst, Imm: int64(bytes)} }

func MoveSlot(dst V, slot int) Instr { return Instr{Op: OpMoveSlot, Dst: dst, Imm: int64(slot)} }

func MoveAt(dst, addr V) Instr { return Instr{Op: OpMoveAt, Dst: dst, A: addr} }

func (i Instr) BinOp() BinOp { return BinOp(i.Sub) }

func (i Instr) CmpOp() CmpOp { return CmpOp(i.Sub) }

func (i Instr) GuardKind() GuardKind { return GuardKind(i.Sub) }

// Slot is the slot an instruction names.
func (i Instr) Slot() int { return int(i.Imm) }

// Offset is the byte offset a field instruction names.
func (i Instr) Offset() int { return int(i.Imm) }

// Symbol is the runtime entry or global an instruction names.
func (i Instr) Symbol() Rt { return Rt(i.Imm) }

// TermKind says how a block ends.
type TermKind uint8

const (
	termNone TermKind = iota
	TermJump
	TermBranch
	TermSwitch
	TermRet
	TermTrap
)

// SwitchCase is a value of a switch and the block it goes to.
type SwitchCase struct {
	Value  int64
	Target BlockID
}

// Term is how a block ends, a record of fixed size like an instruction.
//
//	TermJump    to Then
//	TermBranch  to Then if Value is not 0, else to Else
//	TermSwitch  to the target of the case of Cases[First : First+Count] that Value equals, else to Then
//	TermRet     returns Value
//	TermTrap    stops the program
type Term struct {
	Kind  TermKind
	Value V
	Then  BlockID
	Else  BlockID
	First int32
	Count int32
}

func Jump(target BlockID) Term { return Term{Kind: TermJump, Then: target} }

func Branch(cond V, then, els BlockID) Term {
	return Term{Kind: TermBranch, Value: cond, Then: then, Else: els}
}

func Ret(value V) Term { return Term{Kind: TermRet, Value: value} }

func Trap() Term { return Term{Kind: TermTrap} }

// Block is a run of instructions, Instrs[First:End] of its function, and how it ends.
type Block struct {
	First, End int32
	Term       Term
}

// Refusal is a place in the source that lowered to something the backend cannot run yet. Lowering still completes, so
// the IR stays whole for the interpreter, and the driver turns the first refusal of each function into an error before
// any code is generated.
type Refusal struct {
	Site    ast.Node
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

// CallArgs are the arguments of a call or of a runtime entry.
func (f *IrFunction) CallArgs(i Instr) []V { return f.Args[i.A : i.A+i.B] }

// Callee is the function an OpCall calls.
func (f *IrFunction) Callee(i Instr) *ast.FuncDecl { return f.Callees[i.C] }

// Routine is the function an OpCallRuntime stores to call later, or nil.
func (f *IrFunction) Routine(i Instr) *ast.FuncDecl {
	if i.C == NoRoutine {
		return nil
	}
	return f.Callees[i.C]
}

// TypeOf is the type a drop or a copy names.
func (f *IrFunction) TypeOf(i Instr) ast.Ty { return f.Types[i.C] }

// Text is the string an OpStr makes.
func (f *IrFunction) Text(i Instr) string { return f.Strings[i.C] }

// WhenParts are the behaviour an OpWhen schedules, the cowns it waits for and the values it captures.
func (f *IrFunction) WhenParts(i Instr) (body *IrFunction, cowns, captures []V) {
	end := i.A + i.B
	return f.Behaviours[i.Imm], f.Args[i.A:end], f.Args[end : end+V(i.C)]
}

func kindName(k IrKind) string {
	if k == KindManaged {
		return "managed"
	}
	return "scalar"
}

func argList(args []V) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func quote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(s) + "\""
}

func dstPrefix(d V) string {
	if d == NoReg {
		return ""
	}
	return d.String() + " = "
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

// InstrString is the text the printer gives an instruction, for a tool that reports a place in a function.
func (f *IrFunction) InstrString(i Instr) string {
	switch i.Op {
	case OpConst:
		return fmt.Sprintf("%s = const %d", i.Dst, i.Imm)
	case OpStr:
		return fmt.Sprintf("%s = str %s", i.Dst, quote(f.Text(i)))
	case OpLoadSlot:
		return fmt.Sprintf("%s = load s%d", i.Dst, i.Imm)
	case OpStoreSlot:
		if i.Has(FlagBorrow) {
			return fmt.Sprintf("store s%d, %s borrow", i.Imm, i.A)
		}
		return fmt.Sprintf("store s%d, %s", i.Imm, i.A)
	case OpSlotAddr:
		return fmt.Sprintf("%s = addr s%d", i.Dst, i.Imm)
	case OpLoadParam:
		return fmt.Sprintf("%s = param %d", i.Dst, i.Imm)
	case OpLoadField:
		return fmt.Sprintf("%s = field %s+%d", i.Dst, i.A, i.Imm)
	case OpStoreField:
		return fmt.Sprintf("setfield %s+%d, %s", i.A, i.Imm, i.B)
	case OpFieldAddr:
		return fmt.Sprintf("%s = fieldaddr %s+%d", i.Dst, i.A, i.Imm)
	case OpLoadAt:
		return fmt.Sprintf("%s = loadat %s", i.Dst, i.A)
	case OpStoreAt:
		return fmt.Sprintf("storeat %s, %s", i.A, i.B)
	case OpLoadGlobal:
		return fmt.Sprintf("%s = global %s", i.Dst, i.Symbol())
	case OpBin:
		suffix := ""
		if i.Has(FlagUnsigned) {
			suffix += ".u"
		}
		if i.Has(FlagChecked) {
			suffix += ".checked"
		}
		return fmt.Sprintf("%s = %s%s %s, %s", i.Dst, binOpNames[i.Sub], suffix, i.A, i.B)
	case OpCmp:
		suffix := ""
		if i.Has(FlagUnsigned) {
			suffix = ".u"
		}
		return fmt.Sprintf("%s = cmp.%s%s %s, %s", i.Dst, cmpOpNames[i.Sub], suffix, i.A, i.B)
	case OpNeg:
		return fmt.Sprintf("%s = neg.checked %s", i.Dst, i.A)
	case OpNot:
		return fmt.Sprintf("%s = not %s", i.Dst, i.A)
	case OpGuard:
		return fmt.Sprintf("guard %s %s %d", i.A, guardNames[i.Sub], i.Imm)
	case OpAlloc:
		return fmt.Sprintf("%s = alloc %d", i.Dst, i.Imm)
	case OpCall:
		return fmt.Sprintf("%scall %s(%s)", dstPrefix(i.Dst), f.Callee(i).Symbol(), argList(f.CallArgs(i)))
	case OpCallRuntime:
		text := fmt.Sprintf("%sruntime %s(%s)", dstPrefix(i.Dst), i.Symbol(), argList(f.CallArgs(i)))
		if routine := f.Routine(i); routine != nil {
			text += " routine " + routine.Symbol()
		}
		return text
	case OpWhen:
		body, cowns, captures := f.WhenParts(i)
		return fmt.Sprintf("when %s cowns [%s] captures [%s]", body.Symbol(), argList(cowns), argList(captures))
	case OpDrop:
		return fmt.Sprintf("drop %s %s", i.A, f.TypeOf(i).Name())
	case OpDropSlot:
		return fmt.Sprintf("dropslot s%d %s", i.Imm, f.TypeOf(i).Name())
	case OpDropAt:
		return fmt.Sprintf("dropat %s %s", i.A, f.TypeOf(i).Name())
	case OpCopy:
		return fmt.Sprintf("%s = copy %s %s", i.Dst, i.A, f.TypeOf(i).Name())
	case OpMoveSlot:
		return fmt.Sprintf("%s = move s%d", i.Dst, i.Imm)
	case OpMoveAt:
		return fmt.Sprintf("%s = moveat %s", i.Dst, i.A)
	}
	panic(diag.Internal("printer: unhandled opcode %d", int(i.Op)))
}

// TermString is the text the printer gives a terminator.
func (f *IrFunction) TermString(t Term) string {
	switch t.Kind {
	case TermJump:
		return fmt.Sprintf("jump b%d", t.Then)
	case TermBranch:
		return fmt.Sprintf("branch %s, b%d, b%d", t.Value, t.Then, t.Else)
	case TermSwitch:
		cases := f.Cases[t.First : t.First+t.Count]
		parts := make([]string, len(cases))
		for i, c := range cases {
			parts[i] = fmt.Sprintf("%d: b%d", c.Value, c.Target)
		}
		return fmt.Sprintf("switch %s [%s] default b%d", t.Value, strings.Join(parts, ", "), t.Then)
	case TermRet:
		return fmt.Sprintf("ret %s", t.Value)
	case TermTrap:
		return "trap"
	}
	panic(diag.Internal("printer: a block has no terminator"))
}
