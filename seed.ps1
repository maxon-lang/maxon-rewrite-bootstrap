# Renews the three seeds from self/, each proven to reproduce itself (seed/README.md): cmd/gate's renewal, which runs here,
# on x64 Windows with WSL. .\seed.ps1 --staged renews them for a change of a layout that the seed bakes in.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate seed @args
exit $LASTEXITCODE
