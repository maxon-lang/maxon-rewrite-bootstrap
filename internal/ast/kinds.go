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

type NameKind uint8

const (
	NameUnresolved NameKind = iota
	NameLocal
	NameConst
	NameSelfField
)

type CallKind uint8

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

type MemberKind uint8

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
