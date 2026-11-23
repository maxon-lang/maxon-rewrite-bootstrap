# Regenerates specs/ from maxon's spec tests, whose directory is the argument, and writes specs/passed.txt again:
# cmd/gate's, as on Linux. Run the gate first, which builds the compiler it uses.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate specs @args
exit $LASTEXITCODE
