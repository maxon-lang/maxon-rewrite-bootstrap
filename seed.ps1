# Renews seed\minimaxon.exe from self/: the seed builds C1, C1 builds C2 and C2 builds C3. C2 replaces the seed only
# when it compiles itself to the same bytes and passes the corpus. seed\README.md says when a renewal is needed.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

function Invoke-Stage($name, [scriptblock]$run) {
	$output = & $run 2>&1
	if ($LASTEXITCODE -ne 0) {
		$output | Write-Output
		Write-Output "seed not renewed: $name failed"
		exit 1
	}
}

$null = New-Item -ItemType Directory -Force bin
Invoke-Stage 'C1' { seed\minimaxon.exe build self -o bin\c1.exe }
Invoke-Stage 'C2' { bin\c1.exe build self -o bin\c2.exe }
Invoke-Stage 'C3' { bin\c2.exe build self -o bin\c3.exe }
if ((Get-FileHash bin\c2.exe).Hash -ne (Get-FileHash bin\c3.exe).Hash) {
	Write-Output 'seed not renewed: C2 and C3 differ, so the compiler does not reproduce itself'
	exit 1
}
# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
Invoke-Stage 'the corpus' { $null | bin\c2.exe test }
Copy-Item bin\c2.exe seed\minimaxon.exe
Write-Output 'seed renewed from bin\c2.exe'
