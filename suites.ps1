# Runs every suite that holds a compiler to the repository's rules, with the compiler at -Compiler: the Go tests of the
# generators and tools, then the compiler's own suites. The gate (check.ps1) runs it on the compiler it builds from the
# seed, and the seed renewal (seed.ps1) on the compiler that would become the seed, so the two cannot disagree about
# what a compiler must pass. Arguments after -Compiler go to `minimaxon test`.
#
# The suites are in lanes that share nothing but the compiler and the checkout. By default every suite runs in this process,
# in the order of the lanes, and stops at the first that fails. -Concurrent (or MINIMAXON_GATE_CONCURRENT set, for check.ps1) runs each lane as a process of its own, all at
# once, so that the gate takes as long as its longest lane, and the first lane to fail ends the others: it is not the default
# because a test with a deadline (the arm64 corpus under qemu, a program of timers on one worker) failed now and then when the
# lanes and other work shared the machine (README, "Tests"). -Lane runs one lane, which is how the lanes are started.
param(
	[Parameter(Mandatory)][string]$Compiler,
	[string]$Lane = '',
	[switch]$Concurrent,
	[Parameter(ValueFromRemainingArguments)]$TestArgs
)
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
. "$PSScriptRoot\hashseed.ps1"
. "$PSScriptRoot\steptimes.ps1"
# check.ps1 has started the table of times when it runs this script, and prints it itself. A lane's times are the lane
# starter's, which it is given in a file.
$ownsStepTimes = (Start-StepTimes) -and -not $Lane
$compiler = (Resolve-Path $Compiler).Path

# Every spec test runs (or is replayed from the results of an earlier run over the same inputs, see spec-run.ps1), and the step fails when a test that specs/passed.txt lists does not pass (the line "LOST file.md:
# test: what"). The tests that pass and are not listed are counted (the line "NEW file.md: test"), so that the list can be
# extended.
function Test-Specs {
	$output = & "$PSScriptRoot\spec-run.ps1" $compiler --require-passed 2>&1 | Out-String
	$code = $LASTEXITCODE
	if ($output -notmatch '(?m)^(\d+) passed, \d+ failed, \d+ skipped') {
		Write-Output $output
		Write-Output 'the spec runner printed no count'
		$global:LASTEXITCODE = 1
		return
	}
	$counts = $Matches[0]
	$new = ([regex]::Matches($output, '(?m)^NEW ')).Count
	# spec-run.ps1 says when it took the results of an earlier run, and from which.
	$replayed = [regex]::Matches($output, '(?m)^replayed: [^\r\n]*') | ForEach-Object { $_.Value }
	if ($code -ne 0) {
		[regex]::Matches($output, '(?m)^(LOST [^\r\n]*|\d+ tests of [^\r\n]* did not pass)') | ForEach-Object { Write-Output $_.Value }
		Write-Output "specs: $counts"
		$replayed
		$global:LASTEXITCODE = 1
		return
	}
	Write-Output "specs: $counts; $new new, to add to specs/passed.txt with: bin\minimaxon.exe test specs --record-passed"
	$replayed
	$global:LASTEXITCODE = 0
}

# The worker counts that the programs that schedule behaviours run under again, natively, after the corpus has run with the
# machine's: a bug of the runtime's memory ordering showed only at four workers, so the counts reach past two.
$behaviourWorkers = 1, 2, 4

# The corpus runs with the machine's worker count; the programs that schedule behaviours then run again, natively, under
# each of $behaviourWorkers. The programs are compiled once for all the counts and each executable is run under each, one
# after another (`test --worker-counts=`): cmd/envrun gives an executable its count (and has WSL pass it on, which forwards
# only the variables that WSLENV names), where the environment of the runner, which its executables inherit, would also
# narrow the runner and compile everything again for each count.
$envRunner = Join-Path $PSScriptRoot 'bin\envrun.exe'

function Build-EnvRunner {
	$null = New-Item -ItemType Directory -Force (Split-Path $envRunner)
	go build -o $envRunner ./cmd/envrun
}

function Get-WorkerCountArgs {
	$counts = @("--worker-counts=$($behaviourWorkers -join ',')", "--env-runner=$envRunner")
	if ($Lane) { return $counts + "--executables=$behavioursExecutables" }
	return $counts
}

function Test-Behaviours([string[]]$TargetArgs = @()) {
	$null | & $compiler test --behaviours @TargetArgs @(Get-WorkerCountArgs) @TestArgs
}

# The x64-linux corpus is compiled for Linux and run under WSL, in its default distribution, so the second target is held to
# the same programs, and so is the arm64-linux corpus, whose static executables WSL runs through the qemu-aarch64 that the
# distribution registers with binfmt_misc; each of its programs is compiled with --verify-ir, which holds arm64's selection and
# allocation to the invariants of every function. A machine without WSL or a distribution fails the step: a gate that skipped
# it would pass without running Linux's code.
$linuxTarget = '--target=x64-linux'

$arm64LinuxTarget = '--target=arm64-linux'

function Assert-Wsl {
	if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
		Write-Output 'the Linux corpus runs under WSL, and wsl.exe is not installed: install WSL (wsl --install)'
		$global:LASTEXITCODE = 1
		return
	}
	$null = wsl.exe --exec /bin/true 2>&1
	if ($LASTEXITCODE -ne 0) {
		Write-Output 'the Linux corpus runs under WSL, and its default distribution cannot run a program: install one (wsl --install -d Ubuntu) and start it once'
		$global:LASTEXITCODE = 1
	}
}

function Test-Linux {
	Assert-Wsl
	if ($LASTEXITCODE -ne 0) { return }
	$null | & $compiler test $linuxTarget @TestArgs
}

function Test-LinuxBehaviours {
	Assert-Wsl
	if ($LASTEXITCODE -ne 0) { return }
	Test-Behaviours @($linuxTarget)
}

function Test-Arm64Linux {
	Assert-Wsl
	if ($LASTEXITCODE -ne 0) { return }
	$null | & $compiler test $arm64LinuxTarget --verify-ir @TestArgs
}

function Test-Arm64LinuxBehaviours {
	Assert-Wsl
	if ($LASTEXITCODE -ne 0) { return }
	Test-Behaviours @($arm64LinuxTarget)
}

# The arm64-macos corpus runs on the Mac, which this machine reaches only through a folder that both mount ($macShare,
# the Mac's ~/Dev/agents/minimaxon): the gate compiles every program and the reruns of those that schedule behaviours,
# keeping each executable through cmd/batchrun, and when the share holds the Mac's results of exactly those executables
# (the stamp of their rows), replays them through the test runner, which compares them, and fails on any failure.
# Otherwise it stages the batch at in\gate for the Mac session to run (bash ~/Dev/agents/minimaxon/in/gate/run.sh) and
# skips, as it does when the share is not mounted: the Mac is not the gate's machine.
$macShare = 'M:\agents\minimaxon'

$macTarget = '--target=arm64-macos'

# robocopy's exit codes below 8 say what it copied; 8 and above, that it failed.
$robocopyFailed = 8

function Invoke-MacCorpus([string]$batchrun, [string]$mode, [string]$stage) {
	$runner = "--runner=$batchrun,$mode,$stage"
	$null | & $compiler test $macTarget --verify-ir $runner @TestArgs
	$corpus = $LASTEXITCODE
	$null | & $compiler test --behaviours $macTarget $runner @(Get-WorkerCountArgs) @TestArgs
	return [Math]::Max($corpus, $LASTEXITCODE)
}

function Test-Mac {
	if (-not (Test-Path "$macShare\in")) {
		Write-Output "mac lane: skipped, $macShare is not mounted"
		$global:LASTEXITCODE = 0
		return
	}
	$root = Join-Path ([IO.Path]::GetTempPath()) "minimaxon-mac-$PID"
	try {
		$null = New-Item -ItemType Directory -Force $root
		$batchrun = Join-Path $root 'batchrun.exe'
		go build -o $batchrun ./cmd/batchrun
		if ($LASTEXITCODE -ne 0) { return }
		$stage = Join-Path $root 'stage'
		$null = Invoke-MacCorpus $batchrun 'record' $stage
		$stamp = (Get-ChildItem "$stage\rows" | Sort-Object Name | ForEach-Object Name) -join "`n"
		$results = "$macShare\out\gate"
		if ((Test-Path "$results\stamp.txt") -and ([IO.File]::ReadAllText("$results\stamp.txt").TrimEnd() -eq $stamp)) {
			$replay = Join-Path $root 'results'
			$null = robocopy "$results\out" "$replay\out" /E /NFL /NDL /NJH /NJS /NP
			if ($LASTEXITCODE -ge $robocopyFailed) {
				Write-Output "mac lane: cannot copy the Mac's results from $results"
				$global:LASTEXITCODE = 1
				return
			}
			$output = Invoke-MacCorpus $batchrun 'replay' $replay
			$code = $output[-1]
			$output | Select-Object -SkipLast 1 | Where-Object { $_ -notmatch '^PASS ' }
			$global:LASTEXITCODE = $code
			return
		}
		$batch = "$macShare\in\gate"
		$env:GOOS = 'darwin'; $env:GOARCH = 'arm64'
		try { go build -o "$stage\batchrun" ./cmd/batchrun } finally { Remove-Item Env:GOOS; Remove-Item Env:GOARCH }
		if ($LASTEXITCODE -ne 0) { return }
		Copy-Item cmd\batchrun\run.sh "$stage\run.sh"
		[IO.File]::WriteAllText("$stage\stamp.txt", "$stamp`n")
		# A mirror, so no row or executable of an older batch stays for the Mac to run: the share refuses some listings
		# that Remove-Item makes, and robocopy's purge does not.
		$null = robocopy $stage $batch /MIR /NFL /NDL /NJH /NJS /NP
		if ($LASTEXITCODE -ge $robocopyFailed) {
			Write-Output "mac lane: cannot stage the batch at $batch"
			$global:LASTEXITCODE = 1
			return
		}
		Write-Output "mac lane: skipped, the Mac has no results of this corpus: run bash ~/Dev/agents/minimaxon/in/gate/run.sh there and the gate again"
		$global:LASTEXITCODE = 0
	} finally {
		Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
	}
}

# The benchmarks are not part of the corpus, since they run long enough to time, so a change that stops one compiling would
# show only when somebody next measures. Each is compiled, and none run, for every target, with the IR and the allocation of
# every function verified.
$benchmarkTargets = '--target=x64-windows', '--target=x64-linux', '--target=arm64-linux', '--target=arm64-macos'

function Test-BenchmarksCompile {
	foreach ($t in $benchmarkTargets) {
		$null | & $compiler test tests\bench $t --verify-ir --compile-only
		if ($LASTEXITCODE -ne 0) { return }
	}
}

# The compiler's own source is the largest program there is, and only the x64 build of it runs in the gate, so it is compiled
# for arm64 as well, uncached, with the IR and the allocation of every function verified. Running that build is left to an
# arm64 machine: under qemu a self-build takes about fifteen seconds.
$selfArm64Targets = '--target=arm64-linux', '--target=arm64-macos'

function Test-SelfCompilesForArm64 {
	$root = Join-Path ([IO.Path]::GetTempPath()) "minimaxon-self-arm64-$PID"
	try {
		$null = New-Item -ItemType Directory -Force $root
		foreach ($t in $selfArm64Targets) {
			& $compiler build self $t --verify-ir --no-cache -o (Join-Path $root 'minimaxon')
			if ($LASTEXITCODE -ne 0) { return }
		}
	} finally {
		Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
	}
}

# Debug info is written only with --debug-info and must leave the executable as it is: programs are built for every target
# with and without it, and self for this one, with every span verified (--verify-ir), and the executables compared. The
# sidecar of self must be walkable, and on every target that of a program whose locals are in registers, in frame slots and
# in two words (a String) must say so, with its record's fields. A stack overflow's fault line, whose address is in a
# function of one statement, is then symbolized to that statement on x64 Windows (a copy of the executable that the loader
# keeps at its image base), x64 Linux and arm64 Linux (under WSL and its qemu). On x64 Windows a batch session of the
# debugger drives the locals program (debug --batch) and its events are held to $sessionEvents.
$debugInfoTargets = 'x64-windows', 'x64-linux', 'arm64-linux', 'arm64-macos'
$debugInfoPrograms = 'tests\run\arithmetic.maxon', 'tests\run\array-methods.maxon', 'tests\run\generics.maxon'
$overflowLine = 'overflow.maxon:3:2  (in export::down)'
$localsSource = @"
// The program whose locals the debug info step reads: across holds more values over a call than the callee-saved
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
"@
# What the dump of the locals program must show: a local of across in a register and one in a frame slot, the second word of
# a String, and the fields of Pair.
$localsExpected = '(?m)^        a  \[0x[0-9a-f]+, 0x[0-9a-f]+\)  (r[a-z0-9]+|x[0-9]+)  :', '(?m)^        [a-o]  \[0x[0-9a-f]+, 0x[0-9a-f]+\)  \[(rbp|x29)[-+]0x[0-9a-f]+\]  :', '(?m)^        greeting  .* \(second word\)  : String', '(?m)^        \+0x0008  name : String'
# The events the batch session of the locals program must answer, one per command that has one.
$sessionEvents = @(
	'^\{"event":"breakpoint","action":"set","function":"export::across","file":"locals.maxon","line":20,'
	'^\{"event":"breakpoint","action":"set","function":"export::named","file":"locals.maxon","line":32,'
	'^\{"event":"stop","reason":"breakpoint","function":"export::across","file":"locals.maxon","line":20,.*"current":true.*"backtrace":\[\{"frame":0,"function":"export::across".*\{"frame":1,"function":"export::main"'
	'^\{"event":"value","value":\{"name":"a","type":"[^"]*","kind":"IntRanged","display":"6"\}\}$'
	'^\{"event":"stop","reason":"step","function":"export::across","file":"locals.maxon","line":21,'
	'^\{"event":"value","value":\{"name":"h","type":"[^"]*","kind":"IntRanged","display":"13"\}\}$'
	'^\{"event":"stop","reason":"breakpoint","function":"export::named","file":"locals.maxon","line":32,'
	'^\{"event":"locals","function":"export::named","locals":\[\{"name":"text","type":"String","kind":"String","display":"\\"x\\" \(len=1\)"\}'
	'^\{"event":"stop","reason":"step","function":"export::named","file":"locals.maxon","line":33,'
	'^\{"event":"value","value":\{"name":"pair.name","type":"String","kind":"String","display":"\\"x\\" \(len=1\)"\}\}$'
	'^\{"event":"value","value":\{"name":"pair.left","type":"Count","kind":"IntRanged","display":"4"\}\}$'
	'^\{"event":"backtrace","frames":\[\{"frame":0,"function":"export::named".*\{"frame":1,"function":"export::main"'
	'^\{"event":"stop","reason":"step","function":"export::main","file":"locals.maxon","line":38,'
	'^\{"event":"exit","code":0\}$'
)
$overflowSource = @"
// The program whose stack overflow the debug info step symbolizes: every instruction of down is its one statement's.
function down(n Count) returns Count
	return try down(n + 1) + 1 otherwise 0
end 'down'

function main() returns ExitCode
	_ = down(0)
	return 0
end 'main'
"@

function Test-DebugInfo {
	$root = Join-Path ([IO.Path]::GetTempPath()) "minimaxon-debug-info-$PID"
	try {
		$null = New-Item -ItemType Directory -Force $root
		$overflow = Join-Path $root 'overflow.maxon'
		[IO.File]::WriteAllText($overflow, $overflowSource.Replace("`r`n", "`n") + "`n")
		$locals = Join-Path $root 'locals.maxon'
		[IO.File]::WriteAllText($locals, $localsSource.Replace("`r`n", "`n") + "`n")
		function Build($source, $out, [string[]]$flags) {
			$text = & $compiler build $source -o $out --no-cache @flags 2>&1 | Out-String
			if ($LASTEXITCODE -ne 0) { throw "debug info: the build of $source $flags failed: $text" }
			return (Get-FileHash $out).Hash
		}
		# A Mach-O image is signed under its file's name, so both builds of a program have one.
		$plainRoot = Join-Path $root 'plain'
		$null = New-Item -ItemType Directory -Force $plainRoot
		function Same($source, [string[]]$flags, $name) {
			$plain = Build $source (Join-Path $plainRoot "$name.exe") $flags
			$described = Build $source (Join-Path $root "$name.exe") ($flags + @('--debug-info', '--verify-ir'))
			if ($plain -ne $described) { throw "debug info: --debug-info changes the executable of $source $flags" }
			if (-not (Test-Path (Join-Path $root "$name.exe.mxdbg"))) { throw "debug info: no sidecar beside the build of $source $flags" }
		}
		Same (Join-Path $PSScriptRoot 'self') @() 'self'
		$walked = & $compiler debug --dump-info (Join-Path $root 'self.exe') functions types 2>&1 | Out-String
		if ($LASTEXITCODE -ne 0 -or $walked -notmatch '(?m)^  types \([1-9]') { throw "debug info: the sidecar of self cannot be walked: $($walked.Substring(0, [Math]::Min(400, $walked.Length)))" }
		foreach ($t in $debugInfoTargets) {
			foreach ($p in @($overflow, $locals) + $debugInfoPrograms) {
				Same $p @("--target=$t") "$([IO.Path]::GetFileNameWithoutExtension($p))-$t"
			}
			$dump = & $compiler debug --dump-info (Join-Path $root "locals-$t.exe") functions types 2>&1 | Out-String
			foreach ($expected in $localsExpected) {
				if ($dump -notmatch $expected) { throw "debug info: the locals of the program built for $t do not match $expected" }
			}
		}
		# A batch session drives the locals program on x64 Windows: a breakpoint on a bare line and one on a function, the
		# backtrace, a scalar local in a register and one in a frame slot, a step over a line, a String local, a record's
		# fields, a step out of a frame and the run to the program's exit. The program's own output goes to stderr.
		$ErrorActionPreference = 'Continue'
		$session = Join-Path $root 'locals-x64-windows.exe'
		$events = @(& $compiler debug --batch '--commands=break 20;break named;run;print a;next;print h;continue;locals;next;print pair.name;print pair.left;backtrace;finish;continue' $session 2>$null)
		if ($LASTEXITCODE -ne 0) { throw "debug info: the batch session of $session exits $LASTEXITCODE`: $($events -join "`n")" }
		foreach ($i in 0..($sessionEvents.Count - 1)) {
			if ($i -ge $events.Count -or $events[$i] -notmatch $sessionEvents[$i]) { throw "debug info: event $i of the batch session does not match $($sessionEvents[$i]): $($events -join "`n")" }
		}
		if ($events.Count -ne $sessionEvents.Count) { throw "debug info: the batch session answered $($events.Count) events, not $($sessionEvents.Count): $($events -join "`n")" }
		# The loader keeps an image whose header asks for no move at its image base, where the sidecar places its code.
		$windows = Join-Path $root 'overflow-x64-windows.exe'
		$fixed = Join-Path $root 'fixed.exe'
		$bytes = [IO.File]::ReadAllBytes($windows)
		$characteristics = [BitConverter]::ToInt32($bytes, 0x3C) + 24 + 70
		$bytes[$characteristics] = $bytes[$characteristics] -band (-bnot 0x40)
		[IO.File]::WriteAllBytes($fixed, $bytes)
		Copy-Item "$windows.mxdbg" "$fixed.mxdbg"
		$runs = @(, @($fixed, { param($exe) & $exe 2>&1 }))
		Assert-Wsl
		if ($LASTEXITCODE -ne 0) { return }
		foreach ($t in 'x64-linux', 'arm64-linux') {
			$runs += , @((Join-Path $root "overflow-$t.exe"), { param($exe) wsl.exe --exec ('/mnt/' + $exe.Substring(0, 1).ToLower() + $exe.Substring(2).Replace('\', '/')) 2>&1 })
		}
		# The program that overflows its stack writes its fault line to stderr, which Windows PowerShell 5 raises as an error
		# under 'Stop' where pwsh does not.
		$ErrorActionPreference = 'Continue'
		foreach ($run in $runs) {
			$exe, $start = $run
			$fault = (& $start $exe | Out-String)
			if ($fault -notmatch 'rip=(0x[0-9a-f]+)') { throw "debug info: $exe printed no fault line: $fault" }
			$named = & $compiler debug --symbolize $exe "rip=$($Matches[1])" 2>&1 | Out-String
			if ($LASTEXITCODE -ne 0 -or -not $named.Contains($overflowLine)) { throw "debug info: rip=$($Matches[1]) of $exe symbolizes to $named, not $overflowLine" }
		}
	} catch {
		Write-Output $_.Exception.Message
		$global:LASTEXITCODE = 1
		return
	} finally {
		Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
	}
	$global:LASTEXITCODE = 0
}
# The build cache must make the executable an uncached build makes. A project whose main inlines a leaf of the module it
# names is built cold and warm, then the leaf's body is edited: the order takes the leaf's module to make the summary it
# made before, main's entry is linked, the leaf's lower finds another summary and the compile is done again, and the
# executable must be the uncached one of the edited source and print what that source prints. All of it runs on the
# machine's workers and again on one, each with a cache of its own.
function Test-Cache {
	$case = Join-Path $PSScriptRoot 'tests\modules\a-leaf-of-a-named-module-is-inlined'
	$beforeCache = $env:MINIMAXON_CACHE
	$beforeProcs = $env:MAXON_MAX_PROCS
	$root = Join-Path ([IO.Path]::GetTempPath()) "minimaxon-cache-test-$PID"
	try {
		foreach ($procs in '', '1') {
			$env:MAXON_MAX_PROCS = $procs
			Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
			$project = Join-Path $root 'project'
			$null = New-Item -ItemType Directory -Force $root
			Copy-Item -Recurse $case $project
			$env:MINIMAXON_CACHE = Join-Path $root 'cache'
			function Build($name, [string[]]$flags) {
				$exe = Join-Path $root "$name.exe"
				$out = & $compiler build $project -o $exe @flags 2>&1 | Out-String
				if ($LASTEXITCODE -ne 0) { throw "cache: the $name build failed: $out" }
				return (Get-FileHash $exe).Hash
			}
			function Same($a, $b, $what) {
				if ($a -ne $b) { throw "cache ($(if ($procs) { "$procs worker" } else { 'all workers' })): $what" }
			}
			$uncached = Build 'uncached' @('--no-cache')
			Same (Build 'cold' @()) $uncached 'the cold build differs from the uncached one'
			Same (Build 'warm' @()) $uncached 'the warm build differs from the uncached one'
			$leaf = Join-Path $project 'gauges\gauges.maxon'
			$text = [IO.File]::ReadAllText($leaf)
			$edited = $text.Replace("`t`treturn i`n`tend 'inside'", "`t`treturn i + 1`n`tend 'inside'")
			if ($edited -eq $text) { throw 'cache: the leaf to edit is not in the test project' }
			[IO.File]::WriteAllText($leaf, $edited)
			$editedUncached = Build 'edited-uncached' @('--no-cache')
			Same (Build 'edited' @()) $editedUncached 'after a misprediction the build differs from the uncached one'
			Same (Build 'edited-warm' @()) $editedUncached 'the warm build of the edited source differs from the uncached one'
			$printed = & (Join-Path $root 'edited.exe')
			if ($printed -eq '5920') { throw 'cache: the edited leaf did not change what the program prints' }
			# Two edits of main's body: the first checks the module main names again and keeps its checked state, the second
			# loads that state instead.
			$main = Join-Path $project 'main.maxon'
			$mainText = [IO.File]::ReadAllText($main)
			foreach ($n in 1, 2) {
				$probe = "`tif total == $(999990 + $n) 'probe'`n`t`tprint(`"probe`")`n`tend 'probe'`n`tprint("
				[IO.File]::WriteAllText($main, $mainText.Replace("`tprint(", $probe))
				$mainUncached = Build "main$n-uncached" @('--no-cache')
				Same (Build "main$n" @()) $mainUncached "after edit $n of main the build differs from the uncached one"
			}
			if (-not (Get-ChildItem $env:MINIMAXON_CACHE -Filter *.checked)) { throw 'cache: no checked state was kept' }
			$null = Build 'verified' @('--verify-cache')
		}
	} catch {
		Write-Output $_.Exception.Message
		$global:LASTEXITCODE = 1
		return
	} finally {
		$env:MINIMAXON_CACHE = $beforeCache
		$env:MAXON_MAX_PROCS = $beforeProcs
		Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
	}
	$global:LASTEXITCODE = 0
}

# Each step is its name, what it runs and its lane. The lanes: `prelude` is what the others need (it runs first, here), `checks`
# the compiler's own suites and the generated programs, `builds` what is compiled and not run here, `specs` the spec tests, and one
# lane for each target whose corpus is run. Within a lane the order is the order here.
#
# The lanes are in two phases, the second begins when the first has ended: the reruns of the programs that schedule behaviours
# under one worker are the one thing here that a loaded machine fails (a timer that fires late reorders what such a program
# prints), so they run when the other lanes are done, and only beside each other, each with its executables at the cap below.
$lanePhases = @(
	@('checks', 'builds', 'specs', 'native', 'x64-linux', 'arm64-linux', 'mac'),
	@('native-behaviours', 'x64-linux-behaviours', 'arm64-linux-behaviours')
)
$laneNames = @($lanePhases | ForEach-Object { $_ })
# How many executables the runner of a behaviours lane runs at once, where the default (half the workers, at most 8) would run
# the three lanes' as many.
$behavioursExecutables = 2
$steps = @(
	@('envrun', { Build-EnvRunner }, 'prelude'),
	# cmd/convert lays out what it converts with the compiler's fmt.
	@('Go tests', { $env:MINIMAXON = $compiler; go test ./... }, 'checks'),
	@('selftest', { & $compiler selftest }, 'checks'),
	# Nothing compiles the runtime's source until a program is built, so this is what holds it to the language and to
	# the signatures the compiler calls, for each target, whose branches of the source differ.
	@('runtime', { & $compiler check runtime; foreach ($t in '--target=x64-linux', '--target=arm64-linux', '--target=arm64-macos') { if ($LASTEXITCODE -eq 0) { & $compiler check runtime $t } } }, 'checks'),
	# A program compiles only the library modules it names, so this is what holds the whole standard library to the language,
	# for each target.
	@('stdlib', { & $compiler check stdlib; foreach ($t in '--target=x64-linux', '--target=arm64-linux', '--target=arm64-macos') { if ($LASTEXITCODE -eq 0) { & $compiler check stdlib $t } } }, 'checks'),
	# The compiler's own source is a program too: a rule that it breaks would otherwise show only when the seed is renewed.
	@('self', { & $compiler check self --tests --verify-reads }, 'checks'),
	@('layout', { & $compiler fmt --check self runtime tests stdlib }, 'checks'),
	# Every program the generator writes that the checker accepts must run clean under the interpreter: a violation is a
	# hole in the checker.
	@('fuzz', { & $compiler fuzz }, 'checks'),
	@('benchmarks compile', { Test-BenchmarksCompile }, 'builds'),
	@('self compiles for arm64', { Test-SelfCompilesForArm64 }, 'builds'),
	@('cache', { Test-Cache }, 'builds'),
	@('debug info', { Test-DebugInfo }, 'builds'),
	@('specs', { Test-Specs }, 'specs'),
	# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
	@('corpus', { $null | & $compiler test @TestArgs }, 'native'),
	@('behaviours', { Test-Behaviours }, 'native-behaviours'),
	@('corpus on linux', { Test-Linux }, 'x64-linux'),
	@('behaviours on linux', { Test-LinuxBehaviours }, 'x64-linux-behaviours'),
	@('corpus on arm64 linux', { Test-Arm64Linux }, 'arm64-linux'),
	@('behaviours on arm64 linux', { Test-Arm64LinuxBehaviours }, 'arm64-linux-behaviours'),
	@('corpus on arm64 macos', { Test-Mac }, 'mac')
)

# Invoke-Steps runs the steps of the lanes, in order, in this process, and leaves the code with which they ended in $stepsCode: the
# first one that fails ends them, with its output and the line that names it. The steps that print what the gate shows are `specs` and the
# Mac lane's.
function Invoke-Steps([string[]]$Lanes) {
	foreach ($step in $steps) {
		$name, $run, $stepLane = $step
		if ($stepLane -notin $Lanes) { continue }
		$output = Invoke-Step $name { & $run 2>&1 }
		if ($LASTEXITCODE -ne 0) {
			$output | Write-Output
			Write-Output "check failed at: $name"
			$script:stepsCode = 1
			return
		}
		if ($name -in 'specs', 'corpus on arm64 macos' -and $output) { $output | Write-Output }
	}
	$script:stepsCode = 0
}

# The lanes of the gate each run in a process of their own, started from the same PowerShell as this one, with what they print
# and the times of their steps in files below $laneRoot.
function Get-LanePath([string]$Root, [string]$Name, [string]$Kind) {
	return Join-Path $Root "$Name.$Kind"
}

function Start-LaneProcess([string]$Root, [string]$Name) {
	$arguments = @('-NoProfile', '-File', "`"$PSScriptRoot\suites.ps1`"", '-Compiler', "`"$compiler`"", '-Lane', $Name)
	if ($TestArgs) { $arguments += @($TestArgs | ForEach-Object { "`"$_`"" }) }
	$env:MINIMAXON_LANE_TIMES = Get-LanePath $Root $Name 'times'
	$process = Start-Process -FilePath (Get-Process -Id $PID).Path -ArgumentList $arguments -NoNewWindow -PassThru `
		-RedirectStandardOutput (Get-LanePath $Root $Name 'out') -RedirectStandardError (Get-LanePath $Root $Name 'err')
	# Windows PowerShell 5 reports no exit code for a process whose handle it has not read.
	$null = $process.Handle
	return $process
}

# Stop-LaneProcess ends a lane and the programs it started.
function Stop-LaneProcess($Process) {
	if (-not $Process.HasExited) {
		$null = & taskkill.exe /PID $Process.Id /T /F 2>&1
	}
}

# Invoke-Lanes runs the lanes of each phase at once, the phases one after another, and leaves the code the gate ends with in
# $stepsCode. A lane that fails ends the others, so the run does not go on after it is known to have failed; the output of each
# lane that ran is then printed in the order of $laneNames, and the times of their steps join the table of this process.
function Invoke-Lanes {
	$root = Join-Path ([IO.Path]::GetTempPath()) "minimaxon-lanes-$PID"
	$null = New-Item -ItemType Directory -Force $root
	$processes = [ordered]@{}
	$code = 0
	$clock = [System.Diagnostics.Stopwatch]::StartNew()
	try {
		foreach ($phase in $lanePhases) {
			if ($code -ne 0) { break }
			foreach ($name in $phase) { $processes[$name] = Start-LaneProcess $root $name }
			$ended = @{}
			while ($ended.Count -lt $phase.Count) {
				Start-Sleep -Milliseconds 250
				foreach ($name in $phase) {
					$process = $processes[$name]
					if ($ended.ContainsKey($name) -or -not $process.HasExited) { continue }
					$ended[$name] = $process.ExitCode
					$ending = if ($process.ExitCode -eq 0) { 'passed' } elseif ($code -ne 0) { 'stopped, as another lane failed' } else { 'failed' }
					Write-Host "[$(Format-StepSeconds $clock.Elapsed.TotalSeconds)] lane $name $ending"
					if ($process.ExitCode -ne 0 -and $code -eq 0) {
						$code = 1
						foreach ($other in $phase) { Stop-LaneProcess $processes[$other] }
					}
				}
			}
		}
		foreach ($name in $laneNames) {
			if (-not $processes.Contains($name)) { continue }
			$out = Get-LanePath $root $name 'out'
			$text = if (Test-Path $out) { [IO.File]::ReadAllText($out).TrimEnd() } else { '' }
			if ($text) { Write-Output $text }
			$err = Get-LanePath $root $name 'err'
			if (Test-Path $err) {
				$errText = [IO.File]::ReadAllText($err).Trim()
				if ($errText) { Write-Output "lane $name wrote to its error output:`n$errText" }
			}
			$times = Get-LanePath $root $name 'times'
			if (Test-Path $times) {
				foreach ($line in [IO.File]::ReadAllLines($times)) {
					$stepName, $seconds = $line -split "`t"
					$global:StepTimes.Add([pscustomobject]@{ Name = $stepName; Seconds = [double]::Parse($seconds, [cultureinfo]::InvariantCulture) })
				}
			}
		}
	} finally {
		foreach ($name in $processes.Keys) { Stop-LaneProcess $processes[$name] }
		Remove-Item Env:MINIMAXON_LANE_TIMES -ErrorAction SilentlyContinue
		Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
	}
	$script:stepsCode = $code
}

# A lane tells where its steps' times go, which are printed by the process that started it.
function Save-LaneTimes {
	$lines = $global:StepTimes | ForEach-Object { "$($_.Name)`t$($_.Seconds.ToString('R', [cultureinfo]::InvariantCulture))" }
	[IO.File]::WriteAllLines($env:MINIMAXON_LANE_TIMES, [string[]]@($lines))
}

if ($Lane) {
	Invoke-Steps @($Lane)
	Save-LaneTimes
	exit $stepsCode
}
Invoke-Steps @('prelude')
if ($stepsCode -eq 0) {
	if ($Concurrent -or $env:MINIMAXON_GATE_CONCURRENT) { Invoke-Lanes } else { Invoke-Steps $laneNames }
}
if ($ownsStepTimes) { Stop-StepTimes }
exit $stepsCode
