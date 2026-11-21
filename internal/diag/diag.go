package diag

import "fmt"

//go:generate go run minimaxon/internal/diaggen

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
	// Unqualified is a bare name that the error did not find, when the message goes on to name the other modules that
	// declare it. Only every module's table says which, and a module is checked as soon as the modules it names are, so
	// the hint is written when the error is located, once every module is parsed.
	Unqualified string
}

func (e *CompileError) Error() string {
	if !e.Located() {
		return fmt.Sprintf("error %s: module %d offset %d: %s", e.Code, e.Site.Module, e.Site.Offset, e.Message)
	}
	return fmt.Sprintf("error %s: %s:%d:%d: %s", e.Code, e.File, e.Line, e.Col, e.Message)
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
