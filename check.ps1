# The gate: every check AGENTS.md requires, stopping at the first that fails. Its steps are cmd/gate's, which ./check.sh
# runs on Linux, so the two hosts check the same things (README, "Tests"). Arguments go to `minimaxon test`.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate check @args
exit $LASTEXITCODE
