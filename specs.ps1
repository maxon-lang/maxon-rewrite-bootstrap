# Regenerates specs/ from maxon's spec tests: cmd/convert converts their syntax, then the compiler's fix command applies
# the fix of each diagnostic that minimaxon's language differences raise (README, "Where it differs from Maxon"). The
# compiler is bin\c2.exe, the one the self-built compiler builds, so the result does not depend on the seed that built c1.
# Run .\check.ps1 first to build it. The argument is maxon's specs directory.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

$maxonSpecs = if ($args.Count -gt 0) { $args[0] } else { 'C:\Users\Eric\Dev\maxon\specs' }
if (-not (Test-Path bin\c2.exe)) {
	Write-Output 'bin\c2.exe is missing: run .\check.ps1 to build it'
	exit 1
}
Remove-Item specs\*.md -Exclude README.md
go run ./cmd/convert --specs --out=specs $maxonSpecs
if ($LASTEXITCODE -ne 0) {
	exit 1
}
$null | bin\c2.exe fix specs
if ($LASTEXITCODE -ne 0) {
	exit 1
}
