package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// targets are every target the compiler compiles for, in the order the gate checks them.
var targets = []string{"x64-windows", "x64-linux", "arm64-linux", "arm64-macos"}

// behaviourWorkers are the worker counts that the programs that schedule behaviours run under again after the corpus has
// run with the machine's: a bug of the runtime's memory ordering showed only at four workers, so the counts reach past two.
const behaviourWorkers = "1,2,4"

// hashSeedPin is the hash seed of every program a test runner starts, as maxon's harness hands the programs of a run its
// MAXON_HASH_SEED: a program whose output follows a hash, such as a map's order, then prints the same natively, under the
// interpreter and on every run. A seed already in the environment is kept, so a run under another one is a matter of
// setting it.
const hashSeedPin = "1111111111111111"

// proofSeeds is how many programs of the proof generator the gate runs, which is what fits in half a minute and is far more
// than the few that find a hole in a proof the generator is aimed at (the closure that changes an index, a count effect).
const proofSeeds = "180"

// serviceGraphs is how many of the services lane's 6,898 send graphs the gate runs, spread evenly over them, which takes
// seconds where every one takes four minutes (`fuzz --services` alone).
const serviceGraphs = "200"

// concurrentSeeds is how many programs of the concurrency generator the gate runs, each under six schedules and three times
// natively, which takes seconds where the wide run (uzz --concurrent --native --count=2000) takes minutes.
const concurrentSeeds = "40"

// pinHashSeed sets the hash seed of the programs the gate starts, for the suites and for the regeneration of the specs alike:
// the spec results are kept under a stamp that holds the seed, so a run under another one is not the run that the gate
// replays.
func pinHashSeed() {
	if os.Getenv("MAXON_HASH_SEED") == "" {
		os.Setenv("MAXON_HASH_SEED", hashSeedPin)
	}
	// WSL passes on only the variables that WSLENV names.
	passToWsl("MAXON_HASH_SEED")
}

// recordSpecsFlag, with maxon's specs directory after it, has a run convert the specs from maxon's (as `gate specs` does)
// and record the list of the tests that pass in the same run that requires it: `gate check --record-specs=<dir>`. It is the
// one run of the spec tests that a landing needs.
const recordSpecsFlag = "--record-specs="

// concurrentVariable, set to anything, has the gate run its lanes at once (--concurrent for suites).
const concurrentVariable = "MINIMAXON_GATE_CONCURRENT"

// laneTimesVariable names the file a lane writes its steps' times to, for the process that started it.
const laneTimesVariable = "MINIMAXON_LANE_TIMES"

// behavioursExecutables is how many executables the runner of a behaviours lane runs at once when the lanes run at once,
// where the default (half the workers, at most 8) would run the three lanes' as many.
const behavioursExecutables = "2"

// suiteRun is one run of the suites: the compiler, the arguments for `minimaxon test`, and the lane this process runs, or
// "" for all of them.
type suiteRun struct {
	h        host
	compiler string
	testArgs []string
	lane     string
	// recordSpecs is maxon's specs directory when the run converts the specs and records the list of the tests that pass
	// (recordSpecsFlag), and empty for a run that only requires the list.
	recordSpecs string
}

// laneStep is a step and the lane it runs in.
type laneStep struct {
	step
	lane string
}

// suites runs every suite that holds a compiler to the repository's rules: the Go tests of the generators and tools, then
// the compiler's own. The gate runs it on the compiler it builds from the seed, and the seed renewal on the compiler that
// would become a seed, so the two cannot disagree about what a compiler must pass. args are `--lane=<name>` to run one
// lane (how the lanes are started), `--concurrent` to run the lanes at once, and then the arguments for `minimaxon test`.
//
// The steps are in lanes that share nothing but the compiler and the checkout: `prelude` is what the others need and runs
// first, `checks` the compiler's own suites and the generated programs, `builds` what is compiled and not run here, `specs`
// the spec tests, and a lane for each target whose corpus is run. Within a lane the order is the order here. The lanes are
// in two phases, the second beginning when the first has ended: the reruns of the programs that schedule behaviours under
// one worker are the one thing here that a loaded machine fails (a timer that fires late reorders what such a program
// prints), so they run when the other lanes are done. By default every step runs in this process, lane by lane, and the
// first that fails stops the run; run at once, each lane is a process of its own, and the first to fail ends the others. That
// is not the default because tests with deadlines (the arm64 corpus under qemu, a program of timers on one worker) failed
// now and then when the lanes and other work shared the machine.
func suites(h host, compilerPath string, args []string) error {
	run := suiteRun{h: h, compiler: abs(compilerPath)}
	concurrent := os.Getenv(concurrentVariable) != ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--lane="):
			run.lane = strings.TrimPrefix(a, "--lane=")
		case strings.HasPrefix(a, recordSpecsFlag):
			run.recordSpecs = strings.TrimPrefix(a, recordSpecsFlag)
		case a == "--concurrent":
			concurrent = true
		default:
			run.testArgs = append(run.testArgs, a)
		}
	}
	pinHashSeed()
	steps, phases := run.steps()
	if h.lanes != nil && run.lane == "" {
		fmt.Printf("%s runs the lanes %s only: %s\n", h.target, strings.Join(h.lanes, ", "), h.lanesWhy)
		for _, s := range steps {
			if s.lane != "prelude" && !slices.Contains(h.lanes, s.lane) {
				skip(s.name, h.lanesWhy)
			}
		}
	}
	if run.lane != "" {
		err := runLaneSteps(steps, []string{run.lane})
		if path := os.Getenv(laneTimesVariable); path != "" {
			if werr := writeStepTimes(path); werr != nil && err == nil {
				err = werr
			}
		}
		return err
	}
	if err := runLaneSteps(steps, []string{"prelude"}); err != nil {
		return err
	}
	if concurrent {
		return run.lanesAtOnce(phases)
	}
	return runLaneSteps(steps, slices.Concat(phases...))
}

// steps are the suites' steps, each in its lane, and the lanes of each phase.
func (r suiteRun) steps() ([]laneStep, [][]string) {
	mm := func(args ...string) error { return quiet(nil, r.compiler, args...) }
	steps := []laneStep{
		{step{"envrun", buildEnvRunner}, "prelude"},
		// cmd/convert lays out what it converts with the compiler's fmt.
		{step{"Go tests", func() error { return quiet([]string{"MINIMAXON=" + r.compiler}, "go", "test", "./...") }}, "checks"},
		{step{"selftest", func() error { return mm("selftest") }}, "checks"},
		// Nothing compiles the runtime's source until a program is built, so this is what holds it to the language and to the
		// signatures the compiler calls, for each target, whose branches of the source differ.
		{step{"runtime", func() error { return eachTarget(func(t string) error { return mm("check", "runtime", "--target="+t) }) }}, "checks"},
		// A program compiles only the library modules it names, so this is what holds the whole standard library to the
		// language, for each target.
		{step{"stdlib", func() error { return eachTarget(func(t string) error { return mm("check", "stdlib", "--target="+t) }) }}, "checks"},
		// The compiler's own source is a program too: a rule that it breaks would otherwise show only when the seed is renewed.
		{step{"self", func() error { return mm("check", "self", "--tests", "--verify-reads") }}, "checks"},
		{step{"TMPDIR unset", func() error { return compilesWithoutTmpdir(r.h, r.compiler) }}, "checks"},
		{step{"unwritable output", func() error { return refusesUnwritableOutput(r.compiler) }}, "checks"},
		{step{"layout", func() error { return mm("fmt", "--check", "self", "runtime", "tests", "stdlib") }}, "checks"},
		// Every program the generator writes that the checker accepts must run clean under the interpreter: a violation is a
		// hole in the checker.
		{step{"fuzz", func() error { return mm("fuzz") }}, "checks"},
		// The proof generator's programs aim at the range analysis's proofs of access and arithmetic: an access the analysis proved
		// that lies outside its array is a violation under the interpreter, and the executable must print what the interpreter does.
		{step{"fuzz proofs", func() error { return mm("fuzz", "--proofs", "--native", "--count="+proofSeeds) }}, "checks"},
		// Every run of the services lane's send graphs must keep the interpreter's oracles of backpressure.
		{step{"fuzz services", func() error { return mm("fuzz", "--services", "--count="+serviceGraphs) }}, "checks"},
		// The concurrency generator's programs, parallel loops, async lends and services, are accepted by construction and give
		// one output under every schedule: under the race oracle and the oracles of backpressure, under six schedules of the
		// interpreter and natively, the outputs must agree.
		{step{"fuzz concurrent", func() error { return mm("fuzz", "--concurrent", "--native", "--count="+concurrentSeeds) }}, "checks"},
		{step{"benchmarks compile", func() error { return benchmarksCompile(r.compiler, r.testArgs) }}, "builds"},
		{step{"self compiles for arm64", func() error { return selfCompilesForArm64(r.compiler) }}, "builds"},
		{step{"cache", func() error { return cacheTest(r.h, r.compiler) }}, "builds"},
		{step{"debug info", func() error { return debugInfo(r.h, r.compiler) }}, "builds"},
		{step{"specs", func() error { return specsRequired(r.compiler, r.recordSpecs != "") }}, "specs"},
		// Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: run gives none.
		{step{"corpus", func() error {
			return quiet(nil, r.compiler, slices.Concat([]string{"test"}, verifyFlags(r.h.target), r.testArgs)...)
		}}, "native"},
		{step{"behaviours", func() error { return r.behaviours() }}, "native-behaviours"},
	}
	// What is never run here is compiled here: a compile that panics or fails for a target only another host runs would
	// otherwise show only on that host.
	for _, t := range foreignTargets(r.h) {
		steps = append(steps,
			laneStep{step{"compiles for " + t, func() error { return compilesFor(r.h, r.compiler, t, r.testArgs) }}, "builds"},
			laneStep{step{"self tests compile for " + t, func() error { return selfTestsCompileFor(r.compiler, t) }}, "builds"})
	}
	// The conversion writes specs/, which the other lanes read: it runs before any of them.
	if r.recordSpecs != "" {
		steps = append(steps, laneStep{step{"specs conversion", func() error { return convertSpecsIfStale(r.compiler, r.recordSpecs, false) }}, "prelude"})
	}
	first := []string{"checks", "builds", "specs", "native"}
	second := []string{"native-behaviours"}
	for _, t := range targets {
		if t == r.h.target || t == "arm64-macos" {
			continue
		}
		steps = append(steps,
			laneStep{step{"selftest on " + t, func() error { return r.otherSelftest(t) }}, t},
			laneStep{step{"corpus on " + t, func() error { return r.otherCorpus(t) }}, t},
			laneStep{step{"behaviours on " + t, func() error { return r.otherBehaviours(t) }}, t + "-behaviours"})
		first = append(first, t)
		second = append(second, t+"-behaviours")
	}
	// On the Mac the corpus of arm64-macos is the native lanes'.
	if !r.h.macos() {
		steps = append(steps, laneStep{step{"corpus on arm64-macos", func() error { return macLane(r.h, r.compiler, r.behavioursArgs(), r.testArgs) }}, "mac"})
		first = append(first, "mac")
	}
	if r.h.lanes != nil {
		keep := func(lanes []string) []string {
			return slices.DeleteFunc(lanes, func(l string) bool { return !slices.Contains(r.h.lanes, l) })
		}
		first, second = keep(first), keep(second)
	}
	return steps, [][]string{first, second}
}

// runLaneSteps runs the steps of the lanes, in order, in this process, each timed, and stops at the first that fails.
func runLaneSteps(steps []laneStep, lanes []string) error {
	for _, s := range steps {
		if !slices.Contains(lanes, s.lane) {
			continue
		}
		if err := timed(s.name, s.run); err != nil {
			return fmt.Errorf("%v\ncheck failed at: %s", err, s.name)
		}
	}
	return nil
}

// lanesAtOnce runs the lanes of each phase at once, each as a process of its own, the phases one after another. A lane that
// fails ends the others, so the run does not go on after it is known to have failed; the output of each lane that ran is
// then printed in the order of the lanes, and the times of their steps join this process's table.
func (r suiteRun) lanesAtOnce(phases [][]string) error {
	root, err := scratchDir("minimaxon-lanes-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	type lane struct {
		name string
		cmd  *exec.Cmd
		out  *os.File
	}
	var started []lane
	var failure error
	for _, phase := range phases {
		if failure != nil {
			break
		}
		ended := make(chan int, len(phase))
		var running []lane
		for _, name := range phase {
			out, err := os.Create(filepath.Join(root, name+".out"))
			if err != nil {
				return err
			}
			args := append([]string{"suites", r.compiler, "--lane=" + name}, r.testArgs...)
			if r.h.hostOnly {
				args = append(args, hostOnlyFlag)
			}
			if r.recordSpecs != "" {
				args = append(args, recordSpecsFlag+r.recordSpecs)
			}
			cmd := exec.Command(self, args...)
			cmd.Stdout, cmd.Stderr = out, out
			cmd.Env = append(os.Environ(), laneTimesVariable+"="+filepath.Join(root, name+".times"), concurrentVariable+"=1")
			if err := startGroup(cmd); err != nil {
				return err
			}
			running = append(running, lane{name, cmd, out})
		}
		for i, l := range running {
			go func() {
				_ = l.cmd.Wait()
				ended <- i
			}()
		}
		for range running {
			i := <-ended
			l := running[i]
			ending := "passed"
			if !l.cmd.ProcessState.Success() {
				ending = "failed"
				if failure != nil {
					ending = "stopped, as another lane failed"
				} else {
					failure = fmt.Errorf("lane %s failed", l.name)
					for _, other := range running {
						stopGroup(other.cmd)
					}
				}
			}
			fmt.Printf("[%s] lane %s %s\n", formatSeconds(sinceRun()), l.name, ending)
		}
		started = append(started, running...)
	}
	for _, l := range started {
		l.out.Close()
		if text, err := os.ReadFile(l.out.Name()); err == nil && len(bytesTrim(text)) > 0 {
			fmt.Println(string(bytesTrim(text)))
		}
		readStepTimes(filepath.Join(root, l.name+".times"))
	}
	return failure
}

// behavioursArgs are what a run of the programs that schedule behaviours adds: they are compiled once for all of
// behaviourWorkers and each executable is run under each count in turn, cmd/envrun giving it its count for itself alone (and
// having WSL pass it on), where the runner's environment, which its executables inherit, would also narrow the runner and
// compile everything again for each count. A lane run at once with the others runs fewer executables at a time.
func (r suiteRun) behavioursArgs() []string {
	args := []string{"--worker-counts=" + behaviourWorkers, "--env-runner=" + abs(envRunnerPath())}
	if os.Getenv(concurrentVariable) != "" && r.lane != "" {
		args = append(args, "--executables="+behavioursExecutables)
	}
	return args
}

func (r suiteRun) behaviours(targetArgs ...string) error {
	args := slices.Concat([]string{"test", "--behaviours"}, targetArgs, r.behavioursArgs(), r.testArgs)
	return quiet(nil, r.compiler, args...)
}

// envRunnerPath is where the gate builds cmd/envrun.
func envRunnerPath() string {
	exe := ""
	if os.PathSeparator == '\\' {
		exe = ".exe"
	}
	return filepath.Join("bin", "envrun"+exe)
}

func buildEnvRunner() error {
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	return quiet(nil, "go", "build", "-o", envRunnerPath(), "./cmd/envrun")
}

func eachTarget(f func(t string) error) error {
	for _, t := range targets {
		if err := f(t); err != nil {
			return err
		}
	}
	return nil
}

// passToWsl names a variable in WSLENV, so that the Linux programs WSL starts get it.
func passToWsl(name string) {
	os.Setenv("WSLENV", wslVariables(os.Getenv("WSLENV"), name))
}

// wslVariables is the value of WSLENV that names the variables of have and name.
func wslVariables(have, name string) string {
	if slices.Contains(strings.Split(have, ":"), name) {
		return have
	}
	if have == "" {
		return name
	}
	return have + ":" + name
}

// runsTarget is why the host cannot run the executables of a target, or "" when it can: Windows runs Linux's under WSL,
// in its default distribution, an x64 one natively and an arm64 one through the qemu-aarch64 that the distribution
// registers with binfmt_misc; x64 Linux runs an arm64 one through the qemu-aarch64 it registers itself, and arm64 Linux an
// x64 one not at all; macOS runs only its own.
// A Windows machine without WSL or a distribution fails the step instead: the Windows gate has always run Linux's code, and
// does not skip it.
func runsTarget(h host, t string) (string, error) {
	switch {
	case h.hostOnly:
		return hostOnlyWhy, nil
	case h.windows() && strings.HasSuffix(t, "-linux"):
		return "", assertWsl()
	case !h.windows() && t == "x64-windows":
		return "Windows executables run only on Windows", nil
	case h.armLinux() && t == "x64-linux":
		return "an arm64 Linux host has no emulator for x64 executables, and x64 Linux's gate runs that corpus natively", nil
	case h.macos() && strings.HasSuffix(t, "-linux"):
		return "Linux executables run on Linux or under WSL, and macOS has no way to run them", nil
	case h.linux() && t == "arm64-linux":
		if !isFile(qemuAarch64) {
			return "no qemu-aarch64 is registered with binfmt_misc (" + qemuAarch64 + "): install qemu-user-static, or qemu-user and binfmt-support, to run arm64-linux", nil
		}
		return "", nil
	}
	return "", fmt.Errorf("gate: %s has no way to run %s executables", h.target, t)
}

const qemuAarch64 = "/proc/sys/fs/binfmt_misc/qemu-aarch64"

func assertWsl() error {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return errors.New("the Linux corpus runs under WSL, and wsl.exe is not installed: install WSL (wsl --install)")
	}
	if _, err := output(nil, "", "wsl.exe", "--exec", "/bin/true"); err != nil {
		return errors.New("the Linux corpus runs under WSL, and its default distribution cannot run a program: install one (wsl --install -d Ubuntu) and start it once")
	}
	return nil
}

// corpusFlags are what the corpus of another target than the host's adds.
func corpusFlags(t string) []string {
	return append([]string{"--target=" + t}, verifyFlags(t)...)
}

// verifyFlags are what the corpus of a target adds, the host's included: every program for arm64 is compiled with
// --verify-ir, which holds arm64's selection and allocation to the invariants of every function.
func verifyFlags(t string) []string {
	if strings.HasPrefix(t, "arm64-") {
		return []string{"--verify-ir"}
	}
	return nil
}

func (r suiteRun) otherCorpus(t string) error {
	why, err := runsTarget(r.h, t)
	if err != nil || why != "" {
		if why != "" {
			skip("corpus on "+t, why)
		}
		return err
	}
	return quiet(nil, r.compiler, slices.Concat([]string{"test"}, corpusFlags(t), r.testArgs)...)
}

// otherSelftest runs the compiler's own tests with the compiler built for another target that the host runs. Its tests
// build programs for the target the compiler runs on and run them, so the host's selftest holds only the host's target to
// them: codegen.maxtest's raw write once called arm64 Linux's kernel by x64's number for write, and no lane ran it there.
// The compiler goes under bin/, which WSL reaches from the repository's root as wslCommand runs it. An arm64 compiler's
// tests are not run under qemu-user, as the arm64 corpus is: there they ran 43 minutes without ending, so they run only on
// an arm64 host.
func (r suiteRun) otherSelftest(t string) error {
	why, err := selftestRunsTarget(r.h, t)
	if err != nil || why != "" {
		if why != "" {
			skip("selftest on "+t, why)
		}
		return err
	}
	compiler := filepath.Join("bin", "selftest-"+t, "minimaxon")
	if err := os.MkdirAll(filepath.Dir(compiler), 0o755); err != nil {
		return err
	}
	if err := quiet(nil, r.compiler, "build", "self", "--target="+t, "-o", compiler); err != nil {
		return err
	}
	if r.h.windows() {
		name, args := wslCommand(compiler, "selftest")
		return quiet(nil, name, args...)
	}
	return quiet(nil, abs(compiler), "selftest")
}

// selftestRunsTarget is why the host cannot run the compiler built for a target, or "" when it can: as runsTarget, but
// never through an emulator.
func selftestRunsTarget(h host, t string) (string, error) {
	if strings.HasPrefix(t, "arm64-") && !strings.HasPrefix(h.target, "arm64-") {
		return t + "'s selftest runs only on an arm64 host (CI's arm64 runner, the Mac's Linux VM): under qemu-user the compiler's own tests ran 43 minutes without ending", nil
	}
	return runsTarget(h, t)
}

func (r suiteRun) otherBehaviours(t string) error {
	why, err := runsTarget(r.h, t)
	if err != nil || why != "" {
		if why != "" {
			skip("behaviours on "+t, why)
		}
		return err
	}
	return r.behaviours("--target=" + t)
}

// foreignTargets are the targets whose executables the host does not run: those runsTarget gives a reason for, and macOS's
// off the Mac, where only the Windows gate's Mac lane runs them, on the Mac, when it is reachable. A host that should run a
// target and cannot (Windows without WSL) fails that target's lanes instead.
func foreignTargets(h host) []string {
	var foreign []string
	for _, t := range targets {
		if t == h.target {
			continue
		}
		if t == "arm64-macos" {
			if !h.macos() {
				foreign = append(foreign, t)
			}
			continue
		}
		if why, err := runsTarget(h, t); err == nil && why != "" {
			foreign = append(foreign, t)
		}
	}
	return foreign
}

// specsCompiledFor is, for each host, the one foreign target its gate compiles every spec test for, the one that no other
// lane holds the spec tests to; a host not listed compiles none. Compiling the spec tests costs about three minutes of the
// Mac's time for each target, so each target is paid for once, where it is seen least: the Mac takes x64-windows, whose
// register set and calling convention no lane but Windows's sees, while CI runs the x64-linux spec tests natively on every
// push. Windows and Linux take none: arm64 macOS's spec tests are run natively by the Mac's gate and by CI on every push, so
// compiling them on Windows as well repeated that work for 140 seconds a gate, and Linux's foreign targets are the other two
// hosts' own. The corpus, which is cheap, is compiled for every foreign target (compilesFor).
var specsCompiledFor = map[string]string{
	"arm64-macos": "x64-windows",
}

// compilesFor compiles the corpus for a target, and every spec test where specsCompiledFor names the target for the host,
// runs nothing, and fails at a compiler panic or a compile that does not go as it should: a program of the corpus must
// compile, or give the error it expects, and a spec test must compile as its blocks say wherever the host's compile of it
// does (`test specs --compile-only`).
func compilesFor(h host, compiler, t string, testArgs []string) error {
	flags := append([]string{"--compile-only"}, corpusFlags(t)...)
	if err := quiet(nil, compiler, slices.Concat([]string{"test"}, flags, testArgs)...); err != nil {
		return err
	}
	if specsCompiledFor[h.target] != t {
		return nil
	}
	return quiet(nil, compiler, slices.Concat([]string{"test", "specs"}, flags)...)
}

// selfTestsCompileFor builds the executable of the compiler's own tests (selftest) for a target the host does not run, with
// the IR and the allocation of every function verified, and runs nothing: the host's selftest compiles them for the host
// only, so a compile that panics for another target, as x64's register allocator did on a panic's message of interpolated
// calls in a file of tests (59d43678), would otherwise show only on that target's host. It takes about two seconds a target.
func selfTestsCompileFor(compiler, t string) error {
	return quiet(nil, compiler, "selftest", "--compile-only", "--target="+t, "--verify-ir")
}

// benchmarksCompile compiles every program of tests/bench, and runs none, for every target, with the IR and the allocation of
// every function verified: the benchmarks are not part of the corpus, since they run long enough to time, so a change that
// stops one compiling would show only when somebody next measures.
func benchmarksCompile(compiler string, testArgs []string) error {
	return eachTarget(func(t string) error {
		return quiet(nil, compiler, append([]string{"test", filepath.Join("tests", "bench"), "--target=" + t, "--verify-ir", "--compile-only"}, testArgs...)...)
	})
}

// selfCompilesForArm64 compiles the compiler's own source, the largest program there is, for arm64 as well, uncached, with
// the IR and the allocation of every function verified, since only the host's build of it runs in the gate. Running that
// build is left to an arm64 machine: under qemu a self-build takes about fifteen seconds.
func selfCompilesForArm64(compiler string) error {
	dir, err := scratchDir("minimaxon-self-arm64-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, t := range []string{"arm64-linux", "arm64-macos"} {
		if err := quiet(cacheOffEnvironment(), compiler, "build", "self", "--target="+t, "--verify-ir", "-o", filepath.Join(dir, "minimaxon")); err != nil {
			return err
		}
	}
	return nil
}

// compilesWithoutTmpdir runs a program with TMPDIR taken out of the environment, as a GitHub runner leaves it: the compiler
// must then use /tmp, as maxon does (ast.temporaryDirectory), and the run, which builds its executable there, must succeed
// and leave the compiler's directory in /tmp. Windows has no default for its TEMP, so it is not run there.
func compilesWithoutTmpdir(h host, compiler string) error {
	if h.windows() {
		skip("TMPDIR unset", "Windows names its directory of temporary files by TEMP, and has no default for it")
		return nil
	}
	cmd := exec.Command(compiler, "run", filepath.Join("tests", "run", "a-function-with-no-statement-is-allowed.maxon"))
	cmd.Env = slices.DeleteFunc(os.Environ(), func(v string) bool { return strings.HasPrefix(v, "TMPDIR=") })
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s%s with TMPDIR unset: %w", out, strings.Join(cmd.Args, " "), err)
	}
	if !isDir(filepath.Join("/tmp", "minimaxon")) {
		return errors.New("with TMPDIR unset the compiler did not make its directory of temporary files in /tmp")
	}
	return nil
}

// refusesUnwritableOutput builds a program into a directory that does not exist, which the compiler must refuse as an error
// that names the path and the reason, with exit code 1, and not with a panic: an output it cannot write is the environment's.
func refusesUnwritableOutput(compiler string) error {
	dir, err := scratchDir("minimaxon-unwritable-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "no-such-directory", "program")
	cmd := exec.Command(compiler, "build", filepath.Join("tests", "run", "a-function-with-no-statement-is-allowed.maxon"), "-o", out)
	text, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return fmt.Errorf("%s%s: want exit code 1, got %v", text, strings.Join(cmd.Args, " "), err)
	}
	want := "error: cannot write " + out + ": a directory of the path does not exist\n"
	if string(text) != want {
		return fmt.Errorf("%s: want %q, got %q", strings.Join(cmd.Args, " "), want, text)
	}
	return nil
}

var (
	specCounts  = regexp.MustCompile(`(?m)^(\d+) passed, \d+ failed, \d+ skipped`)
	specNew     = regexp.MustCompile(`(?m)^NEW `)
	specDropped = regexp.MustCompile(`(?m)^DROPPED `)
	specLost    = regexp.MustCompile(`(?m)^(LOST [^\r\n]*|\d+ tests of [^\r\n]* did not pass)`)
)

// specsRequired runs every spec test (or replays the results of an earlier run of the same inputs, specRun), and fails when
// a test that specs/passed.txt lists does not pass (the line "LOST file.md: test: what"). The tests that pass and are not
// listed are counted (the line "NEW file.md: test"), so that the list can be extended. A run that records also writes the
// list again in the same run, as `gate specs` does: the new passes are added, a listed test that the conversion now skips is
// dropped (the line "DROPPED"), and a listed test that fails stays, so the gate fails on it until it is fixed.
func specsRequired(compiler string, record bool) error {
	flags := []string{"--require-passed"}
	if record {
		flags = append(flags, "--record-passed")
	}
	out, replayed, runErr := specRun(compiler, flags...)
	if replayed != "" {
		defer fmt.Println(replayed)
	}
	counts := specCounts.FindString(out)
	if counts == "" {
		return fmt.Errorf("%s\nthe spec runner printed no count", out)
	}
	if runErr != nil {
		lost := specLost.FindAllString(out, -1)
		return fmt.Errorf("%s\nspecs: %s", strings.Join(lost, "\n"), counts)
	}
	news := len(specNew.FindAllString(out, -1))
	if record {
		fmt.Printf("specs: %s; %d added to and %d dropped from specs/passed.txt\n", counts, news, len(specDropped.FindAllString(out, -1)))
		return nil
	}
	fmt.Printf("specs: %s; %d new, to add to specs/passed.txt with: %s test specs --record-passed\n", counts, news, filepath.Join("bin", "minimaxon"))
	return nil
}

// A full spec run (--require-passed or --record-passed, and nothing else) keeps the result of every test in specResults
// beside specStamp, the stamp of everything those results depend on, which is written only when the run ended and nothing
// it depends on changed while it ran. A later full run whose stamp is the same takes the results from the file instead of
// compiling and running every test again (`test specs --replay-results=`), and still counts them against the current
// specs/passed.txt, which is why that list is not in the stamp. Whatever is in doubt (no stamp, a different input, a run that
// did not end, a file that is not of these tests) runs every test.
var (
	specResults = filepath.Join("bin", "spec-results.txt")
	specStamp   = filepath.Join("bin", "spec-results.stamp")
)

// specInterpretFlag has every spec test that passes run under the interpreter too, which fails it for a proven access out of
// range: a read the checker proved in range, lowered with no check, that is out of range shows there and nowhere else.
const specInterpretFlag = "--interpret"

// replayRefusedCode is the runner's code when it refuses results that are not those of its tests (replayRefusedExitCode in
// self/driver).
const replayRefusedCode = 3

// specStampEnvironment is the environment that a test inherits and a result can follow.
var specStampEnvironment = []string{"MAXON_MAX_PROCS", "MAXON_HASH_SEED", cacheVariable}

// specStampOf is the text a spec run's results are kept under, equal texts being equal inputs: the compiler, which holds the
// runner and finds the runtime and the library beside itself, the spec files (the conversion's skips are lines of them),
// the skip table the conversion reads, the sources that every test is compiled with, and the environment. The list of the
// tests that passed is read again by every run, which is why it is left out.
func specStampOf(compiler string) (string, error) {
	compilerSum, err := fileDigest(compiler)
	if err != nil {
		return "", err
	}
	inputs, err := pathsDigest([]string{"specs", "runtime", "stdlib", filepath.Join("self", "driver", "skips.tsv")}, []string{abs(filepath.Join("specs", "passed.txt"))})
	if err != nil {
		return "", err
	}
	var env []string
	for _, name := range specStampEnvironment {
		env = append(env, name+"="+os.Getenv(name))
	}
	return fmt.Sprintf("compiler %s\ninputs %s\nflags %s\nenvironment %s", compilerSum, inputs, specInterpretFlag, strings.Join(env, " ")), nil
}

// specRun runs every spec test with the flags that say what the run makes of the results (--require-passed, --record-passed
// or both), replaying the kept results when their stamp is the current one, and gives what the runner wrote, the
// line that says it replayed, and whether it ended as required. cmd/envrun sets the environment a test's directives ask for
// (procs, preempt) for its program alone.
func specRun(compiler string, classifying ...string) (string, string, error) {
	if err := buildEnvRunner(); err != nil {
		return "", "", err
	}
	classifying = append(classifying, "--env-runner="+abs(envRunnerPath()))
	stamp, err := specStampOf(compiler)
	if err != nil {
		return "", "", err
	}
	// The interpreter acts in the directory it is started in, where the spec tests that write files would leave them.
	dir, err := scratchDir("minimaxon-specs-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	specs := abs("specs")
	if isFile(specResults) && stampHolds(specStamp, stamp) {
		out, err := output(nil, dir, compiler, slices.Concat([]string{"test", specs, "--replay-results=" + abs(specResults)}, classifying)...)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != replayRefusedCode {
			return out, fmt.Sprintf("replayed: the results of the run that stamp %s was written for, kept in %s", stampID(stamp), specResults), err
		}
		fmt.Println("the kept results are not of these tests, so every test runs")
	}
	os.Remove(specStamp)
	os.Remove(specResults)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return "", "", err
	}
	out, runErr := output(nil, dir, compiler, slices.Concat([]string{"test", specs, specInterpretFlag, "--save-results=" + abs(specResults)}, classifying)...)
	// A run ends with the runner's code 0 (all as required) or 1 (a test did not pass); another code is a crash or a refusal.
	var exit *exec.ExitError
	ended := runErr == nil || errors.As(runErr, &exit) && exit.ExitCode() == 1
	if after, err := specStampOf(compiler); ended && err == nil && after == stamp && isFile(specResults) {
		if err := os.WriteFile(specStamp, []byte(stamp), 0o644); err != nil {
			return out, "", err
		}
	} else {
		os.Remove(specStamp)
		os.Remove(specResults)
	}
	return out, "", runErr
}
