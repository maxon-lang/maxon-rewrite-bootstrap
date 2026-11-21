# Times a build of self/ three ways and appends one row to docs/self-compile-times.md, so the compile time of self/
# is on record at each stage of the plan: by the seed (seed\minimaxon.exe), by C1 (self/ built by the seed) and by C2
# (self/ built by C1, so compiled by itself and running its own runtime). One run each: the numbers are a record, not
# a benchmark.
#
#   .\selftime.ps1 -Note 'Stage 2.1: maps'
#   .\selftime.ps1 -Note 'backfill' -Tree <another checkout> -Commit <its commit>
param(
	[Parameter(Mandatory)][string]$Note,
	[string]$Tree = $PSScriptRoot,
	[string]$Commit = ''
)
$ErrorActionPreference = 'Stop'
$log = Join-Path $PSScriptRoot 'docs\self-compile-times.md'
# The self-built compilers find runtime/ by walking up from their own executable, so they are built inside the tree.
$selfBuilt = Join-Path $Tree "bin\selftime-$PID"

# Runs a compiler on self/ and returns the wall time in milliseconds, or $null if it cannot build it.
function Measure-SelfBuild([string]$compilerExe, [string]$outExe) {
	$ErrorActionPreference = 'Continue'
	$elapsed = Measure-Command { & $compilerExe build self -o $outExe 2>&1 | Out-Null }
	$ErrorActionPreference = 'Stop'
	if ($LASTEXITCODE -ne 0 -or -not (Test-Path $outExe)) { return $null }
	return $elapsed.TotalMilliseconds
}

function Format-Milliseconds($value) {
	if ($null -eq $value) { return '—' }
	return '{0:0}' -f $value
}

try {
	Push-Location $Tree
	if ($Commit -eq '') { $Commit = git rev-parse --short HEAD }
	$lines = (Get-ChildItem self -Recurse -Filter *.maxon | Get-Content | Measure-Object -Line).Lines
	New-Item -ItemType Directory -Force $selfBuilt | Out-Null
	$c1 = Join-Path $selfBuilt 'c1.exe'
	$c2 = Join-Path $selfBuilt 'c2.exe'
	$bySeed = Measure-SelfBuild (Join-Path $Tree 'seed\minimaxon.exe') $c1
	if ($null -eq $bySeed) { throw 'the seed does not build self/' }
	$byC1 = Measure-SelfBuild $c1 $c2
	$byC2 = $null
	if ($null -ne $byC1) { $byC2 = Measure-SelfBuild $c2 (Join-Path $selfBuilt 'c3.exe') }
} finally {
	Pop-Location
	if (Test-Path $selfBuilt) { Remove-Item -Recurse -Force $selfBuilt }
}
$row = '| {0} | {1} | {2} | {3} | {4} | {5} | {6} |' -f (Get-Date -Format 'yyyy-MM-dd'), $Commit, $Note, $lines, (Format-Milliseconds $bySeed), (Format-Milliseconds $byC1), (Format-Milliseconds $byC2)
Add-Content $log $row
Write-Output $row
