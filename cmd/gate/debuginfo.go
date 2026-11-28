package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The debug info step: debug info is written only with --debug-info and must leave the executable as it is. Programs are
// built for every target with and without it, and self for the host, with every span verified (--verify-ir), and the
// executables compared. The sidecar of self must be walkable, and on every target that of a program whose locals are in
// registers, in frame slots and in two words (a String) must say so, with its record's fields. A stack overflow's fault
// line, whose address is in a function of one statement, is then symbolized to that statement on every target the host
// runs. On x64 Windows a batch session of the debugger drives the locals program (debug --batch) and its events are held
// to sessionEvents, and a conversational session (debug, its commands as standard input) is held to conversationLines. The
// holdings program (debuginfo_holdings.go) is built for every target too, its dump held to holdingsExpected, and on x64
// Windows a batch session reads back the values that every holder keeps in every way the layout table has.
var debugInfoPrograms = []string{
	filepath.Join("tests", "run", "arithmetic.maxon"),
	filepath.Join("tests", "run", "array-methods.maxon"),
	filepath.Join("tests", "run", "generics.maxon"),
}

const overflowLine = "overflow.maxon:3:2  (in export::down)"

const localsSource = `// The program whose locals the debug info step reads: across holds more values over a call than the callee-saved
// registers, so some are in registers and some in frame slots; named holds a record and a String of two words.
type Pair
	export var left as Count
	export var name as String

	static function make(left Count, consumes name String) returns Pair
		return Pair{left: left, name: name}
	end 'make'
end 'Pair'

function across(seed Count) returns Count
	let a = try seed + 1 otherwise 0
	let b = try seed + 2 otherwise 0
	let c = try seed + 3 otherwise 0
	let d = try seed + 4 otherwise 0
	let e = try seed + 5 otherwise 0
	let f = try seed + 6 otherwise 0
	let g = try seed + 7 otherwise 0
	let h = try seed + 8 otherwise 0
	let i = try seed + 9 otherwise 0
	let j = try seed + 10 otherwise 0
	let k = try seed + 11 otherwise 0
	let m = try seed + 12 otherwise 0
	let n = try seed + 13 otherwise 0
	let o = try seed + 14 otherwise 0
	print("{a}\n")
	return try a + b + c + d + e + f + g + h + i + j + k + m + n + o otherwise 0
end 'across'

function named(text String, count Count) returns String
	let pair = Pair.make(count, name: text.clone())
	let greeting = "{pair.name}:{pair.left}"
	return greeting
end 'named'

function main() returns ExitCode
	print("{across(5)} {named("x", count: 4)}\n")
	return 0
end 'main'
`

// localsExpected is what the dump of the locals program must show: a local of across in a register and one in a frame slot,
// the second word of a String, and the fields of Pair.
var localsExpected = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^        a  \[0x[0-9a-f]+, 0x[0-9a-f]+\)  (r[a-z0-9]+|x[0-9]+)  :`),
	regexp.MustCompile(`(?m)^        [a-o]  \[0x[0-9a-f]+, 0x[0-9a-f]+\)  \[(rbp|x29)[-+]0x[0-9a-f]+\]  :`),
	regexp.MustCompile(`(?m)^        greeting  .* \(second word\)  : String`),
	regexp.MustCompile(`(?m)^        \+0x0008  name : String`),
}

// sessionCommands drive the batch session of the locals program: a breakpoint on a bare line and one on a function, the
// backtrace, a scalar local in a register and one in a frame slot, a step over a line, a String local, a record's fields, a
// step out of a frame and the run to the program's exit.
const sessionCommands = "--commands=break 20;break named;run;print a;next;print h;continue;locals;next;print pair.name;print pair.left;backtrace;finish;continue"

// sessionEvents are the events the batch session must answer, one per command that has one.
var sessionEvents = []*regexp.Regexp{
	regexp.MustCompile(`^\{"event":"breakpoint","action":"set","function":"export::across","file":"locals.maxon","line":20,`),
	regexp.MustCompile(`^\{"event":"breakpoint","action":"set","function":"export::named","file":"locals.maxon","line":32,`),
	regexp.MustCompile(`^\{"event":"stop","reason":"breakpoint","function":"export::across","file":"locals.maxon","line":20,.*"current":true.*"backtrace":\[\{"frame":0,"function":"export::across".*\{"frame":1,"function":"export::main"`),
	regexp.MustCompile(`^\{"event":"value","value":\{"name":"a","type":"[^"]*","kind":"IntRanged","display":"6"\}\}$`),
	regexp.MustCompile(`^\{"event":"stop","reason":"step","function":"export::across","file":"locals.maxon","line":21,`),
	regexp.MustCompile(`^\{"event":"value","value":\{"name":"h","type":"[^"]*","kind":"IntRanged","display":"13"\}\}$`),
	regexp.MustCompile(`^\{"event":"stop","reason":"breakpoint","function":"export::named","file":"locals.maxon","line":32,`),
	regexp.MustCompile(`^\{"event":"locals","function":"export::named","locals":\[\{"name":"text","type":"String","kind":"String","display":"\\"x\\" \(len=1\)"\}`),
	regexp.MustCompile(`^\{"event":"stop","reason":"step","function":"export::named","file":"locals.maxon","line":33,`),
	regexp.MustCompile(`^\{"event":"value","value":\{"name":"pair.name","type":"String","kind":"String","display":"\\"x\\" \(len=1\)"\}\}$`),
	regexp.MustCompile(`^\{"event":"value","value":\{"name":"pair.left","type":"Count","kind":"IntRanged","display":"4"\}\}$`),
	regexp.MustCompile(`^\{"event":"backtrace","frames":\[\{"frame":0,"function":"export::named".*\{"frame":1,"function":"export::main"`),
	regexp.MustCompile(`^\{"event":"stop","reason":"step","function":"export::main","file":"locals.maxon","line":38,`),
	regexp.MustCompile(`^\{"event":"exit","code":0\}$`),
}

// conversationInput is what the conversational session of the locals program reads as its commands: a breakpoint on a
// function, the run to it, its locals, a step over a line, a field, the backtrace and a word that is no command.
const conversationInput = "break named\nrun\nlocals\nnext\nprint pair.left\nbt\nbogus\n"

// conversationLines are what the conversational session must print, in maxon's text, for conversationInput.
var conversationLines = []*regexp.Regexp{
	regexp.MustCompile(`breakpoint set at locals\.maxon:32 in export::named \(1 site\(s\)\)`),
	regexp.MustCompile(`stopped \(breakpoint\) at locals\.maxon:32 in export::named`),
	regexp.MustCompile("(?m)^-> 32\t"),
	regexp.MustCompile(`locals of export::named:`),
	regexp.MustCompile(`(?m)^    text : String = "x" \(len=1\)`),
	regexp.MustCompile(`stopped \(step\) at locals\.maxon:33 in export::named`),
	regexp.MustCompile(`(?m)pair\.left : Count = 4\r?$`),
	regexp.MustCompile(`  #0 export::named at locals\.maxon:33`),
	regexp.MustCompile(`(?m)^  #1 export::main at locals\.maxon:`),
	regexp.MustCompile(`error: 'bogus' is not a debugger command`),
	regexp.MustCompile(`(?m)^the program exited with code 0`),
}

const overflowSource = `// The program whose stack overflow the debug info step symbolizes: every instruction of down is its one statement's.
function down(n Count) returns Count
	return try down(n + 1) + 1 otherwise 0
end 'down'

function main() returns ExitCode
	_ = down(0)
	return 0
end 'main'
`

var faultAddress = regexp.MustCompile(`rip=(0x[0-9a-f]+)`)

var walkedTypes = regexp.MustCompile(`(?m)^  types \([1-9]`)

// peDllCharacteristics is where an image's DLL characteristics are past its PE header, and peDynamicBase the flag that lets
// the loader move it.
const (
	peHeaderOffset       = 0x3C
	peDllCharacteristics = 24 + 70
	peDynamicBase        = 0x40
)

func debugInfo(h host, compiler string) error {
	root, err := scratchDir("minimaxon-debug-info-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	overflow := filepath.Join(root, "overflow.maxon")
	locals := filepath.Join(root, "locals.maxon")
	if err := os.WriteFile(overflow, []byte(overflowSource), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(locals, []byte(localsSource), 0o644); err != nil {
		return err
	}
	holdings := filepath.Join(root, "holdings.maxon")
	if err := os.WriteFile(holdings, []byte(holdingsSource), 0o644); err != nil {
		return err
	}
	// A Mach-O image is signed under its file's name, so both builds of a program have one.
	plainRoot := filepath.Join(root, "plain")
	if err := os.MkdirAll(plainRoot, 0o755); err != nil {
		return err
	}
	buildOne := func(source, out string, flags []string) ([]byte, error) {
		if text, err := output(cacheOffEnvironment(), "", compiler, append([]string{"build", source, "-o", out}, flags...)...); err != nil {
			return nil, fmt.Errorf("debug info: the build of %s %v failed: %s", source, flags, text)
		}
		return os.ReadFile(out)
	}
	same := func(source string, flags []string, name string) error {
		plain, err := buildOne(source, filepath.Join(plainRoot, name+".exe"), flags)
		if err != nil {
			return err
		}
		described, err := buildOne(source, filepath.Join(root, name+".exe"), append(append([]string{}, flags...), "--debug-info", "--verify-ir"))
		if err != nil {
			return err
		}
		if !bytes.Equal(plain, described) {
			return fmt.Errorf("debug info: --debug-info changes the executable of %s %v", source, flags)
		}
		if !isFile(filepath.Join(root, name+".exe.mxdbg")) {
			return fmt.Errorf("debug info: no sidecar beside the build of %s %v", source, flags)
		}
		return nil
	}
	if err := same(abs("self"), nil, "self"); err != nil {
		return err
	}
	walked, err := output(nil, "", compiler, "debug", "--dump-info", filepath.Join(root, "self.exe"), "functions", "types")
	if err != nil || !walkedTypes.MatchString(walked) {
		return fmt.Errorf("debug info: the sidecar of self cannot be walked: %.400s", walked)
	}
	for _, t := range targets {
		for _, p := range append([]string{overflow, locals, holdings}, debugInfoPrograms...) {
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) + "-" + t
			if err := same(p, []string{"--target=" + t}, name); err != nil {
				return err
			}
		}
		dump, _ := output(nil, "", compiler, "debug", "--dump-info", filepath.Join(root, "locals-"+t+".exe"), "functions", "types")
		for _, expected := range localsExpected {
			if !expected.MatchString(dump) {
				return fmt.Errorf("debug info: the locals of the program built for %s do not match %s", t, expected)
			}
		}
		held, _ := output(nil, "", compiler, "debug", "--dump-info", filepath.Join(root, "holdings-"+t+".exe"), "functions", "types")
		for _, expected := range holdingsExpected {
			if !expected.MatchString(held) {
				return fmt.Errorf("debug info: the holdings of the program built for %s do not match %s", t, expected)
			}
		}
	}
	if h.windows() {
		if err := debuggerSession(compiler, filepath.Join(root, "locals-x64-windows.exe")); err != nil {
			return err
		}
		if err := batchSteps(compiler, filepath.Join(root, "holdings-x64-windows.exe"), holdingsSteps()); err != nil {
			return err
		}
	} else {
		skip("debug info: the batch session of the debugger", "the debugger drives x64 Windows programs alone")
	}
	for _, t := range []string{"x64-windows", "x64-linux", "arm64-linux"} {
		exe := filepath.Join(root, "overflow-"+t+".exe")
		start, why, err := faultRunner(h, t, exe)
		if err != nil {
			return err
		}
		if why != "" {
			skip("debug info: the fault line of "+t, why)
			continue
		}
		fault, _ := start.CombinedOutput()
		m := faultAddress.FindSubmatch(fault)
		if m == nil {
			return fmt.Errorf("debug info: %s printed no fault line: %s", exe, fault)
		}
		named, err := output(nil, "", compiler, "debug", "--symbolize", exe, "rip="+string(m[1]))
		if err != nil || !strings.Contains(named, overflowLine) {
			return fmt.Errorf("debug info: rip=%s of %s symbolizes to %s, not %s", m[1], exe, named, overflowLine)
		}
	}
	return nil
}

// debuggerSession runs the batch session of the locals program on x64 Windows, then the conversational one. The batch's program output goes to stderr.
func debuggerSession(compiler, session string) error {
	if err := batchEvents(compiler, session, sessionCommands, sessionEvents); err != nil {
		return err
	}
	cmd := exec.Command(compiler, "debug", session)
	cmd.Stdin = strings.NewReader(conversationInput)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("debug info: the conversational session of %s fails (%v): %s", session, err, out)
	}
	for _, expected := range conversationLines {
		if !expected.Match(out) {
			return fmt.Errorf("debug info: the conversational session does not answer %s: %s", expected, out)
		}
	}
	return nil
}

// batchEvents runs the batch session of the program at session with commands, and holds each event it answers to the one expected
// of the command at its place.
func batchEvents(compiler, session, commands string, expectedEvents []*regexp.Regexp) error {
	out, err := exec.Command(compiler, "debug", "--batch", commands, session).Output()
	events := strings.Split(strings.TrimRight(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
	if err != nil {
		return fmt.Errorf("debug info: the batch session of %s fails (%v): %s", session, err, out)
	}
	for i, expected := range expectedEvents {
		if i >= len(events) || !expected.MatchString(events[i]) {
			return fmt.Errorf("debug info: event %d of the batch session of %s does not match %s: %s", i, session, expected, out)
		}
	}
	if len(events) != len(expectedEvents) {
		return fmt.Errorf("debug info: the batch session of %s answered %d events, not %d: %s", session, len(events), len(expectedEvents), out)
	}
	return nil
}

// faultRunner is the command that runs the overflowing program built for a target on the host, or why the host cannot run
// it. On Windows the loader keeps an image whose header asks for no move at its image base, where the sidecar places its
// code, so a copy of the executable without the flag is run, and Linux's run under WSL.
func faultRunner(h host, t, exe string) (*exec.Cmd, string, error) {
	switch {
	case h.windows() && t == "x64-windows":
		fixed := filepath.Join(filepath.Dir(exe), "fixed.exe")
		if err := pinnedImage(exe, fixed); err != nil {
			return nil, "", err
		}
		return exec.Command(fixed), "", nil
	case h.windows() && !h.hostOnly:
		if err := assertWsl(); err != nil {
			return nil, "", err
		}
		return exec.Command("wsl.exe", "--exec", wslPath(exe)), "", nil
	case t == h.target:
		return exec.Command(exe), "", nil
	}
	why, err := runsTarget(h, t)
	if err != nil || why != "" {
		return nil, why, err
	}
	return exec.Command(exe), "", nil
}

// pinnedImage writes a copy of the PE at src, and of its sidecar, without the flag that lets the loader move it.
func pinnedImage(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(data) < peHeaderOffset+4 {
		return errors.New("debug info: the x64 Windows executable has no PE header")
	}
	at := int(binary.LittleEndian.Uint32(data[peHeaderOffset:])) + peDllCharacteristics
	data[at] &^= peDynamicBase
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return err
	}
	return copyFile(src+".mxdbg", dst+".mxdbg")
}

// wslPath is the path in WSL of a Windows path with a drive, C:\dir\file as /mnt/c/dir/file.
func wslPath(path string) string {
	return "/mnt/" + strings.ToLower(path[:1]) + strings.ReplaceAll(path[2:], `\`, "/")
}
