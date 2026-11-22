# Get-TextSize is the size in bytes of the code of an executable. Of a Windows executable it is the virtual size of its
# .text section, read from the section headers, so it is the code the compiler wrote and not the padding the file aligns
# it to. A Linux executable of the compiler has no section headers: its code is its first segment after the file's
# headers, read from the program headers, with the few bytes that align the code's start. Dot-sourced by bench.ps1 and
# selftime.ps1.
$peSignatureOffsetAt = 0x3C
$coffHeaderSize = 20
$sectionHeaderSize = 40
$sectionNameLength = 8
$virtualSizeAt = 8
$elfMagic = 0x464C457F
$elfProgramHeadersAt = 0x20
$elfProgramHeaderSizeAt = 0x36
$elfProgramHeaderCountAt = 0x38
$elfSegmentFileSizeAt = 0x20

function Get-TextSize([string]$exe) {
	$bytes = [System.IO.File]::ReadAllBytes($exe)
	if ([BitConverter]::ToUInt32($bytes, 0) -eq $elfMagic) {
		$programHeaders = [BitConverter]::ToUInt64($bytes, $elfProgramHeadersAt)
		$headersEnd = $programHeaders + [BitConverter]::ToUInt16($bytes, $elfProgramHeaderSizeAt) * [BitConverter]::ToUInt16($bytes, $elfProgramHeaderCountAt)
		return [BitConverter]::ToUInt64($bytes, $programHeaders + $elfSegmentFileSizeAt) - $headersEnd
	}
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
