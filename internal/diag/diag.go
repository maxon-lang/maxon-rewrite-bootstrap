package diag

import "fmt"

// Site is a place that is named by handle: a module's index and an offset in the module's text. It says where without
// the text, which belongs to the module, so a diagnostic raised at a node costs no line index until it is reported.
type Site struct {
	Module int32
	Offset uint32
}

// CompileError is a diagnostic. One raised at a node holds its Site and has no File, Line or Col until the compile
// locates it with Locate; one made where the place is known has them already and no Site.
type CompileError struct {
	Code    string
	Message string
	File    string
	Line    int
	Col     int
	Site    Site
}

func (e *CompileError) Error() string {
	if !e.Located() {
		return fmt.Sprintf("module %d offset %d: error %s: %s", e.Site.Module, e.Site.Offset, e.Code, e.Message)
	}
	return fmt.Sprintf("%s:%d:%d: error %s: %s", e.File, e.Line, e.Col, e.Code, e.Message)
}

// Located is whether the error has its file, line and column. Every place has a file, so an error without one has not
// been located.
func (e *CompileError) Located() bool { return e.File != "" }

// Locate gives the error its file, line and column, from where the site is.
func (e *CompileError) Locate(file string, line, col int) { e.File, e.Line, e.Col = file, line, col }

func NewError(code, msg, file string, line, col int) *CompileError {
	return &CompileError{Code: code, Message: msg, File: file, Line: line, Col: col}
}

// NewSiteError is an error at a site, which is located when it is reported.
func NewSiteError(code, msg string, site Site) *CompileError {
	return &CompileError{Code: code, Message: msg, Site: site}
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
	ErrNotCopyable               = "E3135"
	ErrNotDroppable              = "E3136"
	ErrDestructorMisuse          = "E3137"
	ErrCapabilityMisuse          = "E3138"
	ErrTakeApartDestructible     = "E3139"
	ErrUnsignedNegation          = "E3218"
	ErrTypeCycle                 = "E4014"
	ErrUnsupported               = "E9000"
	ErrInterpreterOnly           = "E9002"
)
