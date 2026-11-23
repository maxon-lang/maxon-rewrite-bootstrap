# Runs every suite that holds a compiler to the repository's rules, with the compiler at -Compiler: the Go tests of the
# generators and tools, then the compiler's own suites. The gate (check.ps1) runs it on the compiler it builds from the
# seed, and the seed renewal (seed.ps1) on the compiler that would become the seed, so the two cannot disagree about
# what a compiler must pass. Arguments after -Compiler go to `minimaxon test`. Stops at the first failing suite.
param(
	[Parameter(Mandatory)][string]$Compiler,
	[Parameter(ValueFromRemainingArguments)]$TestArgs
)
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
. "$PSScriptRoot\hashseed.ps1"
$compiler = (Resolve-Path $Compiler).Path

# Every spec test runs, and the step fails when a test that specs/passed.txt lists does not pass (the line "LOST file.md:
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
	if ($code -ne 0) {
		[regex]::Matches($output, '(?m)^(LOST [^\r\n]*|\d+ tests of [^\r\n]* did not pass)') | ForEach-Object { Write-Output $_.Value }
		Write-Output "specs: $counts"
		$global:LASTEXITCODE = 1
		return
	}
	Write-Output "specs: $counts; $new new, to add to specs/passed.txt with: bin\minimaxon.exe test specs --record-passed"
	$global:LASTEXITCODE = 0
}

# The worker counts that the programs that schedule behaviours run under again, natively, after the corpus has run with the
# machine's: a bug of the runtime's memory ordering showed only at four workers, so the counts reach past two.
$behaviourWorkers = 1, 2, 4

# The corpus runs with the machine's worker count; the programs that schedule behaviours then run again, natively, under
# each of $behaviourWorkers. The runner cannot give the executables it starts another worker count, but its environment,
# which they inherit, can: WSL passes on only the variables that WSLENV names, so a Linux run names this one.
function Test-Behaviours([string[]]$TargetArgs = @()) {
	$before = $env:MAXON_MAX_PROCS
	$beforeWsl = $env:WSLENV
	$env:WSLENV = if ($beforeWsl) { "${beforeWsl}:MAXON_MAX_PROCS" } else { 'MAXON_MAX_PROCS' }
	try {
		foreach ($procs in $behaviourWorkers) {
			$env:MAXON_MAX_PROCS = $procs
			$null | & $compiler test --behaviours @TargetArgs @TestArgs
			if ($LASTEXITCODE -ne 0) { return }
		}
	} finally {
		$env:MAXON_MAX_PROCS = $before
		$env:WSLENV = $beforeWsl
	}
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
	$before = $env:MAXON_MAX_PROCS
	$behaviours = 0
	try {
		foreach ($procs in $behaviourWorkers) {
			$env:MAXON_MAX_PROCS = $procs
			$null | & $compiler test --behaviours $macTarget $runner @TestArgs
			if ($LASTEXITCODE -ne 0) { $behaviours = $LASTEXITCODE }
		}
	} finally {
		$env:MAXON_MAX_PROCS = $before
	}
	return [Math]::Max($corpus, $behaviours)
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
# with and without it, and self for this one, with every span verified (--verify-ir), and the executables compared. A stack
# overflow's fault line, whose address is in a function of one statement, is then symbolized to that statement on x64
# Windows (a copy of the executable that the loader keeps at its image base), x64 Linux and arm64 Linux (under WSL and its
# qemu).
$debugInfoTargets = 'x64-windows', 'x64-linux', 'arm64-linux', 'arm64-macos'
$debugInfoPrograms = 'tests\run\arithmetic.maxon', 'tests\run\array-methods.maxon', 'tests\run\generics.maxon'
$overflowLine = 'overflow.maxon:3:2  (in export::down)'
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
		foreach ($t in $debugInfoTargets) {
			foreach ($p in @($overflow) + $debugInfoPrograms) {
				Same $p @("--target=$t") "$([IO.Path]::GetFileNameWithoutExtension($p))-$t"
			}
		}
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

$steps = @(
	# cmd/convert lays out what it converts with the compiler's fmt.
	@('Go tests', { $env:MINIMAXON = $compiler; go test ./... }),
	@('selftest', { & $compiler selftest }),
	# Nothing compiles the runtime's source until a program is built, so this is what holds it to the language and to
	# the signatures the compiler calls, for each target, whose branches of the source differ.
	@('runtime', { & $compiler check runtime; foreach ($t in '--target=x64-linux', '--target=arm64-linux', '--target=arm64-macos') { if ($LASTEXITCODE -eq 0) { & $compiler check runtime $t } } }),
	# A program compiles only the library modules it names, so this is what holds the whole standard library to the language,
	# for each target.
	@('stdlib', { & $compiler check stdlib; foreach ($t in '--target=x64-linux', '--target=arm64-linux', '--target=arm64-macos') { if ($LASTEXITCODE -eq 0) { & $compiler check stdlib $t } } }),
	# The compiler's own source is a program too: a rule that it breaks would otherwise show only when the seed is renewed.
	@('self', { & $compiler check self --tests --verify-reads }),
	@('layout', { & $compiler fmt --check self runtime tests stdlib }),
	# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
	@('corpus', { $null | & $compiler test @TestArgs }),
	@('behaviours', { Test-Behaviours }),
	@('corpus on linux', { Test-Linux }),
	@('behaviours on linux', { Test-LinuxBehaviours }),
	@('corpus on arm64 linux', { Test-Arm64Linux }),
	@('behaviours on arm64 linux', { Test-Arm64LinuxBehaviours }),
	@('corpus on arm64 macos', { Test-Mac }),
	@('benchmarks compile', { Test-BenchmarksCompile }),
	@('self compiles for arm64', { Test-SelfCompilesForArm64 }),
	@('cache', { Test-Cache }),
	@('debug info', { Test-DebugInfo }),
	@('specs', { Test-Specs }),
	# Every program the generator writes that the checker accepts must run clean under the interpreter: a violation is a
	# hole in the checker.
	@('fuzz', { & $compiler fuzz })
)
foreach ($step in $steps) {
	$name, $run = $step
	$output = & $run 2>&1
	if ($LASTEXITCODE -ne 0) {
		$output | Write-Output
		Write-Output "check failed at: $name"
		exit 1
	}
	if ($name -in 'specs', 'corpus on arm64 macos' -and $output) { $output | Write-Output }
}
