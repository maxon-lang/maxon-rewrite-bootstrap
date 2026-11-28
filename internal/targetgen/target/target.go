// Package target holds the tables of the targets the backend writes code for: each processor's register file and the
// convention between the compiler's own functions, and each system's conventions on it. The x64 instruction table
// (internal/x64gen/x64table) takes its registers from here, and internal/targetgen writes the Maxon declarations the backend
// reads the conventions from.
package target

import (
	"fmt"
	"slices"
	"strings"
)

// Class is the register file a register belongs to.
type Class int

const (
	General Class = iota // integer and address registers
	Vector               // floating-point and vector registers
)

// Register is one register of a target's register file.
type Register struct {
	Name     string
	Encoding int // the number its instructions encode it by
	Class    Class
}

// Convention is how a call passes its arguments and results, and what it keeps.
type Convention struct {
	// Doc says what the convention is for, in the generated file.
	Doc string
	// Prefix starts the names of the convention's declarations, after the target's.
	Prefix string
	// Args are the registers of the integer arguments in order, and FloatArgs those of the float arguments: each class
	// counts its own arguments, so the third float is in the third of FloatArgs however many integers come before it.
	// The arguments past a class's registers go on the stack, all of them in one run in the order of the arguments. A
	// convention with no FloatArgs passes a float as an integer.
	Args, FloatArgs []string
	// Result is the register of an integer result, and FloatResult of a float one, or "" for a convention with no
	// FloatArgs.
	Result, FloatResult string
	// SecondResult is the register of the second word of a String result, whose first is in Result, or "" for a
	// convention that never gives one.
	SecondResult string
	// Error is the register of the error flag of a function that throws, or "" for a convention with none.
	Error string
	// CalleeSaved are the registers a call keeps, and CallerSaved the ones it may leave holding anything; together
	// they are the registers that are not reserved.
	CalleeSaved, CallerSaved []string
	// ShadowBytes is the stack the caller keeps below the stack arguments for the callee to write.
	ShadowBytes int
	// Number is the register of a system call's number, or "" for a convention of calls.
	Number string
	// VariadicOnStack says that a variadic function takes every variadic argument on the stack, whatever registers are
	// free, as Apple's arm64 convention passes them.
	VariadicOnStack bool
}

// ImageFormat is the format of a system's executables, which picks the backend's writer of the file.
type ImageFormat string

const (
	PE    ImageFormat = "pe"
	ELF   ImageFormat = "elf"
	MachO ImageFormat = "machO"
)

// Reach is how a system's programs reach the system: the one way the runtime's source for it may, and the backend accepts.
type Reach string

const (
	// Imports: through the functions of the system's libraries, which the executable imports.
	Imports Reach = "imports"
	// Syscalls: through system calls that the program makes itself, with no library between.
	Syscalls Reach = "syscalls"
)

// OS is an operating system as the whole compiler sees it, whatever processor it runs on: how a target's name and an
// `#if os(...)` spell it, the format of its executables, how its programs reach it, what an executable's name ends with,
// the separator of its paths, and the environment variable that names its directory of temporary files and the directory
// meant when that is unset or empty, as maxon reads them. ast.TargetSystem has a case for each.
type OS struct {
	// Name is the case of ast.TargetSystem.
	Name string
	// Display is the name of the system in a target's name, lowered, and in `#if os(...)`.
	Display          string
	Image            ImageFormat
	Reach            Reach
	ExecutableSuffix string
	PathSeparator    string
	TempVariable     string
	// TempDefault is the directory of temporary files when TempVariable is unset or empty, as maxon falls back to it, or
	// empty for a system that has none and must name one.
	TempDefault string
}

var (
	Windows = OS{Name: "windows", Display: "Windows", Image: PE, Reach: Imports, ExecutableSuffix: ".exe", PathSeparator: `\`, TempVariable: "TEMP"}
	Linux   = OS{Name: "linux", Display: "Linux", Image: ELF, Reach: Syscalls, PathSeparator: "/", TempVariable: "TMPDIR", TempDefault: "/tmp"}
	MacOS   = OS{Name: "macos", Display: "macOS", Image: MachO, Reach: Imports, PathSeparator: "/", TempVariable: "TMPDIR", TempDefault: "/tmp"}
)

// OSes are the systems, in the order of the cases of ast.TargetSystem.
var OSes = []OS{Windows, Linux, MacOS}

// Facility is a part of the library that some systems do not have yet, maxon's HostFacility of TargetFacilities.maxon: a
// module outside the library that names one of its Types in a compile for such a system is refused at the name (E3104),
// with Why. Lifting a system's refusal is taking it out of Missing.
type Facility struct {
	// Name is the facility's name in the generated code's comments.
	Name string
	// Types are the library's types that are the facility.
	Types []string
	// Missing are the systems that do not have it.
	Missing []OS
	// Why ends the diagnostic: what the system lacks.
	Why string
}

// Facilities are the parts of the library that some systems lack.
var Facilities = []Facility{
	// TLS is not brought over yet (README, "Where it differs from Maxon"): stdlib/tls declares the types the HTTP library
	// names, and maxon's sessions are named here too, so that a spec test that names one is skipped as unsupported.
	{Name: "TLS", Types: []string{
		"TlsError", "TlsClientOptions", "TlsServerOptions", "TlsProtocol", "TlsCipherSuite", "Tls13CipherSuite", "Tls12CipherSuite",
		"TlsApplicationProtocol", "TlsAlert", "TlsIdentity", "TlsClient", "TlsListener", "TlsStream",
	}, Missing: []OS{Windows, Linux, MacOS}, Why: "TLS is not brought over yet"},
}

// System is what an operating system adds to a processor's table: the convention of its libraries' functions, the one
// of its system calls where a program calls the kernel itself, and how a thread finds its context.
type System struct {
	OS OS
	// Host is the convention of the system's libraries.
	Host Convention
	// Syscall is the convention of a system call, or nil for a system whose programs reach the kernel only through its
	// libraries.
	Syscall *Convention
	// ThreadContext names how a thread reads the word of its context on this system: a case of the target's
	// <Name>ThreadContext enum.
	ThreadContext string
	// Pending says that the backend does not compile for the target on this system yet: the system's declarations are
	// written, and knownTargets does not list the target on it.
	Pending bool
}

// Name follows the target's in the names of the system's declarations.
func (s System) Name() string { return s.OS.Display }

// Target is one processor's register file and calling conventions, and those of the systems it runs.
type Target struct {
	// Name is the case of ast.TargetArch, and starts the name of every declaration the generator writes for the target.
	Name string
	// Registers is the register file in its numbering: the general registers by encoding, then the vector ones.
	Registers []Register
	// Reserved are the registers no value is given: those that hold the stack and the frame, and those the code or the
	// system keeps for itself.
	Reserved []string
	// StackAlignment is the alignment of the stack pointer at a call of a host convention.
	StackAlignment int
	// Internal is the convention between the compiler's own functions, the same on every system.
	Internal Convention
	// Systems are the systems the target runs, in the order their declarations are written.
	Systems []System
}

// Conventions are the target's conventions, the internal one first, each with the prefix of its declarations' names.
func (t Target) Conventions() []NamedConvention {
	out := []NamedConvention{{t.Name + t.Internal.Prefix, strings.ToLower(t.Internal.Prefix), t.Internal}}
	for _, s := range t.Systems {
		out = append(out, NamedConvention{t.Name + s.Name() + s.Host.Prefix, s.Name() + " " + strings.ToLower(s.Host.Prefix), s.Host})
		if s.Syscall != nil {
			out = append(out, NamedConvention{t.Name + s.Name() + s.Syscall.Prefix, s.Name() + " " + strings.ToLower(s.Syscall.Prefix), *s.Syscall})
		}
	}
	return out
}

// NamedConvention is a convention, the prefix of its declarations' names, and what the generated file calls it.
type NamedConvention struct {
	Prefix     string
	Title      string
	Convention Convention
}

// Names are the names of a target's registers of a class, in the order of the register file.
func (t Target) Names(c Class) []string {
	var names []string
	for _, r := range t.Registers {
		if r.Class == c {
			names = append(names, r.Name)
		}
	}
	return names
}

// Number is a register's number in the register file.
func (t Target) Number(name string) (int, error) {
	for i, r := range t.Registers {
		if r.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%s: %s is no register", t.Name, name)
}

// Mask is the set of the named registers, a bit per number in the register file.
func (t Target) Mask(names []string) (uint64, error) {
	var m uint64
	for _, n := range names {
		i, err := t.Number(n)
		if err != nil {
			return 0, err
		}
		m |= 1 << i
	}
	return m, nil
}

// Check refuses a table that contradicts itself: a register file out of its numbering, a convention whose saved
// registers do not split the registers that are not reserved, or whose argument, result or error registers are not
// registers it may leave holding anything.
func (t Target) Check() error {
	for c, names := range [][]string{t.Names(General), t.Names(Vector)} {
		for i, n := range names {
			r := t.Registers[slices.IndexFunc(t.Registers, func(r Register) bool { return r.Name == n })]
			if r.Encoding != i {
				return fmt.Errorf("%s: %s of class %d is encoded %d but is register %d of its class", t.Name, n, c, r.Encoding, i)
			}
		}
	}
	if len(t.Names(General))+len(t.Names(Vector)) != len(t.Registers) {
		return fmt.Errorf("%s: a register of no class", t.Name)
	}
	for i, r := range t.Registers {
		if i > 0 && t.Registers[i-1].Class > r.Class {
			return fmt.Errorf("%s: %s comes after a register of a later class", t.Name, r.Name)
		}
	}
	if len(t.Registers) > 64 {
		return fmt.Errorf("%s: a register file past the 64 bits of a mask", t.Name)
	}
	for _, s := range t.Systems {
		if !slices.Contains(OSes, s.OS) {
			return fmt.Errorf("%s: %s is no system of OSes", t.Name, s.Name())
		}
		if (s.OS.Reach == Syscalls) != (s.Syscall != nil) {
			return fmt.Errorf("%s on %s: a system has a system call convention exactly when its programs reach it by system calls", t.Name, s.Name())
		}
	}
	reserved, err := t.Mask(t.Reserved)
	if err != nil {
		return err
	}
	all := uint64(1)<<len(t.Registers) - 1
	for _, named := range t.Conventions() {
		c := named.Convention
		c.Prefix = named.Prefix
		kept, err := t.Mask(c.CalleeSaved)
		if err != nil {
			return err
		}
		lost, err := t.Mask(c.CallerSaved)
		if err != nil {
			return err
		}
		if kept&lost != 0 || kept&reserved != 0 || lost&reserved != 0 || kept|lost|reserved != all {
			return fmt.Errorf("%s %s: the callee-saved, caller-saved and reserved registers do not split the register file", t.Name, c.Prefix)
		}
		if c.Number != "" {
			m, err := t.Mask([]string{c.Number})
			if err != nil {
				return err
			}
			if m&lost == 0 || slices.Contains(c.Args, c.Number) || len(c.FloatArgs) > 0 {
				return fmt.Errorf("%s %s: a system call's number is in a register it may lose that passes no argument, and it passes no float", t.Name, c.Prefix)
			}
		}
		if (len(c.FloatArgs) == 0) != (c.FloatResult == "") {
			return fmt.Errorf("%s %s: a convention has float arguments exactly when it has a float result", t.Name, c.Prefix)
		}
		for _, n := range append(slices.Clone(c.FloatArgs), c.FloatResult) {
			if n == "" {
				continue
			}
			i, err := t.Number(n)
			if err != nil {
				return err
			}
			if t.Registers[i].Class != Vector {
				return fmt.Errorf("%s %s: %s passes a float but is no vector register", t.Name, c.Prefix, n)
			}
		}
		moved := slices.Clone(c.Args)
		moved = append(moved, c.FloatArgs...)
		moved = append(moved, c.Result)
		if c.FloatResult != "" {
			moved = append(moved, c.FloatResult)
		}
		if c.Error != "" {
			moved = append(moved, c.Error)
		}
		if c.SecondResult != "" {
			moved = append(moved, c.SecondResult)
		}
		for _, n := range moved {
			m, err := t.Mask([]string{n})
			if err != nil {
				return err
			}
			if m&lost == 0 {
				return fmt.Errorf("%s %s: %s passes a value but is not caller-saved", t.Name, c.Prefix, n)
			}
		}
		if c.SecondResult != "" && (c.SecondResult == c.Result || c.SecondResult == c.Error) {
			return fmt.Errorf("%s %s: the second word of a String result shares a register with the result or the error flag", t.Name, c.Prefix)
		}
		if c.Error != "" && (c.Error == c.Result || slices.Contains(c.Args, c.Error)) {
			return fmt.Errorf("%s %s: the error flag shares a register with a value", t.Name, c.Prefix)
		}
	}
	return nil
}

func regs(class Class, names ...string) []Register {
	out := make([]Register, len(names))
	for i, n := range names {
		out[i] = Register{Name: n, Encoding: i, Class: class}
	}
	return out
}

// X64 is x64 on Windows and on Linux. The internal convention is maxon's, the same on every x64 system; Windows' host
// convention is Win64, and Linux's System V, whose kernel a program calls itself, with no library between.
var X64 = Target{
	Name: "x64",
	Registers: append(
		regs(General, "rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"),
		regs(Vector, "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15")...),
	Reserved:       []string{"rsp", "rbp"},
	StackAlignment: 16,
	Internal: Convention{
		Doc:          "the convention between the compiler's own functions, maxon's",
		Prefix:       "Internal",
		Args:         []string{"rcx", "rdx", "rax", "r9", "rsi", "rdi"},
		FloatArgs:    []string{"xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5"},
		Result:       "r8",
		SecondResult: "r9",
		FloatResult:  "xmm0",
		Error:        "r10",
		CalleeSaved:  []string{"rbx", "r12", "r13", "r14", "r15", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15"},
		CallerSaved:  []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11", "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5"},
	},
	Systems: []System{
		{
			OS: Windows,
			// A thread's context is in a TLS slot of its TEB, which gs points at, at the index the entry stub keeps in
			// rt.tlsOffset.
			ThreadContext: "tlsSlot",
			// No function of a DLL that the runtime imports takes or returns a float, so the host convention passes only
			// words.
			Host: Convention{
				Doc:         "Win64, the convention of a DLL's functions",
				Prefix:      "Host",
				Args:        []string{"rcx", "rdx", "r8", "r9"},
				Result:      "rax",
				CalleeSaved: []string{"rbx", "rsi", "rdi", "r12", "r13", "r14", "r15", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15"},
				CallerSaved: []string{"rax", "rcx", "rdx", "r8", "r9", "r10", "r11", "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5"},
				ShadowBytes: 32,
			},
		},
		{
			OS: Linux,
			// A thread's context is the word at fs:0, where the entry stub put it.
			ThreadContext: "fsWord",
			Host: Convention{
				Doc:         "System V, the convention of a shared library's functions",
				Prefix:      "Host",
				Args:        []string{"rdi", "rsi", "rdx", "rcx", "r8", "r9"},
				FloatArgs:   []string{"xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7"},
				Result:      "rax",
				FloatResult: "xmm0",
				CalleeSaved: []string{"rbx", "r12", "r13", "r14", "r15"},
				CallerSaved: []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11", "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15"},
			},
			// The kernel keeps every register but rcx and r11, which syscall overwrites, and rax, the result. The table
			// has the call lose its argument registers too, as a call of every other convention does, so that the
			// allocator treats a system call as it does any call.
			Syscall: &Convention{
				Doc:         "a system call: the number in rax and up to six words, the result in rax, or -errno",
				Prefix:      "Syscall",
				Args:        []string{"rdi", "rsi", "rdx", "r10", "r8", "r9"},
				Result:      "rax",
				Number:      "rax",
				CalleeSaved: []string{"rbx", "r12", "r13", "r14", "r15", "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15"},
				CallerSaved: []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11"},
			},
		},
	},
}

// Arm64 is AArch64 on Linux and on macOS, with maxon's register roles (Arm64Backend.maxon) and internal convention
// (StdToArm64Conversion.maxon). x15 is the register a stopped green thread resumes through, x16 and x17 the scratch
// registers of the encoder's immediates, imports and jump tables, x18 Darwin's platform register, which a program
// never writes on either system so that one code generator serves both, x29 the frame pointer and x30 the link
// register. sp is the 31 of an operand that names the stack, which is the zero register where an operand names no
// stack. The host conventions are AAPCS64: Apple's passes every variadic argument on the stack, and Linux's kernel a
// program calls itself.
var Arm64 = Target{
	Name: "arm64",
	Registers: append(
		regs(General, "x0", "x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "x9", "x10", "x11", "x12", "x13", "x14", "x15",
			"x16", "x17", "x18", "x19", "x20", "x21", "x22", "x23", "x24", "x25", "x26", "x27", "x28", "x29", "x30", "sp"),
		regs(Vector, "d0", "d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8", "d9", "d10", "d11", "d12", "d13", "d14", "d15",
			"d16", "d17", "d18", "d19", "d20", "d21", "d22", "d23", "d24", "d25", "d26", "d27", "d28", "d29", "d30", "d31")...),
	Reserved:       []string{"x15", "x16", "x17", "x18", "x29", "x30", "sp"},
	StackAlignment: 16,
	Internal: Convention{
		Doc:          "the convention between the compiler's own functions, maxon's",
		Prefix:       "Internal",
		Args:         arm64Args,
		FloatArgs:    arm64FloatArgs,
		Result:       "x0",
		SecondResult: "x1",
		FloatResult:  "d0",
		Error:        "x9",
		CalleeSaved:  arm64CalleeSaved,
		CallerSaved:  arm64CallerSaved,
	},
	Systems: []System{
		{
			OS: Linux,
			// A thread's context is the value of tpidr_el0, which the entry stub sets.
			ThreadContext: "threadRegister",
			Host: Convention{
				Doc:         "AAPCS64, the convention of a shared library's functions",
				Prefix:      "Host",
				Args:        arm64Args,
				FloatArgs:   arm64FloatArgs,
				Result:      "x0",
				FloatResult: "d0",
				CalleeSaved: arm64CalleeSaved,
				CallerSaved: arm64CallerSaved,
			},
			// The kernel keeps every register but x0, the result. The table has the call lose its argument registers and
			// the number too, as a call of every other convention loses its own, so that the allocator treats a system
			// call as it does any call.
			Syscall: &Convention{
				Doc:    "a system call: the number in x8 and up to six words, the result in x0, or -errno",
				Prefix: "Syscall",
				Args:   []string{"x0", "x1", "x2", "x3", "x4", "x5"},
				Result: "x0",
				Number: "x8",
				CalleeSaved: []string{"x6", "x7", "x9", "x10", "x11", "x12", "x13", "x14", "x19", "x20", "x21", "x22", "x23", "x24", "x25", "x26", "x27", "x28",
					"d0", "d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8", "d9", "d10", "d11", "d12", "d13", "d14", "d15",
					"d16", "d17", "d18", "d19", "d20", "d21", "d22", "d23", "d24", "d25", "d26", "d27", "d28", "d29", "d30", "d31"},
				CallerSaved: []string{"x0", "x1", "x2", "x3", "x4", "x5", "x8"},
			},
		},
		{
			OS: MacOS,
			// A thread's context is in a slot of the thread's TSD array, which tpidrro_el0 points at, at the key that the
			// entry stub keeps, as Windows' is in a TLS slot of the TEB.
			ThreadContext: "tlsSlot",
			Host: Convention{
				Doc:             "Apple's AAPCS64, the convention of libSystem's functions, which passes every variadic argument on the stack",
				Prefix:          "Host",
				Args:            arm64Args,
				FloatArgs:       arm64FloatArgs,
				Result:          "x0",
				FloatResult:     "d0",
				CalleeSaved:     arm64CalleeSaved,
				CallerSaved:     arm64CallerSaved,
				VariadicOnStack: true,
			},
		},
	},
}

// The registers of AAPCS64, which maxon's internal convention takes too: eight of each class for arguments, and the
// callee keeps x19 to x28 and the low halves of v8 to v15, which are d8 to d15.
var (
	arm64Args        = []string{"x0", "x1", "x2", "x3", "x4", "x5", "x6", "x7"}
	arm64FloatArgs   = []string{"d0", "d1", "d2", "d3", "d4", "d5", "d6", "d7"}
	arm64CalleeSaved = []string{"x19", "x20", "x21", "x22", "x23", "x24", "x25", "x26", "x27", "x28",
		"d8", "d9", "d10", "d11", "d12", "d13", "d14", "d15"}
	arm64CallerSaved = []string{"x0", "x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "x9", "x10", "x11", "x12", "x13", "x14",
		"d0", "d1", "d2", "d3", "d4", "d5", "d6", "d7",
		"d16", "d17", "d18", "d19", "d20", "d21", "d22", "d23", "d24", "d25", "d26", "d27", "d28", "d29", "d30", "d31"}
)

// Targets are every target's table that the backend compiles for, in the order their declarations are written.
var Targets = []Target{X64, Arm64}

// Pending are the tables of the targets the backend does not compile for yet, whose instruction selection is not
// written: their declarations are written beside the others', and no architecture of the target model names them.
var Pending []Target
