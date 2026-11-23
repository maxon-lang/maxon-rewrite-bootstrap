# The elapsed time of every step of the gate (check.ps1 and suites.ps1), kept in one place so that every step is timed the
# same way: Invoke-Step prints a step's time when it ends, and Write-StepTable ends the run with the steps by time and the
# total. The times are kept in a global, since suites.ps1 runs inside check.ps1's process and adds its steps to the same
# table; a script that runs alone (suites.ps1 from seed.ps1) starts the table and prints it itself. Times are written with
# Write-Host, which a caller capturing a step's output does not swallow, so they show as the steps end. Dot-source it.

# Start-StepTimes begins the table when no script above has, and says whether this one did, which makes it the one that
# prints the table.
function Start-StepTimes {
	if ($null -ne $global:StepTimes) { return $false }
	$global:StepTimes = [System.Collections.Generic.List[object]]::new()
	$global:StepTimesClock = [System.Diagnostics.Stopwatch]::StartNew()
	return $true
}

function Format-StepSeconds([double]$Seconds) {
	return [string]::Format([cultureinfo]::InvariantCulture, '{0,7:N1} s', $Seconds)
}

# Invoke-Step runs a step's script block, passes on what it writes, and records and prints how long it took, also when the
# block throws. The caller reads $LASTEXITCODE as it would after running the block itself. A group is a step made of steps
# that record themselves (check.ps1's suites): its time is printed and not added to the table, which would count it twice.
# The parameters have names the blocks never use, since a block sees the variables of the function that runs it.
function Invoke-Step([string]$StepName, [scriptblock]$StepBlock, [switch]$Group) {
	$stepClock = [System.Diagnostics.Stopwatch]::StartNew()
	try {
		& $StepBlock
	} finally {
		$stepClock.Stop()
		if (-not $Group) {
			$global:StepTimes.Add([pscustomobject]@{ Name = $StepName; Seconds = $stepClock.Elapsed.TotalSeconds })
		}
		Write-Host "[$(Format-StepSeconds $stepClock.Elapsed.TotalSeconds)] $StepName"
	}
}

# Write-StepTable prints the steps run so far, the longest first, and the time since the table began.
function Write-StepTable {
	Write-Host ''
	Write-Host 'steps by time:'
	$global:StepTimes | Sort-Object -Property Seconds -Descending | ForEach-Object {
		Write-Host "  $(Format-StepSeconds $_.Seconds)  $($_.Name)"
	}
	Write-Host "  $(Format-StepSeconds $global:StepTimesClock.Elapsed.TotalSeconds)  total"
}

# Stop-StepTimes prints the table and ends it, so that a later run in the same session starts a new one.
function Stop-StepTimes {
	Write-StepTable
	Remove-Variable -Scope Global StepTimes, StepTimesClock
}
