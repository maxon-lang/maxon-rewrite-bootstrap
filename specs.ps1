# Regenerates specs/ from maxon's spec tests: cmd/convert converts their syntax, then the compiler's fix command applies
# the fix of each diagnostic that minimaxon's language differences raise (README, "Where it differs from Maxon"). The
# compiler is bin\minimaxon.exe, the one the gate builds from the seed. Run .\check.ps1 first to build it. The run at
# the end leaves out the tests of specs/passed.txt, which have passed, and adds the new passes to it. The argument is
# maxon's specs directory.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

$maxonSpecs = if ($args.Count -gt 0) { $args[0] } else { 'C:\Users\Eric\Dev\maxon\specs' }
if (-not (Test-Path bin\minimaxon.exe)) {
	Write-Output 'bin\minimaxon.exe is missing: run .\check.ps1 to build it'
	exit 1
}
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
bin\minimaxon.exe test specs --skip-passed --record-passed
if ($LASTEXITCODE -ne 0) {
	exit 1
}
