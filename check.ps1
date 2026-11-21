# Runs every check AGENTS.md requires, fixing what can be fixed mechanically first (formatting, go fix's modernizers
# and the generated files), so a slip there never costs a second run. Stops at the first failing step.
#
# The compiler is self/, and it is built from the seed: the seed builds C1, C1 builds C2, and C2 is the compiler that
# every later step runs. Go builds and tests only the generators and the tools.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

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

# Until the parallel driver of Phase 3 makes the full run fast, the gate runs only the spec tests that specs/passed.txt does
# not list, and reports those of them that pass now (the line "NEW file.md: test") so that the list can be extended.
function Test-Specs {
	$output = $null | bin\c2.exe test specs --skip-passed 2>&1 | Out-String
	if ($output -notmatch '(?m)^(\d+) passed, \d+ failed, \d+ skipped') {
		Write-Output $output
		Write-Output 'the spec runner printed no count'
		$global:LASTEXITCODE = 1
		return
	}
	$new = ([regex]::Matches($output, '(?m)^NEW ')).Count
	Write-Output "specs: $($Matches[0]); $new new, to add to specs/passed.txt with: bin\c2.exe test specs --skip-passed --record-passed"
	$global:LASTEXITCODE = 0
}

function Test-Reproduces {
	bin\c2.exe build self -o bin\c3.exe
	if ($LASTEXITCODE -ne 0) { return }
	if ((Get-FileHash bin\c2.exe).Hash -ne (Get-FileHash bin\c3.exe).Hash) {
		Write-Output 'C2 built self/ into a C3 that differs from it: the compiler does not reproduce itself'
		$global:LASTEXITCODE = 1
	}
}

$steps = @(
	# Only this checkout's Go: gofmt walks dot directories too, and .claude/worktrees holds other checkouts that
	# agents are editing.
	@('format', { gofmt -w cmd internal }),
	@('go fix', { go fix ./... }),
	@('vet', { go vet ./... }),
	@('generators', { Test-Generated }),
	@('C1 from the seed', { $null = New-Item -ItemType Directory -Force bin; seed\minimaxon.exe build self -o bin\c1.exe }),
	@('C2 from C1', { bin\c1.exe build self -o bin\c2.exe }),
	# cmd/convert lays out what it converts with the compiler's fmt.
	@('Go tests', { $env:MINIMAXON = (Resolve-Path bin\c2.exe).Path; go test ./... }),
	@('selftest', { bin\c2.exe selftest }),
	# Nothing compiles the runtime's source until a program is built, so this is what holds it to the language and to
	# the signatures the compiler calls.
	@('runtime', { bin\c2.exe check runtime }),
	@('layout', { bin\c2.exe fmt --check self runtime tests }),
	# Test programs inherit stdin, so one that reads it would wait until the runner's deadline ends it: give it none.
	@('corpus', { $null | bin\c2.exe test @args }),
	@('specs', { Test-Specs }),
	# Every program the generator writes that the checker accepts must run clean under the interpreter: a violation is a
	# hole in the checker.
	@('fuzz', { bin\c2.exe fuzz }),
	@('C3 from C2', { Test-Reproduces })
)
foreach ($step in $steps) {
	$name, $run = $step
	$output = & $run @args 2>&1
	if ($LASTEXITCODE -ne 0) {
		$output | Write-Output
		Write-Output "check failed at: $name"
		exit 1
	}
	if ($name -eq 'specs' -and $output) { $output | Write-Output }
}
Write-Output 'all checks passed'
