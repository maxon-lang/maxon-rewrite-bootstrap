# Builds the day-to-day compiler, bin\minimaxon.exe: the seed builds C1, and C1 builds self/ again, so that it carries
# today's code generator (cmd/gate's build, which does nothing when it is already that build of the sources as they are).
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot
go run ./cmd/gate build
exit $LASTEXITCODE
