package main

// macOS's numbers that the runtime's macOS source and the entry stub pass to libSystem, which every program on macOS
// reaches the system through: Apple's system calls are not a stable interface. Each is the value of arm64 macOS's
// headers (<sys/fcntl.h>, <sys/stat.h>, <sys/dirent.h>, <sys/errno.h>, <sys/signal.h>, <sys/_types/_ucontext64.h>,
// <arm/_mcontext.h>, <sys/event.h>, <pthread/pthread_impl.h>, <spawn.h>), under maxon's name where its Darwin runtime has
// one (Targets/Arm64/Arm64DarwinRuntime.maxon, Arm64FaultRuntime.maxon).
var macosABI = []layoutConst{
	// The clocks.
	{"macosClockRealtime", 0, true, toRuntime, "CLOCK_REALTIME: the calendar, as the system keeps it."},
	{"macosClockMonotonic", 6, true, toRuntime, "CLOCK_MONOTONIC: the time since the machine booted, sleep included."},
	{"macosClockThreadCpu", 16, true, toRuntime, "CLOCK_THREAD_CPUTIME_ID: the CPU time of the calling thread."},
	{"macosClockUptimeRaw", 8, true, toRuntime, "CLOCK_UPTIME_RAW: clock_gettime_nsec_np's monotonic clock, which stands still while the machine sleeps, as Linux's CLOCK_MONOTONIC does."},
	{"macosTimespecBytes", 16, true, toRuntime, "a timespec: the seconds, then the nanoseconds, a word each."},

	// mmap and mprotect.
	{"macosPageBytes", 16384, true, toRuntime | toSelfBack, "the size of a page of arm64 macOS, which mprotect works in."},
	{"macosProtNone", 0, true, toRuntime | toSelfBack, "PROT_NONE: a page that faults when touched, a stack's guard."},
	{"macosProtReadWrite", 3, true, toRuntime | toSelfBack, "PROT_READ | PROT_WRITE."},
	{"macosMapAnonPrivate", 0x1002, true, toRuntime | toSelfBack, "MAP_ANON | MAP_PRIVATE: zeroed pages of no file."},
	{"macosMapNoReserve", 0x40, true, toRuntime | toSelfBack, "MAP_NORESERVE: a stack's address space, committed a page at a time as it is touched."},
	{"macosMapFixed", 0x10, true, toRuntime, "MAP_FIXED: the mapping replaces what is at the address it is given."},

	// Files.
	{"macosOpenReadOnly", 0x1000000, true, toRuntime, "O_RDONLY | O_CLOEXEC."},
	{"macosOpenReplace", 0x1000601, true, toRuntime, "O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC."},
	{"macosFileMode", 0x1B6, true, toRuntime, "0666, which the umask narrows: the mode of a file the runtime makes."},
	{"macosExecutableFileMode", 0x1ED, true, toRuntime, "0755, which the umask narrows: the mode of a file the runtime makes for FilePermission.executable."},
	{"macosDirectoryMode", 0x1FF, true, toRuntime, "0777, which the umask narrows: the mode of a directory the runtime makes."},
	{"macosPathBytes", 1024, true, toRuntime, "PATH_MAX, the longest path libSystem gives back, with its terminator."},
	{"macosStatBytes", 144, true, toRuntime, "the size of a struct stat, whose inode is 64 bits on arm64."},
	{"macosStatModeOffset", 4, true, toRuntime, "where a struct stat holds st_mode, 16 bits."},
	{"macosStatAccessedOffset", 32, true, toRuntime, "where a struct stat holds st_atimespec, whose first word is its seconds."},
	{"macosStatModifiedOffset", 48, true, toRuntime, "where a struct stat holds st_mtimespec, whose first word is its seconds."},
	{"macosStatCreatedOffset", 80, true, toRuntime, "where a struct stat holds st_birthtimespec, the time the file was made, whose first word is its seconds."},
	{"macosStatSizeOffset", 96, true, toRuntime, "where a struct stat holds st_size."},
	{"macosModeTypeMask", 0xF000, true, toRuntime, "S_IFMT: the bits of st_mode that say what a file is."},
	{"macosModeDirectory", 0x4000, true, toRuntime, "S_IFDIR."},
	{"macosModeCharacter", 0x2000, true, toRuntime, "S_IFCHR: a character device, a terminal among them."},
	{"macosModeOwnerWrite", 0x80, true, toRuntime, "S_IWUSR: a file without it is the read-only one of FileInfo."},
	{"macosPollIn", 1, true, toRuntime, "POLLIN."},
	{"macosPollOut", 4, true, toRuntime, "POLLOUT."},
	{"macosEpipe", 32, true, toRuntime, "EPIPE: a write to a pipe whose reader has closed it."},
	{"macosFcntlGetFlags", 3, true, toRuntime, "F_GETFL."},
	{"macosFcntlSetFlags", 4, true, toRuntime, "F_SETFL."},
	{"macosOpenNonblock", 4, true, toRuntime, "O_NONBLOCK: a write that would wait writes what fits, or fails with EAGAIN."},
	{"macosFcntlSetNoSigpipe", 73, true, toRuntime, "F_SETNOSIGPIPE: a write to the fd whose reader has closed it fails with EPIPE and sends no SIGPIPE."},
	{"macosPollRecordBytes", 8, true, toRuntime, "a struct pollfd: the fd and the events asked, then the events that came, 16 bits."},
	{"macosPollEventsOffset", 4, true, toRuntime, "where a struct pollfd holds the events asked."},
	{"macosPollReturnedOffset", 6, true, toRuntime, "where a struct pollfd holds the events that came."},
	{"macosDirentNameOffset", 21, true, toRuntime, "where a struct dirent that readdir gives holds its name, zero-terminated, after d_ino, d_seekoff, d_reclen, d_namlen and d_type."},

	// The errors told apart, which a call that fails leaves in errno.
	{"macosEperm", 1, true, toRuntime, "EPERM."},
	{"macosEnoent", 2, true, toRuntime, "ENOENT."},
	{"macosEintr", 4, true, toRuntime, "EINTR: a call that a signal broke off, which is made again."},
	{"macosEacces", 13, true, toRuntime, "EACCES."},
	{"macosEbusy", 16, true, toRuntime, "EBUSY."},
	{"macosEexist", 17, true, toRuntime, "EEXIST."},
	{"macosEnotdir", 20, true, toRuntime, "ENOTDIR."},
	{"macosEisdir", 21, true, toRuntime, "EISDIR."},
	{"macosEtxtbsy", 26, true, toRuntime, "ETXTBSY."},
	{"macosEagain", 35, true, toRuntime, "EAGAIN."},
	{"macosEnotempty", 66, true, toRuntime, "ENOTEMPTY."},

	// The signals, and the struct sigaction of libSystem's sigaction, which supplies the trampoline a handler returns
	// through itself: the handler, the mask and the flags, 16 bytes.
	{"macosSigfpe", 8, true, toSelfBack, "SIGFPE."},
	{"macosSigkill", 9, true, toRuntime, "SIGKILL."},
	{"macosSigbus", 10, true, toSelfBack, "SIGBUS: an access a mapped page refuses, which a stack's guard page gives."},
	{"macosSigsegv", 11, true, toSelfBack, "SIGSEGV."},
	{"macosSigurg", 16, true, toRuntime, "SIGURG, the signal the monitor stops a thread with, as Go's runtime does: no program of the runtime's asks for it."},
	{"macosSigUnblock", 2, true, toRuntime, "SIG_UNBLOCK: pthread_sigmask lets the signals of the set, a 32-bit sigset_t, through and leaves the others as they are."},
	{"macosFpeIntegerOverflow", 8, true, toSelfBack, "FPE_INTOVF: the si_code of a SIGFPE of an integer overflow."},
	{"macosSigactionHandlerOffset", 0, true, toRuntime | toSelfBack, "where a struct sigaction holds the handler."},
	{"macosSigactionMaskOffset", 8, true, toRuntime | toSelfBack, "where a struct sigaction holds the signals blocked while the handler runs, 32 bits."},
	{"macosSigactionFlagsOffset", 12, true, toRuntime | toSelfBack, "where a struct sigaction holds the flags, 32 bits."},
	{"macosSigactionBytes", 16, true, toRuntime | toSelfBack, "the size of a struct sigaction."},
	{"macosFaultActionFlags", 0x41, true, toSelfBack, "SA_SIGINFO | SA_ONSTACK: the flags of the fault handler's action, which runs on the thread's signal stack and is handed the siginfo and the context."},
	{"macosPreemptActionFlags", 0x43, true, toRuntime, "SA_SIGINFO | SA_ONSTACK | SA_RESTART: the flags of the preemption signal's action, which also has a call that the signal broke off made again where it can be."},
	{"macosSiginfoCodeOffset", 8, true, toSelfBack, "where a siginfo_t holds si_code, 32 bits."},
	{"macosSiginfoAddressOffset", 24, true, toSelfBack, "where a siginfo_t holds si_addr, the address a fault was at, after six 32-bit fields."},
	{"macosStackSpOffset", 0, true, toSelfBack, "where a stack_t holds ss_sp, the lowest address of the stack."},
	{"macosStackSizeOffset", 8, true, toSelfBack, "where a stack_t holds ss_size."},
	{"macosStackFlagsOffset", 16, true, toSelfBack, "where a stack_t holds ss_flags, 32 bits."},
	{"macosStackRecordBytes", 24, true, toSelfBack, "the size of a stack_t."},
	// The ucontext_t a handler is given holds a pointer to the registers of the code the signal stopped.
	{"macosUcontextMcontextOffset", 48, true, toRuntime | toSelfBack, "where a ucontext_t holds uc_mcontext, the address of the stopped code's registers, after uc_onstack, uc_sigmask, uc_stack, uc_link and uc_mcsize."},
	{"macosMcontextSpOffset", 0x108, true, toRuntime | toSelfBack, "where an mcontext64 holds the stopped code's stack pointer: __ss, after the 16 bytes of __es, holds x0 to x28, fp, lr, sp and pc."},
	{"macosMcontextPcOffset", 0x110, true, toRuntime | toSelfBack, "where an mcontext64 holds the address of the stopped code, its program counter."},

	// kqueue: a struct kevent, and the user event that the poller's wait is broken off and a poll descriptor signalled by.
	{"macosKeventBytes", 32, true, toRuntime, "the size of a struct kevent: ident, filter (16 bits), flags (16 bits), fflags (32 bits), data and udata."},
	{"macosKeventFilterOffset", 8, true, toRuntime, "where a struct kevent holds its filter, 16 bits."},
	{"macosKeventFlagsOffset", 10, true, toRuntime, "where a struct kevent holds its flags, 16 bits."},
	{"macosKeventFflagsOffset", 12, true, toRuntime, "where a struct kevent holds the flags of its filter, 32 bits."},
	{"macosKeventUdataOffset", 24, true, toRuntime, "where a struct kevent holds udata, the word it is reported with."},
	{"macosEvfiltUser", 0xFFF6, true, toRuntime, "EVFILT_USER, -10 in 16 bits: an event that a kevent call triggers, of no fd."},
	{"macosEvAddClear", 0x21, true, toRuntime, "EV_ADD | EV_CLEAR: registers an event that its report resets, so it is reported once for any number of triggers before it."},
	{"macosEvDelete", 0x2, true, toRuntime, "EV_DELETE: takes an event off the kqueue."},
	{"macosNoteTrigger", 0x1000000, true, toRuntime, "NOTE_TRIGGER: the fflags that trigger a user event."},

	// Threads and parking.
	{"macosPthreadAttrBytes", 64, true, toRuntime, "the size of a pthread_attr_t."},
	{"macosPthreadMutexBytes", 64, true, toRuntime, "the size of a pthread_mutex_t."},
	{"macosPthreadCondBytes", 48, true, toRuntime, "the size of a pthread_cond_t."},
	{"macosScNprocessorsOnln", 58, true, toRuntime, "_SC_NPROCESSORS_ONLN: sysconf's count of the processors online."},

	// posix_spawn.
	{"macosSpawnObjectBytes", 8, true, toRuntime, "the size of a posix_spawn_file_actions_t and of a posix_spawnattr_t, each the address of what the init call allocates."},
	{"macosSpawnCloexecDefault", 0x4000, true, toRuntime, "POSIX_SPAWN_CLOEXEC_DEFAULT: the child has only the fds the file actions give it, so no other run's pipe stays open in it."},
}
