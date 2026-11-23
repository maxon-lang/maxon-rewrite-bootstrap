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
	{"linuxSysRtSigaction", 13, true, toRuntime | toSelfBack, "rt_sigaction(signal, action, old action, size of the signal mask)."},
	{"linuxSysRtSigprocmask", 14, true, toRuntime, "rt_sigprocmask(how, set, old set, size of the signal mask)."},
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
	{"linuxSysGettid", 186, true, toRuntime, "gettid(): the calling thread's id, which a signal is sent to."},
	{"linuxSysTgkill", 234, true, toRuntime, "tgkill(process id, thread id, signal): a signal to one thread of a process."},
	{"linuxSysNanosleep", 35, true, toRuntime, "nanosleep(timespec, remaining)."},
	{"linuxSysGetrandom", 318, true, toRuntime, "getrandom(buffer, count, flags): bytes of the system's entropy, which a hash secret is drawn from."},
	{"linuxSysEpollCreate1", 291, true, toRuntime, "epoll_create1(flags): the poller."},
	{"linuxSysEpollCtl", 233, true, toRuntime, "epoll_ctl(epoll fd, operation, fd, event)."},
	{"linuxSysEpollWait", 232, true, toRuntime, "epoll_wait(epoll fd, events, most events, timeout in milliseconds)."},
	{"linuxSysEventfd2", 290, true, toRuntime, "eventfd2(initial count, flags): a counter that the poller sees readable while it is above 0."},

	// epoll and eventfd.
	{"linuxEpollCloexec", 0x80000, true, toRuntime, "EPOLL_CLOEXEC: the poller is closed in a child that execs."},
	{"linuxEventfdFlags", 0x80800, true, toRuntime, "EFD_CLOEXEC | EFD_NONBLOCK: an eventfd closed in a child that execs, whose read of a count of 0 fails at once."},
	{"linuxEpollCtlAdd", 1, true, toRuntime, "EPOLL_CTL_ADD: registers an fd with the poller."},
	{"linuxEpollIn", 1, true, toRuntime, "EPOLLIN: an event for an fd that has something to read."},
	{"linuxEpollEventDataOffset", 4, true, toRuntime, "where a struct epoll_event, which x86-64 packs, holds its 64-bit data word, after the 32-bit events."},
	{"linuxEpollEventBytes", 12, true, toRuntime, "the size of a packed struct epoll_event."},

	// The kernel's struct sigaction: the handler, the flags, the restorer and the mask, a word each.
	{"linuxSigactionHandlerOffset", 0, true, toRuntime | toSelfBack, "where a struct sigaction holds the handler."},
	{"linuxSigactionFlagsOffset", 8, true, toRuntime | toSelfBack, "where a struct sigaction holds the flags."},
	{"linuxSigactionRestorerOffset", 16, true, toRuntime | toSelfBack, "where a struct sigaction holds the restorer, the code the handler returns through, which x86-64 requires."},
	{"linuxSigactionMaskOffset", 24, true, toRuntime | toSelfBack, "where a struct sigaction holds the signals blocked while the handler runs."},
	{"linuxSigactionBytes", 32, true, toRuntime | toSelfBack, "the size of a struct sigaction."},
	{"linuxSignalMaskBytes", 8, true, toRuntime | toSelfBack, "the size of the signal mask that rt_sigaction takes."},
	{"linuxFaultActionFlags", 0x0C000004, true, toSelfBack, "SA_SIGINFO | SA_ONSTACK | SA_RESTORER: the flags of the fault handler's action, which runs on the thread's signal stack and is handed the siginfo and the context."},
	{"linuxPreemptActionFlags", 0x1C000004, true, toRuntime, "SA_SIGINFO | SA_ONSTACK | SA_RESTORER | SA_RESTART: the flags of the preemption signal's action, which also has a system call that the signal broke off made again where it can be."},
	{"linuxSigurg", 23, true, toRuntime, "SIGURG, the signal the monitor stops a thread with, as Go's runtime does: no program of the runtime's asks for it."},
	{"linuxSigUnblock", 1, true, toRuntime, "SIG_UNBLOCK: rt_sigprocmask lets the signals of the set through and leaves the others as they are."},
	// The ucontext_t that a handler is given holds the registers of the code the signal stopped.
	{"linuxUcontextSpOffset", 160, true, toRuntime | toSelfBack, "where a ucontext_t holds the stopped code's stack pointer."},
	{"linuxUcontextPcOffset", 168, true, toRuntime | toSelfBack, "where a ucontext_t holds the address of the stopped code, its program counter."},

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

// archValue is a row of linuxABI with the value it has on another architecture's kernel.
type archValue struct {
	name  string
	value int
}

// The arm64 kernel's numbers where they differ from the x64 one's: its system calls by the generic table
// (include/uapi/asm-generic/unistd.h), and the layouts its records have there. linuxArm64Lacks are the rows of linuxABI
// that it has no call or flag for, and linuxArm64Extra the calls the runtime makes there in their place, each the one
// relative to a directory fd of the call it lacks, or the one with a signal mask. The runtime's source for Linux takes
// each under `#if arch(arm64)`, and the backend's entry stub for arm64 by its name after arm64.
var linuxArm64 = []archValue{
	{"linuxSysRead", 63},
	{"linuxSysWrite", 64},
	{"linuxSysClose", 57},
	{"linuxSysFstat", 80},
	{"linuxSysMmap", 222},
	{"linuxSysMprotect", 226},
	{"linuxSysMunmap", 215},
	{"linuxSysRtSigaction", 134},
	{"linuxSysRtSigprocmask", 135},
	{"linuxSysRtSigreturn", 139},
	{"linuxSysSchedYield", 124},
	{"linuxSysGetpid", 172},
	{"linuxSysClone", 220},
	{"linuxSysExecve", 221},
	{"linuxSysExit", 93},
	{"linuxSysWait4", 260},
	{"linuxSysKill", 129},
	{"linuxSysGetcwd", 17},
	{"linuxSysSigaltstack", 132},
	{"linuxSysFutex", 98},
	{"linuxSysSchedGetaffinity", 123},
	{"linuxSysGetdents64", 61},
	{"linuxSysClockGettime", 113},
	{"linuxSysExitGroup", 94},
	{"linuxSysOpenat", 56},
	{"linuxSysNewfstatat", 79},
	{"linuxSysStatx", 291},
	{"linuxSysReadlinkat", 78},
	{"linuxSysDup3", 24},
	{"linuxSysPipe2", 59},
	{"linuxSysGettid", 178},
	{"linuxSysTgkill", 131},
	{"linuxSysNanosleep", 101},
	{"linuxSysGetrandom", 278},
	{"linuxSysEpollCreate1", 20},
	{"linuxSysEpollCtl", 21},
	{"linuxSysEventfd2", 19},
	// arm64 does not pack a struct epoll_event: its 64-bit data word is aligned after the 32-bit events.
	{"linuxEpollEventDataOffset", 8},
	{"linuxEpollEventBytes", 16},
	// The generic struct stat: st_mode after st_dev and st_ino, 128 bytes in all.
	{"linuxStatBytes", 128},
	{"linuxStatModeOffset", 16},
	// O_DIRECTORY is 040000 on arm64.
	{"linuxOpenDirectory", 0x84000},
	// The ucontext_t: uc_mcontext, a struct sigcontext, is at 176: the fault address, x0 to x30, then sp and pc.
	{"linuxUcontextSpOffset", 432},
	{"linuxUcontextPcOffset", 440},
}

var linuxArm64Lacks = []string{"linuxSysPoll", "linuxSysRename", "linuxSysMkdir", "linuxSysUnlink", "linuxSysArchPrctl", "linuxSysEpollWait", "linuxArchSetFs"}

var linuxArm64Extra = []layoutConst{
	{"linuxSysMkdirat", 34, true, toRuntime, "mkdirat(directory fd, path, mode)."},
	{"linuxSysUnlinkat", 35, true, toRuntime, "unlinkat(directory fd, path, flags), which with no flags refuses a directory."},
	{"linuxSysRenameat2", 276, true, toRuntime, "renameat2(old directory fd, old, new directory fd, new, flags), which with no flags replaces what is at new."},
	{"linuxSysPpoll", 73, true, toRuntime, "ppoll(fds, count, timespec of the timeout or 0 for none, signal mask, size of the mask)."},
	{"linuxSysEpollPwait", 22, true, toRuntime, "epoll_pwait(epoll fd, events, most events, timeout in milliseconds, signal mask, size of the mask)."},
	{"linuxCloneThreadFlags", 0xD0F00, true, toSelfBack, "the worker's flags with CLONE_SYSVSEM, glibc's flags of a thread: the thread shares the undo list of System V semaphores, which no thread of the runtime has, and without the flag qemu-user, which runs arm64-linux under WSL, refuses to make a thread."},
}
