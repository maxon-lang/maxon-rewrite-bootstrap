// Package irtable is the table of IR operations (table.go): irgen generates self/ir's records, printer and visitors from
// it, and rulegen checks the operations and operands of the rewrite rules against it. The table is written in the names
// below its exported ones, which name the same types, constants and lists.
package irtable

type (
	Kind     = kind
	Field    = field
	Operand  = operand
	Result   = result
	Effect   = effect
	Op       = op
	Enum     = enum
	EnumCase = enumCase
	Flag     = flag
)

const (
	KReg          = kReg
	KRegs         = kRegs
	KMoreRegs     = kMoreRegs
	KSlot         = kSlot
	KInt          = kInt
	KWord         = kWord
	KSymbol       = kSymbol
	KEnum         = kEnum
	KFlag         = kFlag
	KBlock        = kBlock
	KFail         = kFail
	KText         = kText
	KType         = kType
	KCallee       = kCallee
	KRoutine      = kRoutine
	KErrorReg     = kErrorReg
	KBody         = kBody
	KCases        = kCases
	KTable        = kTable
	KEdgeArgs     = kEdgeArgs
	KMoreEdgeArgs = kMoreEdgeArgs

	FDst      = fDst
	FA        = fA
	FB        = fB
	FC        = fC
	FImm      = fImm
	FSub      = fSub
	FFlags    = fFlags
	FValue    = fValue
	FThen     = fThen
	FElse     = fElse
	FFirst    = fFirst
	FCount    = fCount
	FArgs     = fArgs
	FThenArgs = fThenArgs
	FElseArgs = fElseArgs

	NoResult       = noResult
	ValueResult    = valueResult
	OptionalResult = optionalResult

	EffReads     = effReads
	EffWrites    = effWrites
	EffCalls     = effCalls
	EffAllocates = effAllocates
	EffFrees     = effFrees
	EffExits     = effExits
	EffFails     = effFails
)

var (
	// Ops are the operations, instructions and terminators, in the order of their opcodes.
	Ops = ops
	// Enums are the small closed sets that operands name.
	Enums = enums
	// Flags are the bits of an instruction's flags, in the order of their bits.
	Flags = flags
	// Effects are the effects of an operation in the order of their bits, without EffFails, which comes last.
	Effects = effects
)
