# Builds the day-to-day compiler, minimaxon.exe in the root of the repository, from self/: the seed builds C1 into bin\,
# and C1 builds self/ again, so the compiler is self/ compiled by itself. It finds runtime/ by walking up from its own
# executable, so it stays in the tree.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$null = New-Item -ItemType Directory -Force bin
seed\minimaxon.exe build self -o bin\c1.exe
if ($LASTEXITCODE -ne 0) { exit 1 }
bin\c1.exe build self -o minimaxon.exe
if ($LASTEXITCODE -ne 0) { exit 1 }
