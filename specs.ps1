# Regenerates specs/ from maxon's spec tests: cmd/convert converts their syntax, then the compiler's fix command applies
# the fix of each diagnostic that minimaxon's language differences raise (README, "Where it differs from Maxon"). The
# compiler is bin\minimaxon.exe, the one the gate builds from the seed. Run .\check.ps1 first to build it. The run at
# the end runs every test and writes specs/passed.txt again: the new passes are added, a listed test that the conversion
# now skips is dropped (the line "DROPPED"), and a listed test that fails stays, so the gate goes on failing until it is
# fixed. The argument is maxon's specs directory.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

. "$PSScriptRoot\lock.ps1"
# The lock is held to the end of the script (lock.ps1); the body below is not indented, so that the script reads as it did.
Enter-HeavyLock
try {
$maxonSpecs = if ($args.Count -gt 0) { $args[0] } else { 'C:\Users\Eric\Dev\maxon\specs' }
if (-not (Test-Path bin\minimaxon.exe)) {
	Write-Output 'bin\minimaxon.exe is missing: run .\check.ps1 to build it'
	exit 1
}
# The conversion and the fix are skipped when what they read and what they wrote are as they were when they last ended: maxon's
# spec files, the converter's source, and the compiler that formats and fixes with the runtime and the library it finds beside
# itself and the skip table, and the specs they wrote, which anything may have edited since. Whatever differs, or if the last
# conversion did not end, they run, and the stamp is written last.
. "$PSScriptRoot\stamps.ps1"
$conversionStamp = Join-Path $PSScriptRoot 'bin\specs-conversion.stamp'
$specsDir = Join-Path $PSScriptRoot 'specs'
$conversionInputs = $maxonSpecs, (Join-Path $PSScriptRoot 'cmd\convert'), (Join-Path $PSScriptRoot 'go.mod'), (Join-Path $PSScriptRoot 'bin\minimaxon.exe'),
	(Join-Path $PSScriptRoot 'runtime'), (Join-Path $PSScriptRoot 'stdlib'), (Join-Path $PSScriptRoot 'self\driver\skips.tsv')
function Get-ConversionStamp {
	$written = Get-PathsDigest @($specsDir) -Exclude @(Join-Path $specsDir 'passed.txt')
	return "inputs $(Get-PathsDigest $conversionInputs)`nspecs $written"
}
$stamp = Get-ConversionStamp
if (Test-Stamp $conversionStamp $stamp) {
	Write-Output "specs: conversion and fix skipped, the inputs and the specs are those of the last ones (stamp $(Get-StampId $stamp))"
} else {
	Remove-Item $conversionStamp -ErrorAction SilentlyContinue
	# The conversion lays out what it writes with the same compiler, not with whatever minimaxon.exe is in the root.
	$env:MINIMAXON = Join-Path $PSScriptRoot 'bin\minimaxon.exe'
	Remove-Item specs\*.md -Exclude README.md
	go run ./cmd/convert --specs --out=specs $maxonSpecs
	if ($LASTEXITCODE -ne 0) {
		exit 1
	}
	$null | bin\minimaxon.exe fix specs
	if ($LASTEXITCODE -ne 0) {
		exit 1
	}
	# The inputs are read again: a change while the conversion ran leaves it with no stamp.
	$after = Get-ConversionStamp
	if ($after.Split("`n")[0] -ceq $stamp.Split("`n")[0]) {
		[IO.File]::WriteAllText($conversionStamp, $after)
	}
}
& "$PSScriptRoot\spec-run.ps1" bin\minimaxon.exe --record-passed
if ($LASTEXITCODE -ne 0) {
	exit 1
}
} finally {
	Exit-HeavyLock
}
