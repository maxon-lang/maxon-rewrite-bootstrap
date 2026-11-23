# The machine-wide lock of the heavy scripts (check.ps1, specs.ps1, seed.ps1, build.ps1, selftime.ps1): one gate or build runs
# on this machine at a time, in whatever checkout, because each takes every core and a second one makes the timeouts of the
# tests fail and the machine unusable. Dot-source it, call Enter-HeavyLock first and Exit-HeavyLock in a `finally`.
#
# The lock is the named mutex Global\minimaxon-heavy. It is re-entrant: a script that runs another (check.ps1 runs
# suites.ps1, specs.ps1 runs spec-run.ps1) in this process counts its depth, and one that starts another PowerShell finds
# MINIMAXON_HEAVY_OWNER, the id of the process that holds the lock, which its children inherit. A process that ends without
# releasing leaves the mutex abandoned, which the next one takes over.

$heavyMutexName = 'Global\minimaxon-heavy'
$heavyOwnerVariable = 'MINIMAXON_HEAVY_OWNER'

# Test-HeavyOwnerAlive is whether the process that the environment names as the lock's holder is running.
function Test-HeavyOwnerAlive {
	$owner = 0
	if (-not [int]::TryParse([Environment]::GetEnvironmentVariable($heavyOwnerVariable), [ref]$owner) -or $owner -eq 0) { return $false }
	return $null -ne (Get-Process -Id $owner -ErrorAction SilentlyContinue)
}

function Enter-HeavyLock {
	if ($null -ne $global:MinimaxonHeavy) {
		$global:MinimaxonHeavy.Depth++
		return
	}
	$held = [pscustomobject]@{ Depth = 1; Mutex = $null; Owns = $false }
	if (-not (Test-HeavyOwnerAlive)) {
		$mutex = [System.Threading.Mutex]::new($false, $heavyMutexName)
		$taken = $false
		try { $taken = $mutex.WaitOne(0) } catch [System.Threading.AbandonedMutexException] { $taken = $true }
		if (-not $taken) {
			Write-Host 'waiting for another gate/build on this machine'
			try { $null = $mutex.WaitOne() } catch [System.Threading.AbandonedMutexException] { }
		}
		$held.Mutex = $mutex
		$held.Owns = $true
		[Environment]::SetEnvironmentVariable($heavyOwnerVariable, [string]$PID)
	}
	$global:MinimaxonHeavy = $held
}

function Exit-HeavyLock {
	$held = $global:MinimaxonHeavy
	if ($null -eq $held) { return }
	$held.Depth--
	if ($held.Depth -gt 0) { return }
	$global:MinimaxonHeavy = $null
	if ($held.Owns) {
		[Environment]::SetEnvironmentVariable($heavyOwnerVariable, $null)
		$held.Mutex.ReleaseMutex()
		$held.Mutex.Dispose()
	}
}
