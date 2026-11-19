package ast

import (
	"fmt"
	"minimaxon/internal/diag"
)

type Ty interface {
	Name() string
	IsManaged() bool
}

type IntTy struct {
	Alias string
	Min   I128
	Max   I128
}

var Anonymous64 = &IntTy{Min: I64Min, Max: I64Max}

func (t *IntTy) Name() string {
	if t.Alias != "" {
		return t.Alias
	}
	return fmt.Sprintf("int(%s to %s)", t.Min, t.Max)
}

func (t *IntTy) IsManaged() bool      { return false }
func (t *IntTy) Unsigned() bool       { return t.Max.Gt(I64Max) }
func (t *IntTy) IsAnonymous() bool    { return t.Alias == "" }
func (t *IntTy) Holds(v I128) bool    { return v.Ge(t.Min) && v.Le(t.Max) }
func (t *IntTy) Covers(o *IntTy) bool { return o.Min.Ge(t.Min) && o.Max.Le(t.Max) }

func (t *IntTy) IsFullWidth() bool {
	if t.Unsigned() {
		return t.Min.IsZero() && t.Max == U64Max
	}
	return t.Min == I64Min && t.Max == I64Max
}

type ConstIntTy struct {
	Value I128
}

func (t *ConstIntTy) Name() string    { return "integer constant " + t.Value.String() }
func (t *ConstIntTy) IsManaged() bool { return false }

type PrimTy struct {
	name    string
	managed bool
}

var (
	BoolTy   = &PrimTy{name: "bool"}
	StringTy = &PrimTy{name: "String", managed: true}
	VoidTy   = &PrimTy{name: "void"}
	NeverTy  = &PrimTy{name: "never"}
)

func (t *PrimTy) Name() string    { return t.name }
func (t *PrimTy) IsManaged() bool { return t.managed }

type NominalTy interface {
	Ty
	MethodTable() map[string]*FuncDecl
}

type NominalBase struct {
	name    string
	Methods map[string]*FuncDecl
}

func (n *NominalBase) Name() string                      { return n.name }
func (n *NominalBase) MethodTable() map[string]*FuncDecl { return n.Methods }

type FieldInfo struct {
	Name     string
	Exported bool
	Type     Ty
	IsVar    bool
	Default  Expr
	Index    int
}

type RecordTy struct {
	NominalBase
	Decl   *TypeDecl
	Fields []*FieldInfo
}

func NewRecordTy(d *TypeDecl) *RecordTy {
	return &RecordTy{NominalBase: NominalBase{name: d.Name, Methods: map[string]*FuncDecl{}}, Decl: d}
}

func (r *RecordTy) IsManaged() bool { return true }

func (r *RecordTy) Field(name string) *FieldInfo {
	for _, f := range r.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

type EnumTy struct {
	NominalBase
	Decl           *EnumDecl
	Cases          []string
	Raw            []I128
	HasExplicitRaw bool
}

func NewEnumTy(d *EnumDecl) *EnumTy {
	return &EnumTy{NominalBase: NominalBase{name: d.Name, Methods: map[string]*FuncDecl{}}, Decl: d}
}

func (e *EnumTy) IsManaged() bool { return false }

func (e *EnumTy) CaseIndex(name string) int {
	for i, c := range e.Cases {
		if c == name {
			return i
		}
	}
	return -1
}

type PayloadField struct {
	Name string
	Type Ty
}

type UnionCase struct {
	Name    string
	Payload []PayloadField
}

type UnionTy struct {
	NominalBase
	Decl  *UnionDecl
	Cases []*UnionCase
}

func NewUnionTy(d *UnionDecl) *UnionTy {
	return &UnionTy{NominalBase: NominalBase{name: d.Name, Methods: map[string]*FuncDecl{}}, Decl: d}
}

func (u *UnionTy) IsManaged() bool { return true }

func (u *UnionTy) CaseIndex(name string) int {
	for i, c := range u.Cases {
		if c.Name == name {
			return i
		}
	}
	return -1
}

type Local struct {
	Name       string
	Type       Ty
	IsVar      bool
	IsParam    bool
	IsSelf     bool
	ByRef      bool
	ParamIndex int
	Slot       int
}

func NewLocal(name string, t Ty, isVar bool) *Local {
	return &Local{Name: name, Type: t, IsVar: isVar, ParamIndex: -1, Slot: -1}
}

func Shell(d TypeDeclNode) Ty {
	return VisitTypeDeclNodeResult[Ty](shell{}, d)
}

type shell struct{}

func (shell) VisitAliasDecl(t *AliasDecl) Ty {
	lo, hi := t.Low.value(), t.High.value()
	if !t.Inclusive {
		hi = hi.Sub(One)
	}
	if lo.Gt(hi) {
		panic(t.Error(diag.ErrTypeMismatch, fmt.Sprintf("the range of '%s' is empty", t.Name)))
	}
	if lo.IsNeg() && hi.Gt(I64Max) {
		panic(t.Error(diag.ErrTypeMismatch, "an integer range cannot span both negative values and values above i64.max"))
	}
	return &IntTy{Alias: t.Name, Min: lo, Max: hi}
}

// sizedRanges are the bounds a range can name as, for example, i64.min.
var sizedRanges = map[string][2]I128{
	"u8":  {Zero, I64(255)},
	"u16": {Zero, I64(65535)},
	"u32": {Zero, I64(4294967295)},
	"u64": {Zero, U64Max},
	"i8":  {I64(-128), I64(127)},
	"i16": {I64(-32768), I64(32767)},
	"i32": {I64(-2147483648), I64(2147483647)},
	"i64": {I64Min, I64Max},
}

func (b *Bound) value() I128 {
	v := b.Value
	if b.Sized != "" {
		r, ok := sizedRanges[b.Sized]
		if !ok {
			panic(b.Error(diag.ErrUnknownType, fmt.Sprintf("'%s' is not a sized integer; a bound names one of u8, u16, u32, u64, i8, i16, i32 or i64", b.Sized)))
		}
		switch b.Limit {
		case "min":
			v = r[0]
		case "max":
			v = r[1]
		default:
			panic(b.Error(diag.ErrUnknownField, fmt.Sprintf("'%s.%s' is not a bound; write '%s.min' or '%s.max'", b.Sized, b.Limit, b.Sized, b.Sized)))
		}
	}
	if b.Negative {
		return v.Neg()
	}
	return v
}

// ParamSpec is a parameter as the checker resolved it.
type ParamSpec struct {
	Name  string
	Type  Ty
	ByRef bool
}

func (shell) VisitTypeDecl(t *TypeDecl) Ty { return NewRecordTy(t) }

func (shell) VisitEnumDecl(t *EnumDecl) Ty { return NewEnumTy(t) }

func (shell) VisitUnionDecl(t *UnionDecl) Ty { return NewUnionTy(t) }
