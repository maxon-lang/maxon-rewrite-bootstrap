# Regenerates stdlib/ from maxon's standard library: cmd/convert converts each file that stdlib\files.txt names, a path
# relative to maxon's stdlib directory, to the same path under stdlib/. A file is listed once it converts with no warning
# and checks clean; the files are never edited here. The conversion lays out what it writes with bin\minimaxon.exe, the
# compiler the gate builds from the seed: run .\check.ps1 first to build it. The argument is maxon's stdlib directory.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

$maxonStdlib = if ($args.Count -gt 0) { $args[0] } else { 'C:\Users\Eric\Dev\maxon\stdlib' }
if (-not (Test-Path bin\minimaxon.exe)) {
	Write-Output 'bin\minimaxon.exe is missing: run .\check.ps1 to build it'
	exit 1
}
$env:MINIMAXON = Join-Path $PSScriptRoot 'bin\minimaxon.exe'
$listed = Get-Content stdlib\files.txt | Where-Object { $_ -ne '' }
Get-ChildItem stdlib -Recurse -File -Filter *.maxon | Remove-Item
$scratch = Join-Path ([IO.Path]::GetTempPath()) "minimaxon-stdlib-$PID"
try {
	foreach ($file in $listed) {
		$source = Join-Path $maxonStdlib $file
		$target = Join-Path stdlib $file
		$null = New-Item -ItemType Directory -Force (Split-Path $target)
		go run ./cmd/convert --out=$scratch $source
		if ($LASTEXITCODE -ne 0) { exit 1 }
		Move-Item -Force (Join-Path $scratch (Split-Path $file -Leaf)) $target
	}
} finally {
	Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
}
