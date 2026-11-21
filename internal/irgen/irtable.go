package main

import "minimaxon/internal/irgen/irtable"

// The table is package irtable's, so that rulegen reads it too; this generator reads it by the names it is written in.
type (
	kind     = irtable.Kind
	field    = irtable.Field
	operand  = irtable.Operand
	result   = irtable.Result
	effect   = irtable.Effect
	op       = irtable.Op
	enum     = irtable.Enum
	enumCase = irtable.EnumCase
	flag     = irtable.Flag
)

const (
	kReg      = irtable.KReg
	kRegs     = irtable.KRegs
	kMoreRegs = irtable.KMoreRegs
	kSlot     = irtable.KSlot
	kInt      = irtable.KInt
	kWord     = irtable.KWord
	kSymbol   = irtable.KSymbol
	kEnum     = irtable.KEnum
	kFlag     = irtable.KFlag
	kBlock    = irtable.KBlock
	kFail     = irtable.KFail
	kText     = irtable.KText
	kType     = irtable.KType
	kCallee   = irtable.KCallee
	kRoutine  = irtable.KRoutine
	kErrorReg = irtable.KErrorReg
	kBody     = irtable.KBody
	kCases    = irtable.KCases
	kTable    = irtable.KTable

	fDst   = irtable.FDst
	fA     = irtable.FA
	fB     = irtable.FB
	fC     = irtable.FC
	fImm   = irtable.FImm
	fSub   = irtable.FSub
	fFlags = irtable.FFlags
	fValue = irtable.FValue
	fThen  = irtable.FThen
	fElse  = irtable.FElse
	fFirst = irtable.FFirst
	fCount = irtable.FCount

	noResult       = irtable.NoResult
	valueResult    = irtable.ValueResult
	optionalResult = irtable.OptionalResult

	effReads     = irtable.EffReads
	effWrites    = irtable.EffWrites
	effCalls     = irtable.EffCalls
	effAllocates = irtable.EffAllocates
	effFrees     = irtable.EffFrees
	effExits     = irtable.EffExits
	effFails     = irtable.EffFails
)

var (
	ops     = irtable.Ops
	enums   = irtable.Enums
	flags   = irtable.Flags
	effects = irtable.Effects
)
