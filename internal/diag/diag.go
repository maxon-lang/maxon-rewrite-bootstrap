package diag

import "fmt"

type CompileError struct {
	Code    string
	Message string
	File    string
	Line    int
	Col     int
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("%s:%d:%d: error %s: %s", e.File, e.Line, e.Col, e.Code, e.Message)
}

func NewError(code, msg, file string, line, col int) *CompileError {
	return &CompileError{Code: code, Message: msg, File: file, Line: line, Col: col}
}

type InternalError struct {
	Message string
}

func (e *InternalError) Error() string { return e.Message }

func Internal(format string, args ...any) *InternalError {
	return &InternalError{Message: fmt.Sprintf(format, args...)}
}

func Guard(errs *[]*CompileError, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(*CompileError); ok {
				*errs = append(*errs, ce)
				return
			}
			panic(r)
		}
	}()
	f()
}

const (
	ErrUnexpectedToken           = "E2001"
	ErrExpectedType              = "E2003"
	ErrExpressionError           = "E2004"
	ErrUnexpectedEnd             = "E2007"
	ErrMismatchedEndLabel        = "E2008"
	ErrExpectedToken             = "E2010"
	ErrIntegerLiteralRange       = "E2011"
	ErrDependsOnItself           = "E2012"
	ErrAssignToImmutable         = "E2013"
	ErrMatchNotExhaustive        = "E2026"
	ErrMatchDefaultEnumMustThrow = "E2046"
	ErrMatchBlockStatement       = "E2049"
	ErrCallFirstArgNamed         = "E2052"
	ErrCallArgMissingLabel       = "E2053"
	ErrNoMainFunction            = "E3001"
	ErrMainWrongReturnType       = "E3002"
	ErrUnknownFunction           = "E3004"
	ErrTypeMismatch              = "E3005"
	ErrDuplicateDefinition       = "E3006"
	ErrUnknownType               = "E3011"
	ErrMissingReturn             = "E3013"
	ErrUnknownField              = "E3018"
	ErrEnumDuplicateCase         = "E3030"
	ErrEnumUnknownCase           = "E3034"
	ErrEnumWrongBindingCount     = "E3035"
	ErrCallArgCountMismatch      = "E3036"
	ErrCallUnknownArgLabel       = "E3037"
	ErrCallDuplicateArg          = "E3038"
	ErrEnumNotComparable         = "E3066"
	ErrConstructorRestriction    = "E3076"
	ErrEmptyBlock                = "E3082"
	ErrFieldNotInitialized       = "E3086"
	ErrUseAfterMove              = "E3102"
	ErrDivisionByZero            = "E3103"
	ErrMoveOutOfBorrow           = "E3110"
	ErrMoveOutOfField            = "E3111"
	ErrExclusivity               = "E3112"
	ErrUseBeforeInit             = "E3113"
	ErrSetNotInitialized         = "E3114"
	ErrSetArgument               = "E3115"
	ErrTypeArgumentCount         = "E3116"
	ErrBoundNotMet               = "E3117"
	ErrCannotInfer               = "E3118"
	ErrInterfaceAsType           = "E3119"
	ErrConformance               = "E3120"
	ErrAmbiguousBound            = "E3121"
	ErrInstantiationTooDeep      = "E3122"
	ErrGenericDeclaration        = "E3123"
	ErrMapKey                    = "E3124"
	ErrFrozenChange              = "E3125"
	ErrBehaviourBorrow           = "E3126"
	ErrFreezeScalar              = "E3127"
	ErrShareTarget               = "E3128"
	ErrWhenCowns                 = "E3129"
	ErrWhenSameCown              = "E3130"
	ErrCownCycle                 = "E3131"
	ErrCownAccess                = "E3132"
	ErrUnknownTypeParam          = "E3133"
	ErrAliasTarget               = "E3134"
	ErrUnsignedNegation          = "E3218"
	ErrTypeCycle                 = "E4014"
	ErrUnsupported               = "E9000"
	ErrInterpreterOnly           = "E9002"
)
