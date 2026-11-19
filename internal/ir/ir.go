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

type StoreSlot struct {
	InstrBase
	Slot int
	Src  V
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

type IrFunction struct {
	Decl       *ast.FuncDecl
	ParamCount int
	Slots      []IrKind
	Regs       []IrKind
	Blocks     []*IrBlock
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
	fmt.Fprintf(sb, "function %s params %d", f.Decl.Symbol(), f.ParamCount)
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
		return fmt.Sprintf("%sruntime %s(%s)", dstPrefix(x.Dst), x.Symbol, argList(x.Args))
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
