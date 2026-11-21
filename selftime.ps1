# Times an uncached build of self/ with the Go compiler and appends one row to docs/self-compile-times.md, so the
# compile time of self/ is on record at each stage of the plan. One run: the number is a record, not a benchmark.
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
try {
	Push-Location $Tree
	$compiler = Join-Path $scratch 'minimaxon.exe'
	go build -o $compiler .
	if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
	if ($Commit -eq '') { $Commit = git rev-parse --short HEAD }
	$lines = (Get-ChildItem self -Recurse -Filter *.maxon | Get-Content | Measure-Object -Line).Lines
	# --timings goes to stderr, which Windows PowerShell turns into a terminating error under 'Stop'.
	$ErrorActionPreference = 'Continue'
	$out = & $compiler build self --no-cache --timings -o (Join-Path $scratch 'self.exe') 2>&1 | Out-String
	$ErrorActionPreference = 'Stop'
	if ($LASTEXITCODE -ne 0) { throw "build self failed:`n$out" }
	if ($out -notmatch '(?m)^total\s+([\d.]+) ms') { throw "no total in --timings output:`n$out" }
	$total = [double]$Matches[1]
	if ($out -notmatch 'check ([\d.]+) ms, generate ([\d.]+) ms') { throw "no task times in --timings output:`n$out" }
	$check = [double]$Matches[1]
	$generate = [double]$Matches[2]
} finally {
	Pop-Location
	Remove-Item -Recurse -Force $scratch
}
if (-not (Test-Path $log)) {
	@(
		'# Compile time of `self/`',
		'',
		'One uncached `minimaxon build self --timings` with the Go compiler, one row per stage',
		'(`.\selftime.ps1 -Note <stage>`). Total is wall time; check and generate are task time summed over the modules.',
		'',
		'| Date | Commit | Stage | `self/` lines | Total ms | Check ms | Generate ms |',
		'|---|---|---|---:|---:|---:|---:|'
	) | Set-Content $log
}
$row = '| {0} | {1} | {2} | {3} | {4:0.0} | {5:0.0} | {6:0.0} |' -f (Get-Date -Format 'yyyy-MM-dd'), $Commit, $Note, $lines, $total, $check, $generate
Add-Content $log $row
Write-Output $row
