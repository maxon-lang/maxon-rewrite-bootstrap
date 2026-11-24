# Regenerates stdlib/ from maxon's standard library, whose stdlib directory is the argument: cmd/gate's, as on Linux, with
# bin\minimaxon.exe, which it builds first when it is not of the sources as they are. `.\stdlib.ps1 --check <directory>` only
# fails when the regeneration differs from what is committed, leaving stdlib/ as it was.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate stdlib @args
exit $LASTEXITCODE
