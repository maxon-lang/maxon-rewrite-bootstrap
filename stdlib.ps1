# Regenerates stdlib/ from maxon's standard library: cmd/convert converts each file that a line of stdlib\files.txt names, a
# path relative to maxon's stdlib directory, into the library module the line names after it (a directory under stdlib/, '.'
# for the root), and the compiler's fix command then applies the fixes of what minimaxon's language differences raise in it,
# as specs.ps1 does for the spec tests: a 'let' that is changed becomes a 'var', and a name of another library module is
# qualified. A file is listed once it converts with no warning and checks clean; the files are never edited here. The
# conversion lays out what it writes with bin\minimaxon.exe, the compiler the gate builds from the seed: run .\check.ps1 first
# to build it. The argument is maxon's stdlib directory.
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
	foreach ($line in $listed) {
		$file, $module = -split $line
		if (-not $module) {
			Write-Output "stdlib\files.txt: '$line' names no module: write the module's directory after the file, '.' for the root"
			exit 1
		}
		$source = Join-Path $maxonStdlib $file
		$targetDir = Join-Path stdlib $module
		$null = New-Item -ItemType Directory -Force $targetDir
		go run ./cmd/convert --out=$scratch $source
		if ($LASTEXITCODE -ne 0) { exit 1 }
		Move-Item -Force (Join-Path $scratch (Split-Path $file -Leaf)) (Join-Path $targetDir (Split-Path $file -Leaf))
	}
} finally {
	Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
}
$null | bin\minimaxon.exe fix stdlib
if ($LASTEXITCODE -ne 0) { exit 1 }
