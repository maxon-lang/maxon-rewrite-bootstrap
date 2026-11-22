# Runs `test specs` with the compiler at -Compiler and the arguments after it from .spec-run-<pid>, a directory directly
# under the checkout, as maxon's spec runner runs every test from `<checkout>/.spec-run-<pid>/`: the tests' relative paths
# are written against that depth (`../stdlib` is the checkout's library), and a test inherits the runner's working directory.
# specs.ps1 and suites.ps1 run the spec tests through it. Its exit code is the runner's.
param(
	[Parameter(Mandatory)][string]$Compiler,
	[Parameter(ValueFromRemainingArguments)]$TestArgs
)
$ErrorActionPreference = 'Stop'
$compiler = (Resolve-Path $Compiler).Path
$specs = Join-Path $PSScriptRoot 'specs'
$runDir = Join-Path $PSScriptRoot ".spec-run-$PID"
$null = New-Item -ItemType Directory -Force $runDir
Push-Location $runDir
try {
	$null | & $compiler test $specs @TestArgs
	$code = $LASTEXITCODE
} finally {
	Pop-Location
	Remove-Item -Recurse -Force $runDir -ErrorAction SilentlyContinue
}
exit $code
