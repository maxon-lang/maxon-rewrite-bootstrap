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
	$output = $null | & $compiler test specs --require-passed 2>&1 | Out-String
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
# which they inherit, can.
function Test-Behaviours {
	$before = $env:MAXON_MAX_PROCS
	foreach ($procs in 1, 2) {
		$env:MAXON_MAX_PROCS = $procs
		try {
			$null | & $compiler test --behaviours @TestArgs
		} finally {
			$env:MAXON_MAX_PROCS = $before
		}
		if ($LASTEXITCODE -ne 0) { return }
	}
}

$steps = @(
	# cmd/convert lays out what it converts with the compiler's fmt.
	@('Go tests', { $env:MINIMAXON = $compiler; go test ./... }),
	@('selftest', { & $compiler selftest }),
	# Nothing compiles the runtime's source until a program is built, so this is what holds it to the language and to
	# the signatures the compiler calls.
	@('runtime', { & $compiler check runtime }),
	# The compiler's own source is a program too: a rule that it breaks would otherwise show only when the seed is renewed.
	@('self', { & $compiler check self --verify-reads }),
	@('layout', { & $compiler fmt --check self runtime tests }),
	# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
	@('corpus', { $null | & $compiler test @TestArgs }),
	@('behaviours', { Test-Behaviours }),
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
