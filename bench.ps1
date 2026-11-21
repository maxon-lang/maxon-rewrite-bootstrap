# Builds each program of tests\bench with a compiler and reports, as a table, the size of the executable's code (.text, in
# bytes) and its best run time of three, in milliseconds, so a change to the code generator or the optimizer shows in the
# two numbers it is for. A program's output must be the line of its `//>` directive: a result that differs is reported as
# wrong and the program has no time.
#
#   .\bench.ps1
#   .\bench.ps1 -Compiler minimaxon.exe
param(
	[string]$Compiler = 'bin\minimaxon.exe'
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\textsize.ps1"
$runs = 3
$compilerExe = [System.IO.Path]::GetFullPath($Compiler, $PSScriptRoot)
$work = Join-Path ([System.IO.Path]::GetTempPath()) "minimaxon-bench-$PID"
New-Item -ItemType Directory -Force $work | Out-Null
$rows = @()
try {
	foreach ($source in Get-ChildItem (Join-Path $PSScriptRoot 'tests\bench') -Filter *.maxon | Sort-Object Name) {
		$exe = Join-Path $work ($source.BaseName + '.exe')
		& $compilerExe build $source.FullName -o $exe 2>&1 | Out-Null
		if ($LASTEXITCODE -ne 0) { throw "$($source.Name) does not compile" }
		$expected = (Select-String -Path $source.FullName -Pattern '^//> (.*)$').Matches | ForEach-Object { $_.Groups[1].Value }
		$best = $null
		$wrong = $false
		for ($run = 0; $run -lt $runs; $run++) {
			$output = $null
			$elapsed = (Measure-Command { $output = & $exe }).TotalMilliseconds
			if ((@($output) -join "`n") -ne (@($expected) -join "`n")) { $wrong = $true }
			if ($null -eq $best -or $elapsed -lt $best) { $best = $elapsed }
		}
		$rows += [pscustomobject]@{
			Program = $source.BaseName
			'Text bytes' = Get-TextSize $exe
			'Best of 3 ms' = if ($wrong) { 'WRONG OUTPUT' } else { '{0:0}' -f $best }
		}
	}
} finally {
	Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
$rows | Format-Table -AutoSize | Out-String -Width 200 | Write-Output
