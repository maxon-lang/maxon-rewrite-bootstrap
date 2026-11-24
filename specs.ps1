# Regenerates specs/ from maxon's spec tests, whose directory is the argument, and writes specs/passed.txt again:
# cmd/gate's, as on Linux, with bin\minimaxon.exe, which it builds first when it is not of the sources as they are.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate specs @args
exit $LASTEXITCODE
