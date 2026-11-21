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

# Until the parallel driver of Phase 3 makes the full run fast, only the spec tests that specs/passed.txt does not list
# run, and those of them that pass now are reported (the line "NEW file.md: test") so that the list can be extended.
function Test-Specs {
	$output = $null | & $compiler test specs --skip-passed 2>&1 | Out-String
	if ($output -notmatch '(?m)^(\d+) passed, \d+ failed, \d+ skipped') {
		Write-Output $output
		Write-Output 'the spec runner printed no count'
		$global:LASTEXITCODE = 1
		return
	}
	$new = ([regex]::Matches($output, '(?m)^NEW ')).Count
	Write-Output "specs: $($Matches[0]); $new new, to add to specs/passed.txt with: bin\minimaxon.exe test specs --skip-passed --record-passed"
	$global:LASTEXITCODE = 0
}

$steps = @(
	# cmd/convert lays out what it converts with the compiler's fmt.
	@('Go tests', { $env:MINIMAXON = $compiler; go test ./... }),
	@('selftest', { & $compiler selftest }),
	# Nothing compiles the runtime's source until a program is built, so this is what holds it to the language and to
	# the signatures the compiler calls.
	@('runtime', { & $compiler check runtime }),
	@('layout', { & $compiler fmt --check self runtime tests }),
	# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
	@('corpus', { $null | & $compiler test @TestArgs }),
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
