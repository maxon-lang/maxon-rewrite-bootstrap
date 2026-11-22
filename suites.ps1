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

# The corpus runs with the machine's worker count; the programs that schedule behaviours then run again, natively, under
# one worker and under two. The runner cannot give the executables it starts another worker count, but its environment,
# which they inherit, can: WSL passes on only the variables that WSLENV names, so a Linux run names this one.
function Test-Behaviours([string[]]$TargetArgs = @()) {
	$before = $env:MAXON_MAX_PROCS
	$beforeWsl = $env:WSLENV
	$env:WSLENV = if ($beforeWsl) { "${beforeWsl}:MAXON_MAX_PROCS" } else { 'MAXON_MAX_PROCS' }
	try {
		foreach ($procs in 1, 2) {
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
# the same programs. A machine without WSL or a distribution fails the step: a gate that skipped it would pass without
# running Linux's code.
$linuxTarget = '--target=x64-linux'

function Assert-Wsl {
	if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
		Write-Output 'the x64-linux corpus runs under WSL, and wsl.exe is not installed: install WSL (wsl --install)'
		$global:LASTEXITCODE = 1
		return
	}
	$null = wsl.exe --exec /bin/true 2>&1
	if ($LASTEXITCODE -ne 0) {
		Write-Output 'the x64-linux corpus runs under WSL, and its default distribution cannot run a program: install one (wsl --install -d Ubuntu) and start it once'
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
	@('runtime', { & $compiler check runtime; if ($LASTEXITCODE -eq 0) { & $compiler check runtime --target=x64-linux } }),
	# A program compiles only the library modules it names, so this is what holds the whole standard library to the language,
	# for each target.
	@('stdlib', { & $compiler check stdlib; if ($LASTEXITCODE -eq 0) { & $compiler check stdlib --target=x64-linux } }),
	# The compiler's own source is a program too: a rule that it breaks would otherwise show only when the seed is renewed.
	@('self', { & $compiler check self --tests --verify-reads }),
	@('layout', { & $compiler fmt --check self runtime tests stdlib }),
	# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
	@('corpus', { $null | & $compiler test @TestArgs }),
	@('behaviours', { Test-Behaviours }),
	@('corpus on linux', { Test-Linux }),
	@('behaviours on linux', { Test-LinuxBehaviours }),
	@('cache', { Test-Cache }),
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
	if ($name -eq 'specs' -and $output) { $output | Write-Output }
}
