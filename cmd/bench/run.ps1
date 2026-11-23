# Runs the benchmarks of a batch that `bench build` made for Windows and writes what they measured to one results file, for
# `bench report`; run.sh is the same for Linux and macOS. The peak memory is the process's peak working set.
#
#   .\run.ps1
#   .\run.ps1 -Rounds 3 -Results C:\temp\results.txt
#
# Each round runs every program once with minimaxon and once with Go, and the one that goes first alternates from round to
# round. The results file is, by default, ..\..\out\bench-<target>.txt, relative to this directory.
param(
	[ValidateRange(1, 1000)][int]$Rounds = $(if ($env:BENCH_ROUNDS) { [int]$env:BENCH_ROUNDS } else { 7 }),
	[string]$Results = $env:BENCH_RESULTS
)
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$kilobyte = 1024
$manifest = @(Get-Content (Join-Path $here 'manifest.txt') | ForEach-Object { , ($_ -split "`t") })
$target = ($manifest | Where-Object { $_[0] -eq 'target' })[1]
$programs = @($manifest | Where-Object { $_[0] -eq 'program' } | ForEach-Object { $_[1] })
if (-not $Results) { $Results = Join-Path $here "..\..\out\bench-$target.txt" }
$null = New-Item -ItemType Directory -Force (Split-Path -Parent $Results)
$Results = [System.IO.Path]::GetFullPath($Results)
$lines = New-Object System.Collections.Generic.List[string]

Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

// ProcessMemory reads a process's peak working set from its handle, which still answers after the process has ended.
public static class ProcessMemory {
	[StructLayout(LayoutKind.Sequential)]
	struct Counters {
		public uint cb, pageFaultCount;
		public UIntPtr peakWorkingSetSize, workingSetSize, quotaPeakPagedPoolUsage, quotaPagedPoolUsage,
			quotaPeakNonPagedPoolUsage, quotaNonPagedPoolUsage, pagefileUsage, peakPagefileUsage;
	}

	[DllImport("psapi.dll", SetLastError = true)]
	static extern bool GetProcessMemoryInfo(IntPtr process, out Counters counters, uint size);

	// PeakBytes is the peak working set of the process in bytes.
	public static ulong PeakBytes(IntPtr process) {
		Counters counters;
		if (!GetProcessMemoryInfo(process, out counters, (uint)Marshal.SizeOf(typeof(Counters)))) {
			throw new System.ComponentModel.Win32Exception();
		}
		return counters.peakWorkingSetSize.ToUInt64();
	}
}
'@

function Add-Line([string[]]$fields) { $lines.Add($fields -join "`t") }

Add-Line 'meta', 'uname', ([System.Environment]::OSVersion.VersionString + ' ' + [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)
Add-Line 'meta', 'cpu', (Get-CimInstance Win32_Processor | Select-Object -First 1).Name.Trim()
Add-Line 'meta', 'cores', ([System.Environment]::ProcessorCount)
Add-Line 'meta', 'rounds', $Rounds
Add-Line 'meta', 'date', (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
Add-Line 'meta', 'peak-memory', 'recorded as the process''s peak working set'
foreach ($row in $manifest) {
	if ($row[0] -in 'target', 'go', 'compiler') { Add-Line 'meta', $row[0], $row[1] }
	if ($row[0] -eq 'expect') { Add-Line $row }
}
foreach ($impl in 'minimaxon', 'go') {
	foreach ($name in $programs) {
		Add-Line 'size', $impl, $name, (Get-Item (Join-Path $here "$impl\$name.exe")).Length
	}
}

# Invoke-One runs one executable and records what it measured.
function Invoke-One([string]$impl, [string]$name, [int]$round) {
	$info = New-Object System.Diagnostics.ProcessStartInfo (Join-Path $here "$impl\$name.exe")
	$info.RedirectStandardOutput = $true
	$info.UseShellExecute = $false
	$clock = [System.Diagnostics.Stopwatch]::StartNew()
	$process = [System.Diagnostics.Process]::Start($info)
	$handle = $process.Handle
	$output = $process.StandardOutput.ReadToEnd()
	$process.WaitForExit()
	$clock.Stop()
	$rss = [int64]([ProcessMemory]::PeakBytes($handle) / $kilobyte)
	Add-Line 'exit', $impl, $name, $round, $process.ExitCode
	Add-Line 'wall', $impl, $name, $round, ([int64]($clock.ElapsedTicks * (1e9 / [System.Diagnostics.Stopwatch]::Frequency)))
	Add-Line 'rss', $impl, $name, $round, $rss
	foreach ($line in $output -split "`r?`n") {
		if ($line -eq '') { continue }
		if ($line -match '^(.*) ns (\d+)$') {
			Add-Line 'phase', $impl, $name, $round, $Matches[1], $Matches[2]
		} elseif ($round -eq 1) {
			Add-Line 'result', $impl, $name, $line
		}
	}
}

for ($round = 1; $round -le $Rounds; $round++) {
	$order = if ($round % 2 -eq 1) { 'minimaxon', 'go' } else { 'go', 'minimaxon' }
	foreach ($name in $programs) {
		[Console]::Error.WriteLine("round ${round}/${Rounds}: $name")
		foreach ($impl in $order) { Invoke-One $impl $name $round }
	}
}
[System.IO.File]::WriteAllText($Results, ($lines -join "`n") + "`n", (New-Object System.Text.UTF8Encoding $false))
[Console]::Error.WriteLine("results: $Results")
