package main

// The Linux x64 kernel's numbers that the runtime's Linux source and the entry stub pass to it: the system calls by
// their x86-64 numbers (arch/x86/entry/syscalls/syscall_64.tbl), which differ from every other architecture's, and the
// flags and the layouts of the records they read and write. Each is the kernel's value, under maxon's name where its
// Linux runtime has one (Targets/Shared/LinuxRuntime.maxon, Targets/X64/X64LinuxRuntime.maxon).
var linuxABI = []layoutConst{
	// The system calls.
	{"linuxSysRead", 0, true, toRuntime, "read(fd, buffer, count)."},
	{"linuxSysWrite", 1, true, toRuntime | toSelfBack, "write(fd, buffer, count)."},
	{"linuxSysClose", 3, true, toRuntime, "close(fd)."},
	{"linuxSysFstat", 5, true, toRuntime, "fstat(fd, stat)."},
	{"linuxSysPoll", 7, true, toRuntime, "poll(fds, count, timeout in milliseconds)."},
	{"linuxSysMmap", 9, true, toRuntime | toSelfBack, "mmap(address, length, protection, flags, fd, offset)."},
	{"linuxSysMprotect", 10, true, toRuntime | toSelfBack, "mprotect(address, length, protection)."},
	{"linuxSysMunmap", 11, true, toRuntime, "munmap(address, length)."},
	{"linuxSysRtSigaction", 13, true, toSelfBack, "rt_sigaction(signal, action, old action, size of the signal mask)."},
	{"linuxSysRtSigreturn", 15, true, toSelfBack, "rt_sigreturn(), which a signal handler's restorer calls."},
	{"linuxSysSchedYield", 24, true, toRuntime, "sched_yield()."},
	{"linuxSysGetpid", 39, true, toRuntime, "getpid()."},
	{"linuxSysClone", 56, true, toRuntime | toSelfBack, "clone(flags, stack, parent tid, child tid, tls), x86-64's order of the last two."},
	{"linuxSysExecve", 59, true, toRuntime, "execve(path, argv, envp)."},
	{"linuxSysExit", 60, true, toRuntime | toSelfBack, "exit(code), which ends the calling thread alone."},
	{"linuxSysWait4", 61, true, toRuntime, "wait4(pid, status, options, usage)."},
	{"linuxSysKill", 62, true, toRuntime, "kill(pid, signal)."},
	{"linuxSysGetcwd", 79, true, toRuntime, "getcwd(buffer, size)."},
	{"linuxSysRename", 82, true, toRuntime, "rename(old, new), which replaces what is at new."},
	{"linuxSysMkdir", 83, true, toRuntime, "mkdir(path, mode)."},
	{"linuxSysUnlink", 87, true, toRuntime, "unlink(path), which refuses a directory."},
	{"linuxSysSigaltstack", 131, true, toSelfBack, "sigaltstack(stack, old stack)."},
	{"linuxSysArchPrctl", 158, true, toSelfBack, "arch_prctl(code, address)."},
	{"linuxSysFutex", 202, true, toRuntime, "futex(address, operation, value, timeout, address2, value3)."},
	{"linuxSysSchedGetaffinity", 204, true, toRuntime, "sched_getaffinity(pid, size of the mask, mask)."},
	{"linuxSysGetdents64", 217, true, toRuntime, "getdents64(fd, buffer, size)."},
	{"linuxSysClockGettime", 228, true, toRuntime, "clock_gettime(clock, timespec)."},
	{"linuxSysExitGroup", 231, true, toRuntime | toSelfBack, "exit_group(code), which ends every thread of the process."},
	{"linuxSysOpenat", 257, true, toRuntime, "openat(directory fd, path, flags, mode)."},
	{"linuxSysNewfstatat", 262, true, toRuntime, "newfstatat(directory fd, path, stat, flags)."},
	{"linuxSysStatx", 332, true, toRuntime, "statx(directory fd, path, flags, mask, statx): the times of a file with the one it was made, which stat has no field for."},
	{"linuxSysReadlinkat", 267, true, toRuntime, "readlinkat(directory fd, path, buffer, size)."},
	{"linuxSysDup3", 292, true, toRuntime, "dup3(old fd, new fd, flags)."},
	{"linuxSysPipe2", 293, true, toRuntime, "pipe2(fds, flags)."},

	// A system call that fails returns -errno, a word in the last page of the address space.
	{"linuxErrnoLimit", 4096, true, toRuntime | toSelfBack, "one past the largest errno: a result above the word -linuxErrnoLimit is a failure."},

	// mmap and mprotect.
	{"linuxPageBytes", 4096, true, toRuntime | toSelfBack, "the size of a page."},
	{"linuxProtNone", 0, true, toRuntime | toSelfBack, "PROT_NONE: a page that faults when touched, a stack's guard."},
	{"linuxProtReadWrite", 3, true, toRuntime | toSelfBack, "PROT_READ | PROT_WRITE."},
	{"linuxMapAnonPrivate", 0x22, true, toRuntime | toSelfBack, "MAP_PRIVATE | MAP_ANONYMOUS: zeroed pages of no file."},
	{"linuxMapNoReserve", 0x4000, true, toRuntime | toSelfBack, "MAP_NORESERVE: a stack's address space, committed a page at a time as it is touched."},

	// arch_prctl and clone: the thread pointer, which fs addresses, and a worker's thread.
	{"linuxArchSetFs", 0x1002, true, toSelfBack, "ARCH_SET_FS: arch_prctl sets the base that fs addresses from."},
	{"linuxCloneWorkerFlags", 0x90F00, true, toSelfBack, "CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND | CLONE_THREAD | CLONE_SETTLS: a thread of the process with the thread pointer it is given, maxon's worker flags but CLONE_CHILD_CLEARTID, since nothing waits for a worker to end."},
	{"linuxCloneChildFlags", 17, true, toRuntime, "SIGCHLD alone: clone makes a child process with a copy of the memory, as fork does, which signals the parent when it ends."},

	// futex.
	{"linuxFutexWaitPrivate", 128, true, toRuntime, "FUTEX_WAIT | FUTEX_PRIVATE_FLAG: sleep while the 32-bit word holds the value."},
	{"linuxFutexWakePrivate", 129, true, toRuntime, "FUTEX_WAKE | FUTEX_PRIVATE_FLAG: wake as many sleepers of the word as the value says."},

	// The clocks.
	{"linuxClockMonotonic", 1, true, toRuntime, "CLOCK_MONOTONIC."},
	{"linuxTimespecBytes", 16, true, toRuntime, "a timespec: the seconds, then the nanoseconds, a word each."},

	// Files.
	{"linuxAtCwdNegated", 100, true, toRuntime, "AT_FDCWD, -100, negated: the directory fd of a path relative to the working directory."},
	{"linuxOpenReadOnly", 0x80000, true, toRuntime, "O_RDONLY | O_CLOEXEC."},
	{"linuxOpenReplace", 0x80241, true, toRuntime, "O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC."},
	{"linuxOpenCloseOnExec", 0x80000, true, toRuntime, "O_CLOEXEC: an fd that an execve closes, as pipe2 takes it."},
	{"linuxOpenDirectory", 0x90000, true, toRuntime, "O_RDONLY | O_DIRECTORY | O_CLOEXEC."},
	{"linuxFileMode", 0x1B6, true, toRuntime, "0666, which the umask narrows: the mode of a file the runtime makes."},
	{"linuxExecutableFileMode", 0x1ED, true, toRuntime, "0755, which the umask narrows: the mode of a file the runtime makes for FilePermission.executable."},
	{"linuxDirectoryMode", 0x1FF, true, toRuntime, "0777, which the umask narrows: the mode of a directory the runtime makes."},
	{"linuxStatBytes", 144, true, toRuntime, "the size of a struct stat."},
	{"linuxStatModeOffset", 24, true, toRuntime, "where a struct stat holds st_mode, 32 bits."},
	{"linuxStatSizeOffset", 48, true, toRuntime, "where a struct stat holds st_size."},
	{"linuxStatxBytes", 256, true, toRuntime, "the size of a struct statx."},
	{"linuxStatxMaskOffset", 0, true, toRuntime, "where a struct statx holds stx_mask, 32 bits: the fields the kernel filled."},
	{"linuxStatxModeOffset", 28, true, toRuntime, "where a struct statx holds stx_mode, 16 bits."},
	{"linuxStatxSizeOffset", 40, true, toRuntime, "where a struct statx holds stx_size."},
	{"linuxStatxAccessedOffset", 64, true, toRuntime, "where a struct statx holds stx_atime, whose first word is its seconds."},
	{"linuxStatxCreatedOffset", 80, true, toRuntime, "where a struct statx holds stx_btime, whose first word is its seconds."},
	{"linuxStatxModifiedOffset", 112, true, toRuntime, "where a struct statx holds stx_mtime, whose first word is its seconds."},
	{"linuxStatxWanted", 0xFFF, true, toRuntime, "STATX_BASIC_STATS | STATX_BTIME: the fields to fill."},
	{"linuxStatxBirthTime", 0x800, true, toRuntime, "STATX_BTIME: set in stx_mask when the file system records when a file was made."},
	{"linuxModeTypeMask", 0xF000, true, toRuntime, "S_IFMT: the bits of st_mode that say what a file is."},
	{"linuxModeDirectory", 0x4000, true, toRuntime, "S_IFDIR."},
	{"linuxModeOwnerWrite", 0x80, true, toRuntime, "S_IWUSR: a file without it is the read-only one of FileInfo."},
	{"linuxDirentLengthOffset", 16, true, toRuntime, "where a linux_dirent64 holds the length of its record, 16 bits."},
	{"linuxDirentNameOffset", 19, true, toRuntime, "where a linux_dirent64 holds its name, zero-terminated."},

	// The errors told apart, and the signals.
	{"linuxEperm", 1, true, toRuntime, "EPERM."},
	{"linuxEnoent", 2, true, toRuntime, "ENOENT."},
	{"linuxEintr", 4, true, toRuntime, "EINTR: a call that a signal broke off, which is made again."},
	{"linuxEagain", 11, true, toRuntime, "EAGAIN."},
	{"linuxEacces", 13, true, toRuntime, "EACCES."},
	{"linuxEbusy", 16, true, toRuntime, "EBUSY."},
	{"linuxEexist", 17, true, toRuntime, "EEXIST."},
	{"linuxEnotdir", 20, true, toRuntime, "ENOTDIR."},
	{"linuxEisdir", 21, true, toRuntime, "EISDIR."},
	{"linuxEtxtbsy", 26, true, toRuntime, "ETXTBSY."},
	{"linuxEnotempty", 39, true, toRuntime, "ENOTEMPTY."},
	{"linuxSigkill", 9, true, toRuntime, "SIGKILL."},
	{"linuxPollIn", 1, true, toRuntime, "POLLIN."},
	{"linuxPollRecordBytes", 8, true, toRuntime, "a struct pollfd: the fd and the events asked, then the events that came, 16 bits."},
	{"linuxPollEventsOffset", 4, true, toRuntime, "where a struct pollfd holds the events asked."},
	{"linuxPollReturnedOffset", 6, true, toRuntime, "where a struct pollfd holds the events that came."},
}
