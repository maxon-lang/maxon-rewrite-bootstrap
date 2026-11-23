# Builds the day-to-day compiler, minimaxon.exe in the root of the repository, from self/: the seed builds C1 into bin\,
# and C1 builds self/ again. Two steps, not one, because the seed's code generator can be several features behind
# self/'s, and the compiler in the root should carry today's. It finds runtime/ by walking up from its own executable,
# so it stays in the tree.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

. "$PSScriptRoot\lock.ps1"
# The lock is held to the end of the script (lock.ps1); the body below is not indented, so that the script reads as it did.
Enter-HeavyLock
try {
$null = New-Item -ItemType Directory -Force bin
seed\minimaxon.exe build self -o bin\c1.exe
if ($LASTEXITCODE -ne 0) { exit 1 }
bin\c1.exe build self -o minimaxon.exe
if ($LASTEXITCODE -ne 0) { exit 1 }
} finally {
	Exit-HeavyLock
}
