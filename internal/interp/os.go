package interp

import (
	"bytes"
	"context"
	"errors"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// The interpreter's OS entries do what the native ones do through Go's os package, and tell its errors apart by the
// Win32 error under them, through the same table (ir.Win32Failures).

// failureOf is the failure an error of the os package is.
func failureOf(err error) ir.OsFailure {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		return ir.FailureOfWin32(uint32(errno))
	}
	return ir.FailureOther
}

// errorWord sets the error word of an entry that can fail, whose address is its last argument: the case plus 1 of
// the error, or 0 for none.
func (vm *vm) errorWord(c *runtimeCall, word int64) {
	cell := vm.target(c.fr, c.args[len(c.args)-1], "an OS entry's error")
	vm.changed(region{cell: cell})
	cell.v = value{n: word}
}

// fail reports err, when it is not nil, as the case of the operation's error that its failure is, and gives whether
// the operation succeeded.
func (vm *vm) fail(c *runtimeCall, err error, cases ir.FailureCases) bool {
	if err != nil {
		vm.errorWord(c, int64(cases[failureOf(err)])+1)
		return false
	}
	vm.errorWord(c, 0)
	return true
}

// failWith reports a failure that is the case of the operation's error itself.
func (vm *vm) failWith(c *runtimeCall, errorCase int) {
	vm.errorWord(c, int64(errorCase)+1)
}

// stringArray is a new array of the strings.
func (vm *vm) stringArray(texts []string) *object {
	arr := vm.newObject(kindArray)
	for _, text := range texts {
		s := vm.newString(text)
		cl := &cell{holder: arr, v: value{ref: s}}
		s.owner = cl
		arr.elems = append(arr.elems, cl)
	}
	return arr
}

func (vm *vm) entryOsArgs(c *runtimeCall) {
	c.set(value{ref: vm.stringArray(os.Args)})
}

func (vm *vm) entryOsEnvironmentVariable(c *runtimeCall) {
	name := vm.text(c.arg(0), "an environment variable's name")
	val, ok := os.LookupEnv(name)
	if !ok {
		vm.failWith(c, ast.ErrorVariableUnset)
		return
	}
	vm.errorWord(c, 0)
	c.set(value{ref: vm.newString(val)})
}

func (vm *vm) entryOsExecutablePath(c *runtimeCall) {
	path, err := os.Executable()
	if err != nil {
		vm.failWith(c, ast.ErrorPathUnavailable)
		return
	}
	vm.errorWord(c, 0)
	c.set(value{ref: vm.newString(path)})
}

func (vm *vm) entryOsCurrentPath(c *runtimeCall) {
	dir, err := os.Getwd()
	if err != nil {
		panic(diag.Internal("Directory.currentPath: GetCurrentDirectory failed: %v", err))
	}
	c.set(value{ref: vm.newString(dir)})
}

func (vm *vm) entryOsProcessId(c *runtimeCall) {
	c.set(value{n: int64(os.Getpid())})
}

func (vm *vm) entryOsPathFrom(c *runtimeCall) {
	path := vm.text(c.arg(0), "a path")
	for i := range len(path) {
		if path[i] <= ir.LastControlByte || strings.IndexByte(ir.InvalidPathBytes, path[i]) >= 0 {
			vm.failWith(c, ast.ErrorPathInvalidCharacter)
			return
		}
	}
	vm.errorWord(c, 0)
	c.set(value{ref: vm.newString(strings.ReplaceAll(path, string(rune(ir.OtherSeparator)), string(rune(ir.PathSeparator))))})
}

func (vm *vm) entryOsReadText(c *runtimeCall) {
	data, err := os.ReadFile(vm.text(c.arg(0), "a path"))
	if vm.fail(c, err, ir.ReadFailures) {
		c.set(value{ref: vm.newString(string(data))})
	}
}

func (vm *vm) entryOsWriteText(c *runtimeCall) {
	path, text := vm.text(c.arg(0), "a path"), vm.text(c.arg(1), "a file's content")
	vm.fail(c, os.WriteFile(path, []byte(text), 0o644), ir.WriteFailures)
}

func (vm *vm) entryOsWriteBinary(c *runtimeCall) {
	path := vm.text(c.arg(0), "a path")
	content := vm.collection(c.arg(1), kindBytes, "a file's content")
	vm.fail(c, os.WriteFile(path, content.buf, 0o644), ir.WriteFailures)
}

func (vm *vm) entryOsRename(c *runtimeCall) {
	from, to := vm.text(c.arg(0), "a path"), vm.text(c.arg(1), "a path")
	vm.fail(c, os.Rename(from, to), ir.RenameFailures)
}

// entryOsDelete deletes a file and refuses a directory, which os.Remove would remove and the native DeleteFile refuses
// with access denied.
func (vm *vm) entryOsDelete(c *runtimeCall) {
	path := vm.text(c.arg(0), "a path")
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		vm.failWith(c, ir.DeleteFailures[ir.FailureAccessDenied])
		return
	}
	vm.fail(c, os.Remove(path), ir.DeleteFailures)
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (vm *vm) entryOsIsDirectory(c *runtimeCall) {
	c.flag(isDirectory(vm.text(c.arg(0), "a path")))
}

func (vm *vm) entryOsCreateDirectory(c *runtimeCall) {
	path := vm.text(c.arg(0), "a path")
	_ = os.MkdirAll(path, 0o755)
	c.flag(isDirectory(path))
}

func (vm *vm) entryOsList(c *runtimeCall) {
	path := vm.text(c.arg(0), "a path")
	entries, err := os.ReadDir(path)
	if !vm.fail(c, err, ir.ListFailures) {
		return
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = joinPath(path, e.Name())
	}
	c.set(value{ref: vm.stringArray(paths)})
}

// joinPath is maxon's FilePath.join: the name after the path and a separator, unless the path is empty or ends with one.
func joinPath(path, name string) string {
	if path == "" || path[len(path)-1] == ir.PathSeparator {
		return path + name
	}
	return path + string(rune(ir.PathSeparator)) + name
}

// The arguments of rt.os.run that are the addresses its output is stored at.
const (
	runDeadlineArg = 2
	runStdoutArg   = 3
	runStderrArg   = 4
)

// orphanWait is how long a terminated child's output is waited for: a grandchild that inherited its pipes can hold them
// open after the child is gone.
const orphanWait = 100 * time.Millisecond

// entryOsRun runs the child as the native entry does: Go quotes the arguments by the rules the C runtime splits them by.
func (vm *vm) entryOsRun(c *runtimeCall) {
	exe := vm.text(c.arg(0), "an executable")
	argsArr := vm.collection(c.arg(1), kindArray, "the arguments")
	args := make([]string, len(argsArr.elems))
	for i, e := range argsArr.elems {
		args[i] = vm.text(e.v, "an argument")
	}
	ctx := context.Background()
	if ms := c.arg(runDeadlineArg).n; ms != ir.NoDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = orphanWait
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		vm.failWith(c, ast.ErrorTimeout)
		return
	case errors.As(err, &exit):
		err = nil
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist):
		vm.failWith(c, ast.ErrorExecutableNotFound)
		return
	}

	if !vm.fail(c, err, ir.SpawnFailures) {
		return
	}
	vm.output(c, runStdoutArg, stdout.String())
	vm.output(c, runStderrArg, stderr.String())
	c.num(int64(cmd.ProcessState.ExitCode()))
}

// output stores what the child wrote to a stream at the address the argument at index gives.
func (vm *vm) output(c *runtimeCall, index int, text string) {
	cell := vm.target(c.fr, c.args[index], "a child's output")
	vm.changed(region{cell: cell})
	vm.adopt(cell, value{ref: vm.newString(text)}, "a child's output")
}
