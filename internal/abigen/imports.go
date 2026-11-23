package main

import (
	"strconv"
	"strings"
)

// The libraries an executable imports from. A PE imports the DLL functions its live code calls, which the link collects
// from the calls' names (self/backend's LinkImports), so no list of them is kept here: the runtime calls one by a string
// literal (`__Raw.callImportN("Name")`) and the entry stub by its name. macOS's functions are listed, since a call of a
// variadic one depends on how many of its words are fixed.

const importsFile = "../../self/backend/imports_gen.maxon"

// importLibrary is the one library a PE imports from.
const importLibrary = "kernel32.dll"

// libSystemFunction is a function of macOS's libSystem that the runtime or the entry stub may call. A variadic one
// takes its first fixed words in registers and every word after them on the stack, as Apple's convention passes a
// variadic argument.
type libSystemFunction struct {
	name     string
	variadic bool
	fixed    int
}

// The libSystem functions a macOS executable may call, by their C names. Unlike the PE import table, an executable binds
// only those its live code calls (the link narrows them and numbers their slots of __got), so the order here is no
// slot's; a name missing here is found by the test of this package rather than by a panic at link time.
var macosImports = []libSystemFunction{
	{name: "_exit"},
	{name: "write"},
	{name: "read"},
	{name: "open", variadic: true, fixed: 2},
	{name: "close"},
	{name: "fstat"},
	{name: "stat"},
	{name: "mmap"},
	{name: "munmap"},
	{name: "mprotect"},
	{name: "getpid"},
	{name: "getpriority"},
	{name: "setpriority"},
	{name: "getcwd"},
	{name: "clock_gettime_nsec_np"},
	{name: "nanosleep"},
	{name: "__error"},
	{name: "rename"},
	{name: "unlink"},
	{name: "mkdir"},
	{name: "rmdir"},
	{name: "opendir"},
	{name: "readdir"},
	{name: "closedir"},
	{name: "_NSGetExecutablePath"},
	{name: "realpath"},
	{name: "free"},
	{name: "sysconf"},
	{name: "getentropy"},
	{name: "sched_yield"},
	{name: "pthread_attr_init"},
	{name: "pthread_attr_setstacksize"},
	{name: "pthread_attr_destroy"},
	{name: "pthread_create"},
	{name: "pthread_key_create"},
	{name: "pthread_setspecific"},
	{name: "pthread_self"},
	{name: "pthread_kill"},
	{name: "pthread_sigmask"},
	{name: "pthread_mutex_init"},
	{name: "pthread_mutex_lock"},
	{name: "pthread_mutex_unlock"},
	{name: "pthread_cond_init"},
	{name: "pthread_cond_wait"},
	{name: "pthread_cond_signal"},
	{name: "sigaction"},
	{name: "sigaltstack"},
	{name: "kqueue"},
	{name: "kevent"},
	{name: "pipe"},
	{name: "poll"},
	{name: "waitpid"},
	{name: "kill"},
	{name: "posix_spawn"},
	{name: "posix_spawn_file_actions_init"},
	{name: "posix_spawn_file_actions_adddup2"},
	{name: "posix_spawn_file_actions_addinherit_np"},
	{name: "posix_spawn_file_actions_destroy"},
	{name: "posix_spawnattr_init"},
	{name: "posix_spawnattr_setflags"},
	{name: "posix_spawnattr_destroy"},
}

// importsMaxon is self/backend/imports_gen.maxon: the library a PE imports from, and the libSystem functions a macOS
// executable may call, with the variadic ones and the words of each that are fixed.
func importsMaxon(macos []libSystemFunction) string {
	var b strings.Builder
	b.WriteString("// " + header + "\n\n")
	b.WriteString("// rtDll is the one library a PE imports from.\n")
	b.WriteString("export let rtDll = \"" + importLibrary + "\"\n")
	b.WriteString("\n// The libSystem functions a macOS executable may call, by their C names.\n")
	b.WriteString("export function macosImports() returns NameList\n\tvar names = NameList.create(0)\n")
	for _, f := range macos {
		b.WriteString("\tnames.push(\"" + f.name + "\")\n")
	}
	b.WriteString("\treturn names\nend 'macosImports'\n")
	b.WriteString("\n// macosVariadicImports are the variadic functions of macosImports, and macosVariadicFixedWords the words of each\n// that it takes in registers: the rest go on the stack.\n")
	b.WriteString("export function macosVariadicImports() returns NameList\n\tvar names = NameList.create(0)\n")
	for _, f := range macos {
		if f.variadic {
			b.WriteString("\tnames.push(\"" + f.name + "\")\n")
		}
	}
	b.WriteString("\treturn names\nend 'macosVariadicImports'\n\n")
	b.WriteString("export function macosVariadicFixedWords() returns CountList\n\tvar words = CountList.create(0)\n")
	for _, f := range macos {
		if f.variadic {
			b.WriteString("\twords.push(" + strconv.Itoa(f.fixed) + ")\n")
		}
	}
	b.WriteString("\treturn words\nend 'macosVariadicFixedWords'\n")
	return b.String()
}
