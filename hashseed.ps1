# Pins the hash seed of every program a test runner starts, as maxon's harness hands the programs of a run its
# MAXON_HASH_SEED: a program whose output follows a hash, such as a map's order, then prints the same natively, under
# the interpreter and on every run. A seed already in the environment is kept, so a run under another one is a matter of
# setting it. WSL passes on only the variables that WSLENV names, so a Linux run names this one. Dot-source it.
$hashSeedPin = '1111111111111111'
if (-not $env:MAXON_HASH_SEED) {
	$env:MAXON_HASH_SEED = $hashSeedPin
}
if (-not ($env:WSLENV -split ':' -contains 'MAXON_HASH_SEED')) {
	$env:WSLENV = if ($env:WSLENV) { "$($env:WSLENV):MAXON_HASH_SEED" } else { 'MAXON_HASH_SEED' }
}
