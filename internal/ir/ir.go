package ir

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"strings"
)

type IrKind int

const (
	KindScalar IrKind = iota
	KindManaged
)

type V int

func (v V) String() string { return fmt.Sprintf("%%%d", int(v)) }

type BinOp int

const (
	OpAdd BinOp = iota
	OpSub
	OpMul
	OpDiv
	OpMod
	OpAnd
	OpOr
	OpXor
	OpShl
	OpShr
)

var binOpNames = [...]string{"add", "sub", "mul", "div", "mod", "and", "or", "xor", "shl", "shr"}

type CmpOp int

const (
	CmpEq CmpOp = iota
	CmpNe
	CmpLt
	CmpLe
	CmpGt
	CmpGe
)

var cmpOpNames = [...]string{"eq", "ne", "lt", "le", "gt", "ge"}

type GuardKind int

const (
	GuardNonNegative GuardKind = iota
	GuardSignedAtLeast
	GuardSignedAtMost
	GuardUnsignedAtLeast
	GuardUnsignedAtMost
)

var guardNames = [...]string{"NonNegative", "SignedAtLeast", "SignedAtMost", "UnsignedAtLeast", "UnsignedAtMost"}

type Instr interface{ instr() }

type InstrBase struct{}

func (InstrBase) instr() {}

type ConstInstr struct {
	InstrBase
	Dst   V
	Value int64
}

type StrInstr struct {
	InstrBase
	Dst   V
	Value string
}

type LoadSlot struct {
	InstrBase
	Dst  V
	Slot int
}

// StoreSlot puts a value in a slot. A slot owns what it holds unless Borrow is set, which a match binding of a payload
// uses: the slot names a part of the match subject for the arm, and nothing drops it.
type StoreSlot struct {
	InstrBase
	Slot   int
	Src    V
	Borrow bool
}

type SlotAddr struct {
	InstrBase
	Dst  V
	Slot int
}

type LoadParam struct {
	InstrBase
	Dst   V
	Param int
}

type LoadField struct {
	InstrBase
	Dst    V
	Obj    V
	Offset int
}

type StoreField struct {
	InstrBase
	Obj    V
	Offset int
	Src    V
}

type FieldAddr struct {
	InstrBase
	Dst    V
	Obj    V
	Offset int
}

type LoadAt struct {
	InstrBase
	Dst  V
	Addr V
}

type StoreAt struct {
	InstrBase
	Addr V
	Src  V
}

type LoadGlobal struct {
	InstrBase
	Dst    V
	Symbol string
}

type BinInstr struct {
	InstrBase
	Dst      V
	Op       BinOp
	A        V
	B        V
	Unsigned bool
	Checked  bool
}

type CmpInstr struct {
	InstrBase
	Dst      V
	Op       CmpOp
	A        V
	B        V
	Unsigned bool
}

type NegInstr struct {
	InstrBase
	Dst V
	Src V
}

type NotInstr struct {
	InstrBase
	Dst V
	Src V
}

type GuardInstr struct {
	InstrBase
	Src   V
	Kind  GuardKind
	Bound int64
}

type AllocInstr struct {
	InstrBase
	Dst   V
	Bytes int
}

type CallInstr struct {
	InstrBase
	Dst    *V
	Target *ast.FuncDecl
	Args   []V
}

type CallRuntimeInstr struct {
	InstrBase
	Dst    *V
	Symbol string
	Args   []V
	// Routine is the function that the entry stores to call later: the destructor that a cown ends its content with
	// (RtCownNew). It is nil for every other entry.
	Routine *ast.FuncDecl
}

// WhenInstr schedules a behaviour: the body of a `when`, which runs later, in another thread, once it has exclusive access
// to every cown it names (README, "Behaviours"). The cowns are references that the instruction only reads, and each gains
// a count of its own, held until the behaviour has run. Each capture is a value that moves into the behaviour: a managed
// one has been moved out of its owner, so it is in flight, and a scalar is copied. The body is a function of its own
// whose parameters are the access to the value of each cown, an address as a mutates parameter has, and then the
// captures, which it owns as a consumes parameter does. The behaviour returns nothing and nothing waits for it.
type WhenInstr struct {
	InstrBase
	Body     *IrFunction
	Cowns    []V
	Captures []V
}

// The instructions below say where an owner dies or gives its value away (README, "The IR"). Each names the type of
// the value, with the type arguments of an instance already applied, so codegen can call the drop routine of that
// type. Dropping a value drops what it owns, deeply by type: the fields of a record, the payload of a union, the
// elements of a primitive. A place that holds no value, because it was moved from or never assigned, is dropped as a
// no-op, so a drop at the end of a scope needs no knowledge of the path that led there.

// DropInstr drops the owned value in a register, such as a temporary that nothing took, a removed element, or the old
// value of a place just read.
type DropInstr struct {
	InstrBase
	Src V
	Ty  ast.Ty
}

// DropSlotInstr drops what a slot owns, if anything, and leaves it empty.
type DropSlotInstr struct {
	InstrBase
	Slot int
	Ty   ast.Ty
}

// DropAtInstr drops what the address holds, if anything, and leaves it empty: the old value of a field, an element or an
// mutates parameter that is about to be assigned.
type DropAtInstr struct {
	InstrBase
	Addr V
	Ty   ast.Ty
}

// MoveSlotInstr reads a slot and leaves it empty: the value now belongs to whoever receives it.
type MoveSlotInstr struct {
	InstrBase
	Dst  V
	Slot int
}

// MoveAtInstr reads the value at an address and leaves it empty, which is how a payload leaves the box of a union that
// is consumed.
type MoveAtInstr struct {
	InstrBase
	Dst  V
	Addr V
}

// CopyInstr is a deep copy of the owned value in Src, which is left as it is: a new box for each record, union and array
// inside it, and a new string for each string that is not in the executable, so every part of the copy has an owner of its
// own to free it. A frozen value or a cown reference copies as another owner of the same box. Ty is the type of the value,
// with an instance's type arguments applied, which is what tells the copy where the managed words of each box are.
type CopyInstr struct {
	InstrBase
	Dst V
	Src V
	Ty  ast.Ty
}

type Terminator interface{ term() }

type TermBase struct{}

func (TermBase) term() {}

type JumpTerm struct {
	TermBase
	Target *IrBlock
}

type BranchTerm struct {
	TermBase
	Cond V
	Then *IrBlock
	Else *IrBlock
}

type SwitchTerm struct {
	TermBase
	Value   V
	Values  []int64
	Targets []*IrBlock
	Default *IrBlock
}

type RetTerm struct {
	TermBase
	Value V
}

type TrapTerm struct {
	TermBase
}

type IrBlock struct {
	ID     int
	Instrs []Instr
	Term   Terminator
}

// Refusal is a place in the source that lowered to something the backend cannot run yet. Lowering still completes, so
// the IR stays whole for the interpreter, and the driver turns the first refusal of each function into an error before
// any code is generated.
type Refusal struct {
	Site    ast.Node
	Code    string
	Message string
}

type IrFunction struct {
	Decl *ast.FuncDecl
	// Name is what the function is called when that is not its declaration's symbol: a behaviour body is named for the
	// function it is in, and an instance of a generic function that holds one has one for each instance.
	Name       string
	ParamCount int
	Slots      []IrKind
	Regs       []IrKind
	Blocks     []*IrBlock
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

func (f *IrFunction) NewBlock() *IrBlock {
	b := &IrBlock{ID: len(f.Blocks)}
	f.Blocks = append(f.Blocks, b)
	return b
}

func Successors(b *IrBlock) []*IrBlock {
	switch t := b.Term.(type) {
	case *JumpTerm:
		return []*IrBlock{t.Target}
	case *BranchTerm:
		return []*IrBlock{t.Then, t.Else}
	case *SwitchTerm:
		return append(append([]*IrBlock{}, t.Targets...), t.Default)
	case *RetTerm, *TrapTerm:
		return nil
	}
	panic(diag.Internal("block b%d has no terminator", b.ID))
}

func (f *IrFunction) RemoveUnreachable() {
	reached := make([]bool, len(f.Blocks))
	work := []*IrBlock{f.Blocks[0]}
	reached[0] = true
	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]
		for _, next := range Successors(b) {
			if !reached[next.ID] {
				reached[next.ID] = true
				work = append(work, next)
			}
		}
	}
	kept := f.Blocks[:0]
	for _, b := range f.Blocks {
		if reached[b.ID] {
			kept = append(kept, b)
		}
	}
	f.Blocks = kept
	for i, b := range f.Blocks {
		b.ID = i
	}
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

func dstPrefix(d *V) string {
	if d == nil {
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
	for _, b := range f.Blocks {
		fmt.Fprintf(sb, "b%d:\n", b.ID)
		for _, i := range b.Instrs {
			sb.WriteString("  " + instrText(i) + "\n")
		}
		sb.WriteString("  " + termText(b.Term) + "\n")
	}
	sb.WriteByte('\n')
	for _, body := range f.Behaviours {
		PrintIr(body, sb)
	}
}

func instrText(i Instr) string {
	switch x := i.(type) {
	case *ConstInstr:
		return fmt.Sprintf("%s = const %d", x.Dst, x.Value)
	case *StrInstr:
		return fmt.Sprintf("%s = str %s", x.Dst, quote(x.Value))
	case *LoadSlot:
		return fmt.Sprintf("%s = load s%d", x.Dst, x.Slot)
	case *StoreSlot:
		if x.Borrow {
			return fmt.Sprintf("store s%d, %s borrow", x.Slot, x.Src)
		}
		return fmt.Sprintf("store s%d, %s", x.Slot, x.Src)
	case *SlotAddr:
		return fmt.Sprintf("%s = addr s%d", x.Dst, x.Slot)
	case *LoadParam:
		return fmt.Sprintf("%s = param %d", x.Dst, x.Param)
	case *LoadField:
		return fmt.Sprintf("%s = field %s+%d", x.Dst, x.Obj, x.Offset)
	case *StoreField:
		return fmt.Sprintf("setfield %s+%d, %s", x.Obj, x.Offset, x.Src)
	case *FieldAddr:
		return fmt.Sprintf("%s = fieldaddr %s+%d", x.Dst, x.Obj, x.Offset)
	case *LoadAt:
		return fmt.Sprintf("%s = loadat %s", x.Dst, x.Addr)
	case *StoreAt:
		return fmt.Sprintf("storeat %s, %s", x.Addr, x.Src)
	case *LoadGlobal:
		return fmt.Sprintf("%s = global %s", x.Dst, x.Symbol)
	case *BinInstr:
		suffix := ""
		if x.Unsigned {
			suffix += ".u"
		}
		if x.Checked {
			suffix += ".checked"
		}
		return fmt.Sprintf("%s = %s%s %s, %s", x.Dst, binOpNames[x.Op], suffix, x.A, x.B)
	case *CmpInstr:
		suffix := ""
		if x.Unsigned {
			suffix = ".u"
		}
		return fmt.Sprintf("%s = cmp.%s%s %s, %s", x.Dst, cmpOpNames[x.Op], suffix, x.A, x.B)
	case *NegInstr:
		return fmt.Sprintf("%s = neg.checked %s", x.Dst, x.Src)
	case *NotInstr:
		return fmt.Sprintf("%s = not %s", x.Dst, x.Src)
	case *GuardInstr:
		return fmt.Sprintf("guard %s %s %d", x.Src, guardNames[x.Kind], x.Bound)
	case *AllocInstr:
		return fmt.Sprintf("%s = alloc %d", x.Dst, x.Bytes)
	case *CallInstr:
		return fmt.Sprintf("%scall %s(%s)", dstPrefix(x.Dst), x.Target.Symbol(), argList(x.Args))
	case *CallRuntimeInstr:
		text := fmt.Sprintf("%sruntime %s(%s)", dstPrefix(x.Dst), x.Symbol, argList(x.Args))
		if x.Routine != nil {
			text += " routine " + x.Routine.Symbol()
		}
		return text
	case *WhenInstr:
		return fmt.Sprintf("when %s cowns [%s] captures [%s]", x.Body.Symbol(), argList(x.Cowns), argList(x.Captures))
	case *DropInstr:
		return fmt.Sprintf("drop %s %s", x.Src, x.Ty.Name())
	case *DropSlotInstr:
		return fmt.Sprintf("dropslot s%d %s", x.Slot, x.Ty.Name())
	case *DropAtInstr:
		return fmt.Sprintf("dropat %s %s", x.Addr, x.Ty.Name())
	case *CopyInstr:
		return fmt.Sprintf("%s = copy %s %s", x.Dst, x.Src, x.Ty.Name())
	case *MoveSlotInstr:
		return fmt.Sprintf("%s = move s%d", x.Dst, x.Slot)
	case *MoveAtInstr:
		return fmt.Sprintf("%s = moveat %s", x.Dst, x.Addr)
	}
	panic(diag.Internal("printer: unhandled instruction %T", i))
}

func termText(t Terminator) string {
	switch x := t.(type) {
	case *JumpTerm:
		return fmt.Sprintf("jump b%d", x.Target.ID)
	case *BranchTerm:
		return fmt.Sprintf("branch %s, b%d, b%d", x.Cond, x.Then.ID, x.Else.ID)
	case *SwitchTerm:
		parts := make([]string, len(x.Values))
		for i, v := range x.Values {
			parts[i] = fmt.Sprintf("%d: b%d", v, x.Targets[i].ID)
		}
		return fmt.Sprintf("switch %s [%s] default b%d", x.Value, strings.Join(parts, ", "), x.Default.ID)
	case *RetTerm:
		return fmt.Sprintf("ret %s", x.Value)
	case *TrapTerm:
		return "trap"
	}
	panic(diag.Internal("printer: a block has no terminator"))
}

// InstrString is the text the printer gives an instruction, for a tool that reports a place in a function.
func InstrString(i Instr) string { return instrText(i) }

// TermString is the text the printer gives a terminator.
func TermString(t Terminator) string { return termText(t) }
