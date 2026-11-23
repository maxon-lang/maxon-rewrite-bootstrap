# Runs `test specs` with the compiler at -Compiler and the arguments after it from .spec-run-<pid>, a directory directly
# under the checkout, as maxon's spec runner runs every test from `<checkout>/.spec-run-<pid>/`: the tests' relative paths
# are written against that depth (`../stdlib` is the checkout's library), and a test inherits the runner's working directory.
# specs.ps1 and suites.ps1 run the spec tests through it. Its exit code is the runner's.
#
# A full run (--require-passed or --record-passed, and nothing else) keeps the result of every test in bin\spec-results.txt
# beside bin\spec-results.stamp, the stamp of everything those results depend on, which is written only when the run ended and
# nothing it depends on changed while it ran. A later full run whose stamp is the same takes the results from the file instead
# of compiling and running every test again (`test specs --replay-results=`), and still counts them against the current
# specs/passed.txt, which is why that list is not in the stamp. Whatever is in doubt (no stamp, a different input, a run
# that did not end, a file that is not of these tests) runs every test.
param(
	[Parameter(Mandatory)][string]$Compiler,
	[Parameter(ValueFromRemainingArguments)]$TestArgs
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\hashseed.ps1"
. "$PSScriptRoot\stamps.ps1"
$compiler = (Resolve-Path $Compiler).Path
$specs = Join-Path $PSScriptRoot 'specs'
$runDir = Join-Path $PSScriptRoot ".spec-run-$PID"

$resultsFile = Join-Path $PSScriptRoot 'bin\spec-results.txt'
$stampFile = Join-Path $PSScriptRoot 'bin\spec-results.stamp'

# What `test specs` reads: the compiler, which holds the runner and finds the runtime and the library beside itself, the spec
# files (the conversion's skips are lines of them), the skip table that the conversion reads, and the sources that every
# test is compiled with. The list of the tests that passed is read again by every run, which is why it is left out.
$stampPaths = $specs, (Join-Path $PSScriptRoot 'runtime'), (Join-Path $PSScriptRoot 'stdlib'), (Join-Path $PSScriptRoot 'self\driver\skips.tsv')
$stampExclude = @(Join-Path $specs 'passed.txt')

# The arguments that only decide what a run says of the results it has; every other argument can change a result, so a run with
# one never keeps or replays results.
$classifyingFlags = '--require-passed', '--record-passed'
# The code with which the runner refuses results that are not those of its tests (replayRefusedExitCode in self/driver).
$replayRefusedCode = 3
# The environment that a test inherits and a result can follow.
$stampEnvironment = 'MAXON_MAX_PROCS', 'MAXON_HASH_SEED', 'MINIMAXON_CACHE'

# The text a result is kept under: equal texts are equal inputs.
function Get-Stamp([string[]]$Flags) {
	$environment = ($stampEnvironment | ForEach-Object { "$_=$([Environment]::GetEnvironmentVariable($_))" }) -join ' '
	return @(
		"compiler $((Get-FileHash $compiler -Algorithm SHA256).Hash)",
		"inputs $(Get-PathsDigest $stampPaths -Exclude $stampExclude)",
		"flags $($Flags -join ' ')",
		"environment $environment"
	) -join "`n"
}

# Invoke-Specs lets the runner's output through, so its code is left in $specsCode. The runner writes to stderr when it refuses
# its flags, which Windows PowerShell 5 raises as an error under 'Stop' where pwsh does not.
function Invoke-Specs([string[]]$RunArgs) {
	$null = New-Item -ItemType Directory -Force $runDir
	Push-Location $runDir
	$ErrorActionPreference = 'Continue'
	try {
		$null | & $compiler test $specs @RunArgs
		$script:specsCode = $LASTEXITCODE
	} finally {
		Pop-Location
		Remove-Item -Recurse -Force $runDir -ErrorAction SilentlyContinue
	}
}

# The results and the stamp are of a run that ended: a run that is interrupted leaves neither, since the stamp is written last.
function Remove-KeptResults {
	Remove-Item $stampFile, $resultsFile -ErrorAction SilentlyContinue
}

$flags = @($TestArgs | Where-Object { $_ -notin $classifyingFlags })
$fullRun = $flags.Count -eq 0 -and $TestArgs.Count -gt 0
if (-not $fullRun) {
	Invoke-Specs @($TestArgs)
	exit $specsCode
}

$stamp = Get-Stamp $flags
if ((Test-Path $resultsFile) -and (Test-Stamp $stampFile $stamp)) {
	Invoke-Specs (@("--replay-results=$resultsFile") + $TestArgs)
	if ($specsCode -ne $replayRefusedCode) {
		Write-Output "replayed: the results of the run that stamp $(Get-StampId $stamp) was written for, kept in $resultsFile"
		exit $specsCode
	}
	Write-Output "the kept results are not of these tests, so every test runs"
}

Remove-KeptResults
$null = New-Item -ItemType Directory -Force (Split-Path $resultsFile)
Invoke-Specs (@("--save-results=$resultsFile") + $TestArgs)
# A run ends with the runner's code 0 (all as required) or 1 (a test did not pass); another code is a crash or a refusal.
if (($specsCode -eq 0 -or $specsCode -eq 1) -and (Test-Path $resultsFile) -and ((Get-Stamp $flags) -ceq $stamp)) {
	[IO.File]::WriteAllText($stampFile, $stamp)
} else {
	Remove-KeptResults
}
exit $specsCode
