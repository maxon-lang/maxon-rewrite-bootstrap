# Get-TextSize is the size in bytes of the code of a Windows executable: the virtual size of its .text section, read from
# the section headers, so it is the code the compiler wrote and not the padding the file aligns it to. Dot-sourced by
# bench.ps1 and selftime.ps1.
$peSignatureOffsetAt = 0x3C
$coffHeaderSize = 20
$sectionHeaderSize = 40
$sectionNameLength = 8
$virtualSizeAt = 8

function Get-TextSize([string]$exe) {
	$bytes = [System.IO.File]::ReadAllBytes($exe)
	$pe = [BitConverter]::ToInt32($bytes, $peSignatureOffsetAt)
	$coff = $pe + 4
	$sectionCount = [BitConverter]::ToUInt16($bytes, $coff + 2)
	$optionalSize = [BitConverter]::ToUInt16($bytes, $coff + 16)
	$first = $coff + $coffHeaderSize + $optionalSize
	for ($i = 0; $i -lt $sectionCount; $i++) {
		$at = $first + $i * $sectionHeaderSize
		$name = [System.Text.Encoding]::ASCII.GetString($bytes, $at, $sectionNameLength).TrimEnd([char]0)
		if ($name -eq '.text') { return [BitConverter]::ToUInt32($bytes, $at + $virtualSizeAt) }
	}
	throw "$exe has no .text section"
}
