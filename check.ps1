# Runs every check AGENTS.md requires, fixing what can be fixed mechanically first (formatting, go fix's modernizers
# and the generated files), so a slip there never costs a second run. Stops at the first failing step.
#
# The compiler is self/, and the seed builds it into bin\minimaxon.exe, which suites.ps1 then tests. Whether a compiler
# reproduces itself is checked only where a seed is renewed (seed.ps1). Go builds and tests only the generators and the
# tools.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

. "$PSScriptRoot\lock.ps1"
# The lock is held to the end of the script (lock.ps1); the body below is not indented, so that the script reads as it did.
Enter-HeavyLock
try {
. "$PSScriptRoot\steptimes.ps1"
$null = Start-StepTimes
$gateArgs = $args

# The generated files are named *_gen.*; they are hashed before and after the generators run, so a stale one fails the
# check even where there is no git to show it.
function Get-GeneratedHashes {
	Get-ChildItem self, runtime, cmd -Recurse -File -Filter '*_gen.*' | ForEach-Object {
		"$($_.FullName) $((Get-FileHash $_.FullName).Hash)"
	}
}

function Test-Generated {
	$before = Get-GeneratedHashes
	go generate ./internal/...
	if ($LASTEXITCODE -ne 0) { return }
	$changed = Compare-Object @($before) @(Get-GeneratedHashes)
	if ($changed) {
		Write-Output 'generated files were stale and are regenerated now; review and commit them:'
		$changed | ForEach-Object { Write-Output "  $(($_.InputObject -split ' ')[0])" }
		$global:LASTEXITCODE = 1
	}
}

$steps = @(
	# Only this checkout's Go: gofmt walks dot directories too, and .claude/worktrees holds other checkouts that
	# agents are editing.
	@('format', { gofmt -w cmd internal tests/bench/go }),
	@('go fix', { go fix ./... }),
	@('vet', { go vet ./... }),
	@('generators', { Test-Generated }),
	@('compiler from the seed', { $null = New-Item -ItemType Directory -Force bin; seed\minimaxon.exe build self -o bin\minimaxon.exe }),
	@('suites', { & "$PSScriptRoot\suites.ps1" bin\minimaxon.exe @args })
)
foreach ($step in $steps) {
	$name, $run = $step
	# suites.ps1 times each of its steps itself.
	$output = Invoke-Step $name -Group:($name -eq 'suites') { & $run @gateArgs 2>&1 }
	if ($LASTEXITCODE -ne 0) {
		$output | Write-Output
		Write-Output "check failed at: $name"
		Stop-StepTimes
		exit 1
	}
	if ($name -eq 'suites' -and $output) { $output | Write-Output }
}
Stop-StepTimes
Write-Output 'all checks passed'
} finally {
	Exit-HeavyLock
}
