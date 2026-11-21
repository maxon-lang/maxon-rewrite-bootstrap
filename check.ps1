# Runs every check AGENTS.md requires, fixing what can be fixed mechanically first (formatting and go fix's
# modernizers), so a formatting slip never costs a second run. Stops at the first failing step.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	$env:PATH += ';C:\Program Files\Go\bin'
}
Set-Location $PSScriptRoot

$steps = @(
	# Only this checkout's Go: gofmt walks dot directories too, and .claude/worktrees holds other checkouts that
	# agents are editing.
	@('format', { gofmt -w (Get-ChildItem -Name -Filter *.go) cmd internal }),
	@('go fix', { go fix ./... }),
	@('vet', { go vet ./... }),
	@('unit tests', { go test ./... }),
	@('build', { go build -o minimaxon.exe . }),
	@('corpus', { .\minimaxon.exe test @args }),
	# The Go compiler never compiles the runtime's source, so this is what holds it to the language and to the
	# signatures the compiler calls.
	@('runtime', { .\minimaxon.exe check runtime })
)
foreach ($step in $steps) {
	$name, $run = $step
	$output = & $run @args 2>&1
	if ($LASTEXITCODE -ne 0) {
		$output | Write-Output
		Write-Output "check failed at: $name"
		exit 1
	}
}
Write-Output 'all checks passed'
