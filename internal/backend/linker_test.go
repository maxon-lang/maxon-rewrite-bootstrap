package backend

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/ast"
)

const (
	testLinkUnits       = 60
	testLinkSections    = 40
	testLinkEntry       = "u0.s0"
	testLinkDeadLiteral = "dead literal"
)

func testSection(unit, section int) string { return fmt.Sprintf("u%d.s%d", unit, section) }

// linkUnits makes units whose sections call each other at random and load strings and values that other units define,
// and whose section 0 of unit 0 is the entry. Sections that nothing reaches hold testLinkDeadLiteral.
func linkUnits(seed uint64) []*CodeUnit {
	rng := rand.New(rand.NewPCG(seed, 1))
	units := make([]*CodeUnit, testLinkUnits)
	for i := range units {
		u := NewCodeUnit()
		units[i] = u
		u.DefineData(fmt.Sprintf("u%d.value", i), 16)
		for s := range testLinkSections {
			u.BeginSection()
			u.DefineCode(testSection(i, s))
			a := u.Asm
			a.Raw(make([]byte, 1+rng.IntN(40))...)
			for range rng.IntN(5) {
				a.CallSymbol(testSection(rng.IntN(testLinkUnits), rng.IntN(testLinkSections)))
			}
			a.LeaData(Rax, u.Data.String(fmt.Sprintf("literal %d.%d", i, s)))
			a.LoadSymbol(Rax, fmt.Sprintf("u%d.value", rng.IntN(testLinkUnits)))
			a.Raw(0xC3)
		}
	}
	return units
}

func linkToBytes(t *testing.T, units []*CodeUnit, entry string, jobs int) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.exe")
	Link(out, units, entry, jobs)
	exe, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// What the link keeps, and where it puts it, is a property of the units: the same for any number of jobs, which the
// mark and the layout do in parallel.
func TestLinkIsIdenticalForAnyJobCount(t *testing.T) {
	for seed := uint64(1); seed <= 3; seed++ {
		want := linkToBytes(t, linkUnits(seed), testLinkEntry, 1)
		for _, jobs := range []int{2, 3, 16} {
			if got := linkToBytes(t, linkUnits(seed), testLinkEntry, jobs); !bytes.Equal(got, want) {
				t.Errorf("seed %d: the executable differs between jobs=1 and jobs=%d", seed, jobs)
			}
		}
	}
}

// Only what the entry reaches is kept: a chain of calls through every unit keeps its strings, and the strings of a
// section that nothing calls are dropped.
func TestLinkKeepsOnlyWhatTheEntryReaches(t *testing.T) {
	units := make([]*CodeUnit, 4)
	for i := range units {
		u := NewCodeUnit()
		units[i] = u
		for s := range 2 {
			u.BeginSection()
			u.DefineCode(testSection(i, s))
			if s == 0 && i+1 < len(units) {
				u.Asm.CallSymbol(testSection(i+1, 0))
			}
			if s == 0 {
				u.Asm.LeaData(Rax, u.Data.String(fmt.Sprintf("live literal %d", i)))
			} else {
				u.Asm.LeaData(Rax, u.Data.String(testLinkDeadLiteral))
			}
			u.Asm.Raw(0xC3)
		}
	}
	for _, jobs := range []int{1, 4} {
		exe := linkToBytes(t, units, testSection(0, 0), jobs)
		for i := range units {
			if !bytes.Contains(exe, []byte(fmt.Sprintf("live literal %d", i))) {
				t.Errorf("jobs=%d: the literal of unit %d, which the entry reaches, is not in the executable", jobs, i)
			}
		}
		if bytes.Contains(exe, []byte(testLinkDeadLiteral)) {
			t.Errorf("jobs=%d: the literal of a section that nothing calls is in the executable", jobs)
		}
	}
}

func catchInternal(t *testing.T, work func()) (message string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if wrapped, ok := r.(interface{ Error() string }); ok {
			message = wrapped.Error()
			return
		}
		t.Fatalf("panicked with %v, not an error", r)
	}()
	work()
	return ""
}

func TestLinkRefusesASymbolThatTwoUnitsDefine(t *testing.T) {
	var units []*CodeUnit
	for range 2 {
		u := NewCodeUnit()
		u.BeginSection()
		u.DefineCode(testLinkEntry)
		u.Asm.Raw(0xC3)
		units = append(units, u)
	}
	for _, jobs := range []int{1, 4} {
		message := catchInternal(t, func() { Link(filepath.Join(t.TempDir(), "out.exe"), units, testLinkEntry, jobs) })
		if !strings.Contains(message, "defined") || !strings.Contains(message, testLinkEntry) {
			t.Errorf("jobs=%d: got %q, want an error naming the symbol that is defined twice", jobs, message)
		}
	}
}

func TestLinkRefusesASymbolThatOneUnitDefinesTwice(t *testing.T) {
	message := catchInternal(t, func() {
		u := NewCodeUnit()
		u.BeginSection()
		u.DefineCode(testLinkEntry)
		u.Asm.Raw(0xC3)
		u.BeginSection()
		u.DefineCode(testLinkEntry)
		u.Asm.Raw(0xC3)
		Link(filepath.Join(t.TempDir(), "out.exe"), []*CodeUnit{u}, testLinkEntry, 1)
	})
	if !strings.Contains(message, "defined") || !strings.Contains(message, testLinkEntry) {
		t.Errorf("got %q, want an error naming the symbol that is defined twice", message)
	}
}

// A symbol that nothing defines is an error only where live code uses it: the checker assumes the source is correct, so
// a dead function that calls a missing one costs nothing.
func TestLinkRefusesAnUndefinedSymbolOnlyInLiveCode(t *testing.T) {
	build := func(liveCallsMissing bool) []*CodeUnit {
		u := NewCodeUnit()
		u.BeginSection()
		u.DefineCode(testLinkEntry)
		if liveCallsMissing {
			u.Asm.CallSymbol("missing")
		}
		u.Asm.Raw(0xC3)
		u.BeginSection()
		u.DefineCode("dead")
		u.Asm.CallSymbol("missing")
		u.Asm.Raw(0xC3)
		return []*CodeUnit{u}
	}
	linkToBytes(t, build(false), testLinkEntry, 4)
	message := catchInternal(t, func() { linkToBytes(t, build(true), testLinkEntry, 4) })
	if !strings.Contains(message, "undefined symbol missing") {
		t.Errorf("got %q, want an error naming the undefined symbol", message)
	}
}

// A link that fails leaves the executable that was there before, and no file of its own.
func TestAbandonedOutputKeepsTheOldExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.exe")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := OpenPeOutput(path)
	output.Start(NewPeImage(peWriteChunk*3, 0, "kernel32.dll", []string{"ExitProcess"}, 0))
	output.CodeFinal(peWriteChunk * 2)
	output.Abandon()
	if data, err := os.ReadFile(path); err != nil || string(data) != "old" {
		t.Errorf("the old executable was not kept: %q, %v", data, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the abandoned link left files behind: %v", entries)
	}
}

// testInstance is an instance of a generic function, the one symbol that more than one unit may define. The same value
// is the same symbol, as the interned instance is in a compile.
func testInstance(name string) *ast.FuncDecl {
	generic := &ast.FuncDecl{Name: name, LinkName: name}
	return &ast.FuncDecl{Name: name, LinkName: name + "[T]", Generic: generic}
}

// instanceMarker is the code of the instance that the unit with this number defines, which no other unit has.
func instanceMarker(unit int) []byte { return []byte{0xDE, 0xC0, 0xDE, byte(unit)} }

// instanceUnits are an entry unit that calls the symbol and then each unit in definers, which defines it with a code of
// its own, the way every module that needs the same instance does.
func instanceUnits(function *ast.FuncDecl, definers ...int) []*CodeUnit {
	entry := NewCodeUnit()
	entry.BeginSection()
	entry.DefineCode(testLinkEntry)
	entry.Asm.CallSymbol(function.LinkName)
	entry.Asm.Raw(0xC3)
	units := []*CodeUnit{entry}
	for _, number := range definers {
		u := NewCodeUnit()
		u.BeginSection()
		u.DefineFunction(function)
		u.Asm.Raw(instanceMarker(number)...)
		u.Asm.Raw(0xC3)
		units = append(units, u)
	}
	return units
}

// requireOnlyDefinition fails unless the executable holds the code of the unit with this number and of no other.
func requireOnlyDefinition(t *testing.T, exe []byte, kept int, others []int, context string) {
	t.Helper()
	if !bytes.Contains(exe, instanceMarker(kept)) {
		t.Errorf("%s: the code of the first unit that defines the instance, %d, is not in the executable", context, kept)
	}
	for _, n := range others {
		if bytes.Contains(exe, instanceMarker(n)) {
			t.Errorf("%s: the code of unit %d, which comes after %d, is in the executable too", context, n, kept)
		}
	}
}

// An instance is defined by every module that needs it, and the link keeps the definition of the first unit in unit
// order, whichever job entered its definition first.
func TestLinkKeepsTheFirstDefinitionOfAnInstance(t *testing.T) {
	instance := testInstance("swap")
	for _, jobs := range []int{1, 2, 16} {
		for range 20 {
			exe := linkToBytes(t, instanceUnits(instance, 1, 2, 3, 4, 5, 6, 7, 8), testLinkEntry, jobs)
			requireOnlyDefinition(t, exe, 1, []int{2, 3, 4, 5, 6, 7, 8}, fmt.Sprintf("jobs=%d", jobs))
		}
	}
	// Which definition wins follows the order of the units, not the numbers in them.
	exe := linkToBytes(t, instanceUnits(instance, 7, 3, 5), testLinkEntry, 4)
	requireOnlyDefinition(t, exe, 7, []int{3, 5}, "units in the order 7, 3, 5")
}

func TestLinkKeepsOneDefinitionOfAnInstanceAmongManyUnits(t *testing.T) {
	instance := testInstance("swap")
	var definers, others []int
	for n := 1; n <= 200; n++ {
		definers = append(definers, n)
		if n > 1 {
			others = append(others, n)
		}
	}
	want := linkToBytes(t, instanceUnits(instance, definers...), testLinkEntry, 1)
	requireOnlyDefinition(t, want, 1, others, "jobs=1")
	for _, jobs := range []int{3, 16} {
		for range 10 {
			if got := linkToBytes(t, instanceUnits(instance, definers...), testLinkEntry, jobs); !bytes.Equal(got, want) {
				t.Fatalf("the executable differs between jobs=1 and jobs=%d", jobs)
			}
		}
	}
}

// Only an instance may be defined by more than one unit: any other function, and any name, stays an error.
func TestLinkRefusesAFunctionThatIsNotAnInstanceAndTwoUnitsDefine(t *testing.T) {
	plain := &ast.FuncDecl{Name: "helper", LinkName: "helper"}
	for _, jobs := range []int{1, 4} {
		message := catchInternal(t, func() { linkToBytes(t, instanceUnits(plain, 1, 2), testLinkEntry, jobs) })
		if !strings.Contains(message, "defined twice") || !strings.Contains(message, "helper") {
			t.Errorf("jobs=%d: got %q, want an error naming the function that is defined twice", jobs, message)
		}
	}
}
