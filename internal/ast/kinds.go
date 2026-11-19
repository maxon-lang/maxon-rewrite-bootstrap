package ast

type PatternKind int

const (
	PatternLiteral PatternKind = iota
	PatternRange
	PatternCase
)

type ArmKind int

const (
	ArmThen ArmKind = iota
	ArmGives
	ArmPanic
)

type NameKind int

const (
	NameUnresolved NameKind = iota
	NameLocal
	NameConst
	NameSelfField
)

type CallKind int

const (
	CallUnresolved CallKind = iota
	CallFunction
	CallMethod
	CallStatic
	CallUnionCtor
	CallPrint
	CallPrintError
	CallCopy
	CallBound
)

type MemberKind int

const (
	MemberUnresolved MemberKind = iota
	MemberField
	MemberEnumCase
	MemberUnionCase
	MemberEnumName
	MemberEnumOrdinal
	MemberEnumRaw
	MemberUnionName
	MemberUnionOrdinal
)
