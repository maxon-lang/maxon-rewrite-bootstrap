# The suites a compiler must pass, with the compiler at the first argument and the arguments after it going to
# `minimaxon test`: cmd/gate's, which the gate and the seed renewal run.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate suites @args
exit $LASTEXITCODE
