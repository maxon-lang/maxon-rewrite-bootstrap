package backend

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"minimaxon/internal/par"
)

// A drop or a copy of a value is code that depends on the type of the value: which of the words of a box are boxes
// themselves, and in what layout. It is generated once for each concrete type that a program drops or copies, after
// monomorphization, as a routine of its own that the code of every module calls, so a type that contains itself (a record
// with an array of itself) is a routine that calls itself and not code written out where it is wanted.
//
// A routine takes one argument, the value, as every internal call does, and a copy returns the new value in rax. A drop
// of a value that is 0 does nothing, which is how a place that was moved from, or never assigned, is dropped.
//
// The routines are in a unit of their own, built after the units of the modules, because which of them a program needs is
// known only once every module has been generated. Which they are, and in what order, comes from the modules in source
// order and from each routine's own parts in field order, so the executable does not depend on the number of jobs.

type routineKind int

const (
	dropRoutine routineKind = iota
	copyRoutine
)

var routineKindNames = [...]string{"drop", "copy"}

const routineKindCount = len(routineKindNames)

// typeRoutine names the routine that drops or copies a value of one type. Types are interned, so two uses of one type
// name one routine.
type typeRoutine struct {
	kind routineKind
	ty   ast.Ty
}

func (r typeRoutine) String() string {
	return fmt.Sprintf("%s %s", routineKindNames[r.kind], r.ty.Name())
}

// routineSet is the routines a body of code asks for, each once, in the order they were first asked for. The name of a
// routine is made once, because a call names it by an interface value that would otherwise be allocated at each call.
type routineSet struct {
	list    []typeRoutine
	symbols [routineKindCount]map[ast.Ty]any
}

// symbol is the name of the code that drops or copies a value of a type, and notes that the code asks for it. A drop of a
// box that holds nothing to drop is the free itself, with no routine of its own.
func (s *routineSet) symbol(kind routineKind, t ast.Ty) any {
	names := s.symbols[kind]
	if name, ok := names[t]; ok {
		return name
	}
	if names == nil {
		names = map[ast.Ty]any{}
		s.symbols[kind] = names
	}
	if kind == dropRoutine && onlyFrees(t) {
		names[t] = ir.RtFree
		return ir.RtFree
	}
	r := typeRoutine{kind, t}
	names[t] = r
	s.list = append(s.list, r)
	return r
}

// onlyFrees is whether dropping a value of the type is freeing its box and nothing else: a record with no managed field,
// or a union that has no managed payload in any case.
func onlyFrees(t ast.Ty) bool {
	switch t := t.(type) {
	case *ast.RecordTy:
		for _, f := range t.FieldList() {
			if t.FieldType(f).IsManaged() {
				return false
			}
		}
		return true
	case *ast.UnionTy:
		for i := range t.CaseList() {
			if anyManaged(t.Payload(i)) {
				return false
			}
		}
		return true
	}
	return false
}

// routinesPerUnit is how many routines go into one unit, which is what lets them be generated in parallel. It is fixed, so
// the units and the executable do not depend on how many jobs generate them.
const routinesPerUnit = 256

// estimatedRoutineBytes sizes a unit's code buffer before its routines are generated, so it is not grown by appending: the
// average of a generated project, rounded up.
const estimatedRoutineBytes = 128

// EmitTypeRoutines is the units with the drop and copy routine of every type that the units ask for, and of every type
// those routines reach. A routine asks for the routines of the parts of its type, so they are made in waves: the ones the
// modules ask for, then the ones those ask for that are not made yet, and so on, each wave in parallel and in the order
// the routines were first asked for.
func EmitTypeRoutines(units []*CodeUnit, jobs int) []*CodeUnit {
	var known routineSet
	var pending []typeRoutine
	for _, unit := range units {
		pending = known.addNew(pending, unit.needed.list)
	}
	var made []*CodeUnit
	for len(pending) > 0 {
		wave := make([]*CodeUnit, (len(pending)+routinesPerUnit-1)/routinesPerUnit)
		par.ForEachIndex(len(wave), jobs, func(i int) {
			wave[i] = emitRoutines(pending[i*routinesPerUnit : min((i+1)*routinesPerUnit, len(pending))])
		})
		made = append(made, wave...)
		var next []typeRoutine
		for _, unit := range wave {
			next = known.addNew(next, unit.needed.list)
		}
		pending = next
	}
	return made
}

// addNew appends to list the routines that the set has not seen, and notes them.
func (s *routineSet) addNew(list, routines []typeRoutine) []typeRoutine {
	for _, r := range routines {
		before := len(s.list)
		s.symbol(r.kind, r.ty)
		if len(s.list) > before {
			list = append(list, r)
		}
	}
	return list
}

func emitRoutines(routines []typeRoutine) *CodeUnit {
	u := NewCodeUnit()
	u.Asm.Code = make([]byte, 0, len(routines)*estimatedRoutineBytes)
	e := &routineEmitter{u: u, a: u.Asm, free: ExternLabel(ir.RtFree), alloc: ExternLabel(ir.RtAlloc)}
	for _, r := range routines {
		e.routine(r)
	}
	u.needed = e.needed
	return u
}

// stringLengthOffset is where a string holds its length, the word before its bytes.
const stringLengthOffset = 0

// Frame offsets of the routines: the argument is above the return address, and the locals below the frame pointer.
const (
	routineArg    = 2 * wordBytes
	routineLocal0 = -wordBytes
	routineLocal1 = -2 * wordBytes
	routineLocal2 = -3 * wordBytes
	routineLocal3 = -4 * wordBytes
)

type routineEmitter struct {
	u      *CodeUnit
	a      *Asm
	needed routineSet
	free   *Label
	alloc  *Label
}

// call calls the routine of a type with the value in rax. A routine that has not been asked for yet is queued.
func (e *routineEmitter) call(kind routineKind, t ast.Ty) {
	e.a.Push(Rax)
	e.a.CallSymbol(e.needed.symbol(kind, t))
	e.a.AddImm(Rsp, wordBytes)
}

func (e *routineEmitter) callWithRax(target *Label) {
	e.a.Push(Rax)
	e.a.Call(target)
	e.a.AddImm(Rsp, wordBytes)
}

// prologue starts a routine that keeps a number of words of its own below the frame pointer. A routine is called once for
// each level of a nested value, so a tree that is deep enough would use up the stack: a routine reserves only what it uses.
func (e *routineEmitter) prologue(locals int) {
	e.a.Push(Rbp)
	e.a.Mov(Rbp, Rsp)
	if locals > 0 {
		e.a.SubImm(Rsp, locals*wordBytes)
	}
}

func (e *routineEmitter) epilogue() {
	e.a.Mov(Rsp, Rbp)
	e.a.Pop(Rbp)
	e.a.Ret()
}

// freeArgument gives back the box that the routine's argument is.
func (e *routineEmitter) freeArgument() {
	e.a.Load(Rax, Rbp, routineArg)
	e.callWithRax(e.free)
}

// allocate calls the allocator for a box of a size, leaving it in rax.
func (e *routineEmitter) allocate(bytes int) {
	e.a.MovImm(Rax, int64(bytes))
	e.callWithRax(e.alloc)
}

// routine emits the code of one routine.
func (e *routineEmitter) routine(r typeRoutine) {
	e.u.BeginSection()
	e.u.DefineCode(r)
	switch r.kind {
	case dropRoutine:
		e.drop(r.ty)
	case copyRoutine:
		e.copy(r.ty)
	default:
		panic(diag.Internal("routines: unknown kind %d", int(r.kind)))
	}
}

func (e *routineEmitter) drop(t ast.Ty) {
	switch t := t.(type) {
	case *ast.PrimTy:
		if t == ast.StringTy {
			e.dropString()
			return
		}
	case *ast.RecordTy:
		e.dropRecord(t)
		return
	case *ast.UnionTy:
		e.dropUnion(t)
		return
	case *ast.BuiltinTy:
		if t.Kind == ast.BuiltinArray {
			e.dropArray(t)
			return
		}
	}
	panic(diag.Internal("routines: no drop routine for %s, which the driver should have refused", t.Name()))
}

func (e *routineEmitter) copy(t ast.Ty) {
	switch t := t.(type) {
	case *ast.PrimTy:
		if t == ast.StringTy {
			e.copyString()
			return
		}
	case *ast.RecordTy:
		e.copyRecord(t)
		return
	case *ast.UnionTy:
		e.copyUnion(t)
		return
	case *ast.BuiltinTy:
		if t.Kind == ast.BuiltinArray {
			e.copyArray(t)
			return
		}
	}
	panic(diag.Internal("routines: no copy routine for %s, which the driver should have refused", t.Name()))
}

// returnIfEmpty is the start of a drop: a value that is 0 holds nothing.
func (e *routineEmitter) returnIfEmpty(done *Label) {
	e.a.Load(Rax, Rbp, routineArg)
	e.a.Test(Rax, Rax)
	e.a.Jcc(CcE, done)
}

// dropWord drops the managed word at an offset of the box that the argument is.
func (e *routineEmitter) dropWord(offset int, t ast.Ty) {
	e.a.Load(Rax, Rbp, routineArg)
	e.a.Load(Rax, Rax, offset)
	e.call(dropRoutine, t)
}

func (e *routineEmitter) dropRecord(t *ast.RecordTy) {
	done := NewLabel()
	e.prologue(0)
	e.returnIfEmpty(done)
	for i, f := range t.FieldList() {
		if ft := t.FieldType(f); ft.IsManaged() {
			e.dropWord(ir.FieldOffset(i, false), ft)
		}
	}
	e.freeArgument()
	e.a.Bind(done)
	e.epilogue()
}

// dropUnion drops the payload of the case the box holds, and then the box.
func (e *routineEmitter) dropUnion(t *ast.UnionTy) {
	done := NewLabel()
	release := NewLabel()
	e.prologue(0)
	e.returnIfEmpty(done)
	for i := range t.CaseList() {
		payload := t.Payload(i)
		if !anyManaged(payload) {
			continue
		}
		next := NewLabel()
		e.a.Load(Rax, Rbp, routineArg)
		e.a.Load(Rax, Rax, ir.TagOffset)
		e.a.CmpImm(Rax, i)
		e.a.Jcc(CcNE, next)
		for j, f := range payload {
			if f.Type.IsManaged() {
				e.dropWord(ir.FieldOffset(j, true), f.Type)
			}
		}
		e.a.Jmp(release)
		e.a.Bind(next)
	}
	e.a.Bind(release)
	e.freeArgument()
	e.a.Bind(done)
	e.epilogue()
}

func anyManaged(fields []ast.PayloadField) bool {
	for _, f := range fields {
		if f.Type.IsManaged() {
			return true
		}
	}
	return false
}

// dropArray drops each element, and then the buffer and the header.
func (e *routineEmitter) dropArray(t *ast.BuiltinTy) {
	a := e.a
	done := NewLabel()
	e.prologue(1)
	e.returnIfEmpty(done)
	if elem := t.Element(); elem.IsManaged() {
		top := NewLabel()
		out := NewLabel()
		a.Xor(Rax, Rax)
		a.Store(Rbp, routineLocal0, Rax)
		a.Bind(top)
		a.Load(Rcx, Rbp, routineArg)
		a.Load(Rdx, Rcx, arrayLengthOffset)
		a.Load(Rax, Rbp, routineLocal0)
		a.Cmp(Rax, Rdx)
		a.Jcc(CcAE, out)
		a.Load(Rcx, Rcx, arrayDataOffset)
		a.MovImm(Rdx, wordBytes)
		a.Imul(Rax, Rdx)
		a.Add(Rcx, Rax)
		a.Load(Rax, Rcx, 0)
		e.call(dropRoutine, elem)
		a.Load(Rax, Rbp, routineLocal0)
		a.AddImm(Rax, 1)
		a.Store(Rbp, routineLocal0, Rax)
		a.Jmp(top)
		a.Bind(out)
	}
	a.Load(Rcx, Rbp, routineArg)
	a.Load(Rax, Rcx, arrayDataOffset)
	e.callWithRax(e.free)
	e.freeArgument()
	a.Bind(done)
	e.epilogue()
}

// jumpIfImmortal jumps when the string that the argument is lives in the executable's data section, which no one frees:
// its address is between the bounds the linker defines. A string on the heap is never there.
func (e *routineEmitter) jumpIfImmortal(target *Label) {
	a := e.a
	a.Load(Rax, Rbp, routineArg)
	a.LeaSymbol(Rcx, ir.RtImageDataStart)
	a.Sub(Rax, Rcx)
	a.LeaSymbol(Rdx, ir.RtImageDataEnd)
	a.Sub(Rdx, Rcx)
	a.Cmp(Rax, Rdx)
	a.Jcc(CcB, target)
}

func (e *routineEmitter) dropString() {
	done := NewLabel()
	e.prologue(0)
	e.returnIfEmpty(done)
	e.jumpIfImmortal(done)
	e.freeArgument()
	e.a.Bind(done)
	e.epilogue()
}

// copyWord copies the word at an offset of the box that the argument is into the same offset of the new box, which is
// the first local: a managed word is copied by the routine of its type.
func (e *routineEmitter) copyWord(offset int, t ast.Ty) {
	a := e.a
	a.Load(Rcx, Rbp, routineArg)
	a.Load(Rax, Rcx, offset)
	if t.IsManaged() {
		e.call(copyRoutine, t)
	}
	a.Load(Rcx, Rbp, routineLocal0)
	a.Store(Rcx, offset, Rax)
}

func (e *routineEmitter) copyRecord(t *ast.RecordTy) {
	fields := t.FieldList()
	e.prologue(1)
	e.allocate(ir.BoxBytes(len(fields), false))
	e.a.Store(Rbp, routineLocal0, Rax)
	for i, f := range fields {
		e.copyWord(ir.FieldOffset(i, false), t.FieldType(f))
	}
	e.a.Load(Rax, Rbp, routineLocal0)
	e.epilogue()
}

// copyUnion makes a box for the case the value holds, sized for its payload as lowering sizes it.
func (e *routineEmitter) copyUnion(t *ast.UnionTy) {
	a := e.a
	done := NewLabel()
	e.prologue(1)
	cases := t.CaseList()
	for i := range cases {
		payload := t.Payload(i)
		last := i == len(cases)-1
		next := NewLabel()
		if !last {
			a.Load(Rax, Rbp, routineArg)
			a.Load(Rax, Rax, ir.TagOffset)
			a.CmpImm(Rax, i)
			a.Jcc(CcNE, next)
		}
		e.allocate(ir.BoxBytes(len(payload), true))
		a.Store(Rbp, routineLocal0, Rax)
		a.MovImm(Rcx, int64(i))
		a.Store(Rax, ir.TagOffset, Rcx)
		for j, f := range payload {
			e.copyWord(ir.FieldOffset(j, true), f.Type)
		}
		if !last {
			a.Jmp(done)
			a.Bind(next)
		}
	}
	a.Bind(done)
	a.Load(Rax, Rbp, routineLocal0)
	e.epilogue()
}

// copyArray makes a header and a buffer of exactly the length, with a copy of each element.
func (e *routineEmitter) copyArray(t *ast.BuiltinTy) {
	a := e.a
	const (
		header = routineLocal0
		length = routineLocal1
		index  = routineLocal2
		buffer = routineLocal3
	)
	done := NewLabel()
	top := NewLabel()
	e.prologue(4)
	e.allocate(arrayHeaderBytes)
	a.Store(Rbp, header, Rax)
	a.Load(Rcx, Rbp, routineArg)
	a.Load(Rax, Rcx, arrayLengthOffset)
	a.Store(Rbp, length, Rax)
	a.Test(Rax, Rax)
	a.Jcc(CcE, done)
	a.MovImm(Rdx, wordBytes)
	a.Imul(Rax, Rdx)
	e.callWithRax(e.alloc)
	a.Store(Rbp, buffer, Rax)
	a.Load(Rcx, Rbp, header)
	a.Load(Rax, Rbp, length)
	a.Store(Rcx, arrayLengthOffset, Rax)
	a.Store(Rcx, arrayCapacityOffset, Rax)
	a.Load(Rax, Rbp, buffer)
	a.Store(Rcx, arrayDataOffset, Rax)
	a.Xor(Rax, Rax)
	a.Store(Rbp, index, Rax)
	a.Bind(top)
	a.Load(Rax, Rbp, index)
	a.Load(Rdx, Rbp, length)
	a.Cmp(Rax, Rdx)
	a.Jcc(CcAE, done)
	a.Load(Rcx, Rbp, routineArg)
	a.Load(Rcx, Rcx, arrayDataOffset)
	a.MovImm(Rdx, wordBytes)
	a.Imul(Rax, Rdx)
	a.Add(Rcx, Rax)
	a.Load(Rax, Rcx, 0)
	if elem := t.Element(); elem.IsManaged() {
		e.call(copyRoutine, elem)
	}
	a.Load(Rcx, Rbp, index)
	a.MovImm(Rdx, wordBytes)
	a.Imul(Rcx, Rdx)
	a.Load(Rdx, Rbp, buffer)
	a.Add(Rcx, Rdx)
	a.Store(Rcx, 0, Rax)
	a.Load(Rax, Rbp, index)
	a.AddImm(Rax, 1)
	a.Store(Rbp, index, Rax)
	a.Jmp(top)
	a.Bind(done)
	a.Load(Rax, Rbp, header)
	e.epilogue()
}

// copyString is a new string with the same length and bytes, unless the string is in the executable: that one is never
// freed and never changes, so its copy is itself.
func (e *routineEmitter) copyString() {
	a := e.a
	immortal := NewLabel()
	e.prologue(1)
	e.jumpIfImmortal(immortal)
	a.Load(Rcx, Rbp, routineArg)
	a.Load(Rax, Rcx, stringLengthOffset)
	a.AddImm(Rax, wordBytes)
	e.callWithRax(e.alloc)
	a.Store(Rbp, routineLocal0, Rax)
	a.Mov(Rdi, Rax)
	a.Load(Rsi, Rbp, routineArg)
	a.Load(Rcx, Rsi, stringLengthOffset)
	a.AddImm(Rcx, wordBytes)
	a.RepMovsb()
	a.Load(Rax, Rbp, routineLocal0)
	e.epilogue()
	a.Bind(immortal)
	a.Load(Rax, Rbp, routineArg)
	e.epilogue()
}
