# Builds each program of tests\bench with a compiler and reports, as a table, the size of the executable's code (.text, in
# bytes), the time of its work and the time of its process, each the best of a number of runs with the spread of the runs
# (the slowest less the best), in milliseconds, so a change to the code generator or the optimizer shows in the numbers it
# is for. A program times its own work with Clock.nowNanos and prints it last, as a `work ns` line; the rest of its output
# must be the lines of its `//>` directives: a result that differs is reported as wrong and the program has no time.
#
# The process is timed by a launcher that adds next to nothing to it: on Windows CreateProcess, with the output going to a
# file, and the wait for the process to end; for x64-linux a shell in WSL's default distribution, which runs a copy of the
# program from the distribution's own /tmp, so neither the start of WSL nor its view of the Windows drives is timed.
#
#   .\bench.ps1
#   .\bench.ps1 -Compiler minimaxon.exe -Target x64-linux -Runs 11
param(
	[string]$Compiler = 'bin\minimaxon.exe',
	[ValidateSet('x64-windows', 'x64-linux')][string]$Target = 'x64-windows',
	[ValidateRange(1, 1000)][int]$Runs = 7
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\textsize.ps1"
$compilerExe = [System.IO.Path]::GetFullPath($Compiler, $PSScriptRoot)
$work = Join-Path ([System.IO.Path]::GetTempPath()) "minimaxon-bench-$PID"
$workLine = '^work ns (\d+)$'
$nanosPerMilli = 1e6

Add-Type -TypeDefinition @'
using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;

// BenchLauncher runs a program with its standard output and error in a file and gives the wall time from its creation to
// the end of the wait for it, in nanoseconds.
public static class BenchLauncher {
	[StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
	struct StartupInfo {
		public int cb;
		public IntPtr reserved, desktop, title;
		public int x, y, xSize, ySize, xCountChars, yCountChars, fillAttribute, flags;
		public short showWindow, reserved2Size;
		public IntPtr reserved2, stdInput, stdOutput, stdError;
	}

	[StructLayout(LayoutKind.Sequential)]
	struct ProcessInformation {
		public IntPtr process, thread;
		public int processId, threadId;
	}

	const int useStdHandles = 0x100;
	const int handleFlagInherit = 1;
	const uint infinite = 0xFFFFFFFF;

	[DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
	static extern bool CreateProcessW(string application, System.Text.StringBuilder commandLine, IntPtr processAttributes, IntPtr threadAttributes, bool inheritHandles, int creationFlags, IntPtr environment, string directory, ref StartupInfo startup, out ProcessInformation info);

	[DllImport("kernel32.dll", SetLastError = true)]
	static extern bool SetHandleInformation(IntPtr handle, int mask, int flags);

	[DllImport("kernel32.dll")]
	static extern uint WaitForSingleObject(IntPtr handle, uint milliseconds);

	[DllImport("kernel32.dll")]
	static extern bool GetExitCodeProcess(IntPtr process, out int code);

	[DllImport("kernel32.dll")]
	static extern bool CloseHandle(IntPtr handle);

	// Run gives the wall time of a run of exe in nanoseconds and its exit code.
	public static long[] Run(string exe, string outputPath) {
		using (FileStream output = new FileStream(outputPath, FileMode.Create, FileAccess.Write, FileShare.ReadWrite)) {
			IntPtr handle = output.SafeFileHandle.DangerousGetHandle();
			if (!SetHandleInformation(handle, handleFlagInherit, handleFlagInherit)) {
				throw new System.ComponentModel.Win32Exception();
			}
			StartupInfo startup = new StartupInfo();
			startup.cb = Marshal.SizeOf(typeof(StartupInfo));
			startup.flags = useStdHandles;
			startup.stdOutput = handle;
			startup.stdError = handle;
			ProcessInformation info;
			long started = Stopwatch.GetTimestamp();
			if (!CreateProcessW(exe, new System.Text.StringBuilder("\"" + exe + "\""), IntPtr.Zero, IntPtr.Zero, true, 0, IntPtr.Zero, null, ref startup, out info)) {
				throw new System.ComponentModel.Win32Exception();
			}
			WaitForSingleObject(info.process, infinite);
			long ended = Stopwatch.GetTimestamp();
			int code;
			GetExitCodeProcess(info.process, out code);
			CloseHandle(info.thread);
			CloseHandle(info.process);
			return new long[] { (long)((ended - started) * (1e9 / Stopwatch.Frequency)), code };
		}
	}
}
'@

# Format-Best is the best of a run's times in milliseconds and the spread of them.
function Format-Best([double[]]$nanos) {
	$sorted = $nanos | Sort-Object
	return '{0:0.0} (+{1:0.0})' -f ($sorted[0] / $nanosPerMilli), (($sorted[-1] - $sorted[0]) / $nanosPerMilli)
}

# Split-Run is a run's output: the result lines and the time of the work, which is $null when it printed none.
function Split-Run([string[]]$lines) {
	$result = @()
	$workNanos = $null
	foreach ($line in $lines) {
		if ($line -match $workLine) { $workNanos = [double]$Matches[1] } else { $result += $line }
	}
	return @{ Result = ($result -join "`n"); Work = $workNanos }
}

# Invoke-WindowsRuns runs exe Runs times and gives each run's output lines and process time.
function Invoke-WindowsRuns([string]$exe) {
	$outputPath = "$exe.out"
	for ($run = 0; $run -lt $Runs; $run++) {
		$timed = [BenchLauncher]::Run($exe, $outputPath)
		@{ Lines = @(Get-Content $outputPath); Process = [double]$timed[0]; Code = $timed[1] }
	}
}

# Invoke-LinuxRuns copies exe into the distribution's /tmp and runs it there Runs times, giving each run's output lines and
# process time. Each run's output is framed by a line that starts it and the line of its exit code and its time.
function Invoke-LinuxRuns([string]$exe) {
	$name = Split-Path -Leaf $exe
	$from = (wsl.exe --exec wslpath -a ($exe -replace '\\', '/')).Trim()
	$dir = "/tmp/minimaxon-bench-$PID"
	$script = 'set -e; mkdir -p "$1"; cp "$2" "$1/$3"; chmod +x "$1/$3"; cd "$1"; set +e; ' +
		'for i in $(seq "$4"); do echo "@run"; s=$EPOCHREALTIME; "./$3" > out 2>&1; c=$?; e=$EPOCHREALTIME; cat out; ' +
		'echo "@end $c $(( ${e/[.,]/} - ${s/[.,]/} ))"; done; rm -rf "$1"'
	$lines = @(wsl.exe --exec bash -c $script bench $dir $from $name $Runs)
	$current = $null
	foreach ($line in $lines) {
		if ($line -eq '@run') { $current = @(); continue }
		if ($line -match '^@end (\d+) (\d+)$') {
			@{ Lines = $current; Process = [double]$Matches[2] * 1000; Code = [int]$Matches[1] }
			continue
		}
		$current += $line
	}
}

$extension = if ($Target -eq 'x64-linux') { '' } else { '.exe' }
New-Item -ItemType Directory -Force $work | Out-Null
$rows = @()
try {
	foreach ($source in Get-ChildItem (Join-Path $PSScriptRoot 'tests\bench') -Filter *.maxon | Sort-Object Name) {
		$exe = Join-Path $work ($source.BaseName + $extension)
		& $compilerExe build $source.FullName "--target=$Target" -o $exe 2>&1 | Out-Null
		if ($LASTEXITCODE -ne 0) { throw "$($source.Name) does not compile" }
		$expected = ((Select-String -Path $source.FullName -Pattern '^//> (.*)$').Matches | ForEach-Object { $_.Groups[1].Value }) -join "`n"
		$runsOf = if ($Target -eq 'x64-linux') { @(Invoke-LinuxRuns $exe) } else { @(Invoke-WindowsRuns $exe) }
		$wrong = $runsOf.Count -ne $Runs
		$workTimes = @()
		$processTimes = @()
		foreach ($r in $runsOf) {
			$split = Split-Run $r.Lines
			if ($r.Code -ne 0 -or $null -eq $split.Work -or $split.Result -ne $expected) { $wrong = $true; continue }
			$workTimes += $split.Work
			$processTimes += $r.Process
		}
		$rows += [pscustomobject]@{
			Program = $source.BaseName
			'Text bytes' = Get-TextSize $exe
			"Work ms, best of $Runs" = if ($wrong) { 'WRONG OUTPUT' } else { Format-Best $workTimes }
			"Process ms, best of $Runs" = if ($wrong) { '' } else { Format-Best $processTimes }
		}
	}
} finally {
	Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
$rows | Format-Table -AutoSize | Out-String -Width 200 | Write-Output
