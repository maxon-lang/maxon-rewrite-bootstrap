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
	Base() *NominalBase
}

// NominalBase is what the nominal types share. A generic declaration has Params and its instances, one for each
// list of type arguments, have Args instead: an instance reads its members and conformances through the
// declaration, substituting its Args for the declaration's Params.
type NominalBase struct {
	name    string
	Methods map[string]*FuncDecl
	// Params are the type parameters of a generic declaration.
	Params []*TypeParamTy
	// Args are the type arguments of an instance.
	Args []Ty
	// Conforms are the interfaces the declaration says it implements.
	Conforms  []*InterfaceTy
	declaring *NominalBase
	instances *InstanceTable
}

func (n *NominalBase) Name() string                      { return n.name }
func (n *NominalBase) MethodTable() map[string]*FuncDecl { return n.Methods }
func (n *NominalBase) Base() *NominalBase                { return n }

// Declaration is the base of the declaration this type is an instance of, or the base itself for any other type.
func (n *NominalBase) Declaration() *NominalBase {
	if n.declaring != nil {
		return n.declaring
	}
	return n
}

// IsGeneric is whether the type is a generic declaration, which needs type arguments before it names a type.
func (n *NominalBase) IsGeneric() bool { return len(n.Params) > 0 }

func (n *NominalBase) newInstanceBase(args []Ty) NominalBase {
	return NominalBase{name: InstanceSpelling(n.name, typeNames(args)), Methods: n.Methods, Args: args, declaring: n}
}

func (n *NominalBase) initGeneric(params []*TypeParam, where []*Constraint) {
	for _, p := range params {
		p.Ty = NewTypeParamOf(p, where)
		n.Params = append(n.Params, p.Ty)
	}
	if len(params) > 0 {
		n.instances = &InstanceTable{}
	}
}

type FieldInfo struct {
	Name     string
	Exported bool
	Type     Ty
	IsVar    bool
	Default  Expr
	Index    int
	// Uses are the calls to generic functions in the default, which a literal that takes the default also makes.
	Uses []*CallExpr
}

type RecordTy struct {
	NominalBase
	Decl   *TypeDecl
	Fields []*FieldInfo
	origin *RecordTy
}

func NewRecordTy(d *TypeDecl) *RecordTy {
	r := &RecordTy{name: d.Name, Methods: map[string]*FuncDecl{}, Decl: d}
	r.initGeneric(d.TypeParams, d.Where)
	return r
}

func (r *RecordTy) IsManaged() bool { return true }

// FieldList is the fields of the record; an instance has the declaration's.
func (r *RecordTy) FieldList() []*FieldInfo {
	if r.origin != nil {
		return r.origin.Fields
	}
	return r.Fields
}

func (r *RecordTy) Field(name string) *FieldInfo {
	for _, f := range r.FieldList() {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// FieldType is the type of a field as this record has it: an instance has the declaration's type with its own type
// arguments substituted.
func (r *RecordTy) FieldType(f *FieldInfo) Ty {
	if r.origin == nil {
		return f.Type
	}
	return subst(f.Type, r.origin.Params, r.Args)
}

type EnumTy struct {
	NominalBase
	Decl           *EnumDecl
	Cases          []string
	Raw            []I128
	HasExplicitRaw bool
}

func NewEnumTy(d *EnumDecl) *EnumTy {
	return &EnumTy{name: d.Name, Methods: map[string]*FuncDecl{}, Decl: d}
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
	Decl   *UnionDecl
	Cases  []*UnionCase
	origin *UnionTy
}

func NewUnionTy(d *UnionDecl) *UnionTy {
	u := &UnionTy{name: d.Name, Methods: map[string]*FuncDecl{}, Decl: d}
	u.initGeneric(d.TypeParams, d.Where)
	return u
}

func (u *UnionTy) IsManaged() bool { return true }

// CaseList is the cases of the union; an instance has the declaration's.
func (u *UnionTy) CaseList() []*UnionCase {
	if u.origin != nil {
		return u.origin.Cases
	}
	return u.Cases
}

func (u *UnionTy) CaseIndex(name string) int {
	for i, c := range u.CaseList() {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// Payload is the payload of a case as this union has it, with an instance's type arguments substituted.
func (u *UnionTy) Payload(caseIndex int) []PayloadField {
	payload := u.CaseList()[caseIndex].Payload
	if u.origin == nil {
		return payload
	}
	b := Binding{Params: u.origin.Params, Args: u.Args}
	var out []PayloadField
	for i, p := range payload {
		t := b.Apply(p.Type)
		if t != p.Type && out == nil {
			out = append(make([]PayloadField, 0, len(payload)), payload[:i]...)
		}
		if out != nil {
			out = append(out, PayloadField{Name: p.Name, Type: t})
		}
	}
	if out == nil {
		return payload
	}
	return out
}

// Convention says how a parameter takes its argument.
type Convention int

const (
	// ConvLet is the default: an immutable borrow.
	ConvLet Convention = iota
	// ConvMutates is a mutable borrow: the caller sees the callee's changes.
	ConvMutates
	// ConvConsumes moves the argument into the callee, which owns it.
	ConvConsumes
	// ConvSets takes an uninitialised variable that the callee must initialise.
	ConvSets
	// ConvDestructs is the receiver of a destructor: it moves into the method like ConvConsumes, and ends the value.
	ConvDestructs
)

// ConventionOf reads the keyword a parameter or receiver is written with; none means a let.
func ConventionOf(word string) Convention {
	switch word {
	case "":
		return ConvLet
	case "mutates":
		return ConvMutates
	case "consumes":
		return ConvConsumes
	case "sets":
		return ConvSets
	case "destructs":
		return ConvDestructs
	}
	panic(diag.Internal("unknown parameter convention '%s'", word))
}

// ByAddress is whether the caller passes the argument's address, so the callee writes the caller's storage.
func (c Convention) ByAddress() bool { return c == ConvMutates || c == ConvSets }

// Owns is whether the callee owns what it is given: the caller's value moves in.
func (c Convention) Owns() bool { return c == ConvConsumes || c == ConvDestructs }

func (c Convention) String() string {
	switch c {
	case ConvLet:
		return "let"
	case ConvMutates:
		return "mutates"
	case ConvConsumes:
		return "consumes"
	case ConvSets:
		return "sets"
	case ConvDestructs:
		return "destructs"
	}
	panic(diag.Internal("unknown convention %d", int(c)))
}

type Local struct {
	Name    string
	Type    Ty
	IsVar   bool
	IsParam bool
	IsSelf  bool
	// Conv is how a parameter (or the receiver) takes its argument; it means nothing for other locals.
	Conv Convention
	// Projected marks a match binding of a payload that is not a scalar: a borrow of part of the subject.
	Projected  bool
	ParamIndex int
	Slot       int
	// id is the local's handle in the function that lists it.
	id LocalID
}

func NewLocal(name string, t Ty, isVar bool) *Local {
	return &Local{Name: name, Type: t, IsVar: isVar, ParamIndex: -1, Slot: -1}
}

// ByRef is whether the local is the address of the caller's storage.
func (l *Local) ByRef() bool { return l.IsParam && !l.IsSelf && l.Conv.ByAddress() }

// Borrowed is whether the local only borrows its value, so the value cannot be moved out of it.
func (l *Local) Borrowed() bool {
	return l.Projected || (l.IsParam && !l.Conv.Owns())
}

// Mutable is whether the local, and the fields reached through it, may be assigned.
func (l *Local) Mutable() bool {
	return l.IsVar || (l.IsParam && l.Conv.ByAddress())
}

func Shell(d TypeDeclNode) Ty {
	return VisitTypeDeclNodeResult[Ty](shell{}, d)
}

type shell struct{}

// VisitAliasDecl is the integer type a typealias of a range names. A typealias of an instance has no shell: its type
// is the instance, which the aliases phase finds once the types it names are known.
func (shell) VisitAliasDecl(t *AliasDecl) Ty {
	if t.Instance != nil {
		return nil
	}
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
	Name string
	Type Ty
	Conv Convention
}

func (shell) VisitTypeDecl(t *TypeDecl) Ty { return NewRecordTy(t) }

func (shell) VisitEnumDecl(t *EnumDecl) Ty { return NewEnumTy(t) }

func (shell) VisitUnionDecl(t *UnionDecl) Ty { return NewUnionTy(t) }

func (shell) VisitInterfaceDecl(t *InterfaceDecl) Ty { return NewInterfaceTy(t) }
