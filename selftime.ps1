# Times a build of self/ three ways and appends one row to docs/self-compile-times.md, so the compile time of self/
# is on record at each stage of the plan: by the Go compiler, by C1 (self/ built by the Go compiler) and by C2 (self/
# built by C1, so compiled by itself and running its own runtime). One run each: the numbers are a record, not a
# benchmark.
#
#   .\selftime.ps1 -Note 'Stage 1.2a: modules'
#   .\selftime.ps1 -Note 'backfill' -Tree <another checkout> -Commit <its commit>
param(
	[Parameter(Mandatory)][string]$Note,
	[string]$Tree = $PSScriptRoot,
	[string]$Commit = ''
)
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
$log = Join-Path $PSScriptRoot 'docs\self-compile-times.md'
$scratch = Join-Path ([IO.Path]::GetTempPath()) "selftime-$PID"
New-Item -ItemType Directory -Force $scratch | Out-Null
# The self-built compilers find runtime/ by walking up from their own executable, so they are built inside the tree.
$selfBuilt = Join-Path $Tree "bin\selftime-$PID"

# Runs a self-built compiler on self/ and returns the wall time in milliseconds, or $null if it cannot build yet.
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
	$compiler = Join-Path $scratch 'minimaxon.exe'
	go build -o $compiler .
	if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
	if ($Commit -eq '') { $Commit = git rev-parse --short HEAD }
	$lines = (Get-ChildItem self -Recurse -Filter *.maxon | Get-Content | Measure-Object -Line).Lines
	New-Item -ItemType Directory -Force $selfBuilt | Out-Null
	$c1 = Join-Path $selfBuilt 'c1.exe'
	# --timings goes to stderr, which Windows PowerShell turns into a terminating error under 'Stop'.
	$ErrorActionPreference = 'Continue'
	$out = & $compiler build self --no-cache --timings -o $c1 2>&1 | Out-String
	$ErrorActionPreference = 'Stop'
	if ($LASTEXITCODE -ne 0) { throw "build self failed:`n$out" }
	if ($out -notmatch '(?m)^total\s+([\d.]+) ms') { throw "no total in --timings output:`n$out" }
	$total = [double]$Matches[1]
	# The task-time line lists the stages by name; its other stages vary with the compiler's version.
	if ($out -notmatch ' check ([\d.]+) ms') { throw "no check time in --timings output:`n$out" }
	$check = [double]$Matches[1]
	if ($out -notmatch ' generate ([\d.]+) ms') { throw "no generate time in --timings output:`n$out" }
	$generate = [double]$Matches[1]
	$c2 = Join-Path $selfBuilt 'c2.exe'
	$byC1 = Measure-SelfBuild $c1 $c2
	$byC2 = $null
	if ($null -ne $byC1) { $byC2 = Measure-SelfBuild $c2 (Join-Path $selfBuilt 'c3.exe') }
} finally {
	Pop-Location
	Remove-Item -Recurse -Force $scratch
	if (Test-Path $selfBuilt) { Remove-Item -Recurse -Force $selfBuilt }
}
if (-not (Test-Path $log)) {
	@(
		'# Compile time of `self/`',
		'',
		'One uncached build of `self/` per row (`.\selftime.ps1 -Note <stage>`). Total is the Go compiler''s wall time',
		'(`minimaxon build self --timings`); check and generate are its task time summed over the modules. C1 is `self/`',
		'built by the Go compiler and C2 is `self/` built by C1; their columns are the wall time of each building `self/`.',
		'',
		'| Date | Commit | Stage | `self/` lines | Total ms | Check ms | Generate ms | C1 ms | C2 ms |',
		'|---|---|---|---:|---:|---:|---:|---:|---:|'
	) | Set-Content $log
}
$row = '| {0} | {1} | {2} | {3} | {4:0.0} | {5:0.0} | {6:0.0} | {7} | {8} |' -f (Get-Date -Format 'yyyy-MM-dd'), $Commit, $Note, $lines, $total, $check, $generate, (Format-Milliseconds $byC1), (Format-Milliseconds $byC2)
Add-Content $log $row
Write-Output $row
