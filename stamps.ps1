# Stamps: the way the scripts tell that a result they kept (the results of the spec tests, the specs the conversion wrote) is
# of the inputs they have now. A stamp is the digest of every byte an output follows, and a kept result is used only when its
# stamp is equal to the current one; anything else, or no stamp, redoes the work. Dot-source it.

# Get-PathsDigest is the SHA-256 of the files at Paths (a directory stands for every file below it) in the order of their full
# names, each with its name and length so that a moved or split file changes it. Files named in Exclude are left out.
function Get-PathsDigest([string[]]$Paths, [string[]]$Exclude = @()) {
	$files = [System.Collections.Generic.List[string]]::new()
	foreach ($path in $Paths) {
		if (Test-Path -LiteralPath $path -PathType Container) {
			foreach ($file in Get-ChildItem -LiteralPath $path -Recurse -File -Force) {
				if ($Exclude -notcontains $file.FullName) { $files.Add($file.FullName) }
			}
		} elseif (Test-Path -LiteralPath $path -PathType Leaf) {
			$files.Add((Resolve-Path -LiteralPath $path).Path)
		} else {
			throw "stamp: $path does not exist"
		}
	}
	$sorted = $files.ToArray()
	[Array]::Sort($sorted, [StringComparer]::Ordinal)
	$hash = [System.Security.Cryptography.IncrementalHash]::CreateHash([System.Security.Cryptography.HashAlgorithmName]::SHA256)
	try {
		foreach ($file in $sorted) {
			$bytes = [IO.File]::ReadAllBytes($file)
			$hash.AppendData([Text.Encoding]::UTF8.GetBytes("$file`0$($bytes.Length)`0"))
			$hash.AppendData($bytes)
		}
		return ([BitConverter]::ToString($hash.GetHashAndReset()) -replace '-', '')
	} finally {
		$hash.Dispose()
	}
}

# Get-StampId is a short name of a stamp, to say which one a result was kept under.
function Get-StampId([string]$Stamp) {
	$sha = [System.Security.Cryptography.SHA256]::Create()
	try {
		return ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($Stamp))) -replace '-', '').Substring(0, 12).ToLower()
	} finally {
		$sha.Dispose()
	}
}

# Test-Stamp is whether the stamp kept in the file is the one given.
function Test-Stamp([string]$StampFile, [string]$Stamp) {
	return (Test-Path -LiteralPath $StampFile) -and ([IO.File]::ReadAllText($StampFile) -ceq $Stamp)
}
