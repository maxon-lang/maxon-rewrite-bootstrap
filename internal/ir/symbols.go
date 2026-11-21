package ir

import "minimaxon/internal/diag"

const RtDll = "kernel32.dll"

var RtImports = []string{
	"GetProcessHeap",
	"HeapAlloc",
	"GetStdHandle",
	"WriteFile",
	"ExitProcess",
	"HeapFree",
	"GetCommandLineA",
	"CreateFileA",
	"ReadFile",
	"GetFileSizeEx",
	"CloseHandle",
	"MoveFileExA",
	"DeleteFileA",
	"FindFirstFileA",
	"FindNextFileA",
	"FindClose",
	"GetFileAttributesA",
	"CreateDirectoryA",
	"GetEnvironmentVariableA",
	"CreateProcessA",
	"WaitForSingleObject",
	"GetExitCodeProcess",
	"GetLastError",
	"CreatePipe",
	"SetHandleInformation",
	"PeekNamedPipe",
}

// IsNativeEntry is whether the backend implements a primitive's runtime entry.
func IsNativeEntry(symbol Rt) bool {
	switch symbol.Unit() {
	case RtUnitArrays, RtUnitBytes, RtUnitOs:
		return true
	}
	return false
}

// IsBytesEntry is whether symbol lives in the bytes runtime unit.
func IsBytesEntry(symbol Rt) bool { return symbol.Unit() == RtUnitBytes }

// IsOsEntry is whether symbol lives in the OS runtime unit.
func IsOsEntry(symbol Rt) bool { return symbol.Unit() == RtUnitOs }

func (r Rt) String() string {
	r.require()
	return rtNames[r]
}

// Unit is where the symbol is defined.
func (r Rt) Unit() RtUnit {
	r.require()
	return rtUnits[r]
}

func (r Rt) require() {
	if int(r) >= len(rtNames) || rtNames[r] == "" {
		panic(diag.Internal("%d is not a runtime symbol", int(r)))
	}
}
