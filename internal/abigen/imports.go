package main

import (
	"strconv"
	"strings"
)

// The import table of every executable: the library the runtime and the entry stub call into, and its functions in the
// order of their slots, which the PE writer lays out and the code calls through by index. The runtime names a function
// by a string literal (`__Raw.callImportN("Name")`), so a name missing here is found by the test of this package rather
// than by a panic at link time. Appending keeps every earlier slot; inserting moves them all.

const importsFile = "../../self/backend/imports_gen.maxon"

// importLibrary is the one library the executable imports from.
const importLibrary = "kernel32.dll"

var importNames = []string{
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
	"GetCommandLineW",
	"CreateFileW",
	"MoveFileExW",
	"DeleteFileW",
	"FindFirstFileW",
	"FindNextFileW",
	"GetFileAttributesW",
	"CreateDirectoryW",
	"GetEnvironmentVariableW",
	"CreateProcessW",
	"GetModuleFileNameW",
	"GetCurrentDirectoryW",
	"GetModuleFileNameA",
	"GetCurrentDirectoryA",
	"GetTickCount64",
	"TerminateProcess",
	"GetCurrentProcessId",
	"SetThreadStackGuarantee",
	"AddVectoredExceptionHandler",
	"CreateThread",
	"CreateEventW",
	"SetEvent",
	"TlsAlloc",
	"TlsSetValue",
	"GetActiveProcessorCount",
	"SwitchToThread",
	"QueryPerformanceCounter",
	"QueryPerformanceFrequency",
}

// importsMaxon is self/backend/imports_gen.maxon: the library, the slot of each import as a constant (imp followed by
// its name) and the list of names in slot order.
func importsMaxon(names []string) string {
	var b strings.Builder
	b.WriteString("// " + header + "\n\n")
	b.WriteString("// rtDll is the one library the executable imports from.\n")
	b.WriteString("export let rtDll = \"" + importLibrary + "\"\n")
	b.WriteString("\n// The imports are in the order of their slots in the import table, which code calls through by index.\n")
	for i, n := range names {
		b.WriteString("export let imp" + n + " = " + strconv.Itoa(i) + " as Count\n\n")
	}
	b.WriteString("export function rtImports() returns NameList\n\tvar names = NameList.create(0)\n")
	for _, n := range names {
		b.WriteString("\tnames.push(\"" + n + "\")\n")
	}
	b.WriteString("\treturn names\nend 'rtImports'\n")
	return b.String()
}
