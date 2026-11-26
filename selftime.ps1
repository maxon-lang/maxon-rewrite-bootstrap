# Times a build of self/ three ways and appends one row to docs/self-compile-times.md, so the compile time and the
# memory use of self/ are on record at each stage of the plan: by the seed (seed\minimaxon.exe), by C1 (self/ built by
# the seed) and by C2 (self/ built by C1, so compiled by itself and running its own runtime). One run each, without the
# build cache: the numbers are a record, not a benchmark. The row also holds the size in bytes of the code (.text) of
# C2, the compiler the current code generator made, and the two peaks of C2's own build of self/: the most physical
# memory it held at once (working set) and the most it had committed.
#
#   .\selftime.ps1 -Note 'Stage 2.1: maps'
#   .\selftime.ps1 -Note 'backfill' -Tree <another checkout> -Commit <its commit>
param(
	[Parameter(Mandatory)][string]$Note,
	[string]$Tree = $PSScriptRoot,
	[string]$Commit = ''
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\textsize.ps1"
$log = Join-Path $PSScriptRoot 'docs\self-compile-times.md'
# The self-built compilers find runtime/ by walking up from their own executable, so they are built inside the tree.
$selfBuilt = Join-Path $Tree "bin\selftime-$PID"
$bytesPerMegabyte = 1MB

# .NET reads a process's peaks by its id, which fails once it has exited; the handle still answers.
Add-Type -Namespace SelfTime -Name Memory -MemberDefinition @'
[StructLayout(LayoutKind.Sequential)]
public struct Counters {
	public uint cb; public uint PageFaultCount;
	public UIntPtr PeakWorkingSetSize; public UIntPtr WorkingSetSize;
	public UIntPtr QuotaPeakPagedPoolUsage; public UIntPtr QuotaPagedPoolUsage;
	public UIntPtr QuotaPeakNonPagedPoolUsage; public UIntPtr QuotaNonPagedPoolUsage;
	public UIntPtr PagefileUsage; public UIntPtr PeakPagefileUsage;
}
[DllImport("kernel32.dll", SetLastError = true)]
public static extern bool K32GetProcessMemoryInfo(IntPtr process, out Counters counters, uint size);
'@

# Runs a compiler on self/ and returns its wall time in milliseconds and its peak working set and peak commit in
# bytes, or $null if it cannot build it.
function Measure-SelfBuild([string]$compilerExe, [string]$outExe) {
	$start = [Diagnostics.ProcessStartInfo]::new($compilerExe)
	foreach ($argument in 'build', 'self', '-o', $outExe) { $start.ArgumentList.Add($argument) }
	$start.EnvironmentVariables['MINIMAXON_CACHE'] = 'off'
	$start.WorkingDirectory = $Tree
	$start.UseShellExecute = $false
	$start.RedirectStandardOutput = $true
	$start.RedirectStandardError = $true
	$watch = [Diagnostics.Stopwatch]::StartNew()
	$process = [Diagnostics.Process]::Start($start)
	try {
		$handle = $process.Handle
		$out = $process.StandardOutput.ReadToEndAsync()
		$err = $process.StandardError.ReadToEndAsync()
		$process.WaitForExit()
		$watch.Stop()
		[void]$out.Result
		[void]$err.Result
		if ($process.ExitCode -ne 0 -or -not (Test-Path $outExe)) { return $null }
		$counters = [SelfTime.Memory+Counters]::new()
		$size = [Runtime.InteropServices.Marshal]::SizeOf($counters)
		if (-not [SelfTime.Memory]::K32GetProcessMemoryInfo($handle, [ref]$counters, $size)) {
			throw "cannot read the memory counters of $compilerExe"
		}
		return [pscustomobject]@{
			Milliseconds   = $watch.Elapsed.TotalMilliseconds
			PeakWorkingSet = $counters.PeakWorkingSetSize.ToUInt64()
			PeakCommit     = $counters.PeakPagefileUsage.ToUInt64()
		}
	} finally {
		$process.Dispose()
	}
}

function Format-Milliseconds($value) {
	if ($null -eq $value) { return '—' }
	return '{0:0}' -f $value
}

function Format-Megabytes($bytes) {
	if ($null -eq $bytes) { return '—' }
	return '{0:0.0}' -f ($bytes / $bytesPerMegabyte)
}

. "$PSScriptRoot\lock.ps1"
Enter-HeavyLock
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
	$textBytes = $null
	if ($null -ne $byC1) {
		$byC2 = Measure-SelfBuild $c2 (Join-Path $selfBuilt 'c3.exe')
		$textBytes = Get-TextSize $c2
	}
} finally {
	Pop-Location
	if (Test-Path $selfBuilt) { Remove-Item -Recurse -Force $selfBuilt }
	Exit-HeavyLock
}
$row = '| {0} | {1} | {2} | {3} | {4} | {5} | {6} | {7} | {8} | {9} |' -f (Get-Date -Format 'yyyy-MM-dd'), $Commit, $Note, $lines, (Format-Milliseconds (${bySeed}?.Milliseconds)), (Format-Milliseconds (${byC1}?.Milliseconds)), (Format-Milliseconds (${byC2}?.Milliseconds)), (Format-Milliseconds $textBytes), (Format-Megabytes (${byC2}?.PeakWorkingSet)), (Format-Megabytes (${byC2}?.PeakCommit))
Add-Content $log $row
Write-Output $row
