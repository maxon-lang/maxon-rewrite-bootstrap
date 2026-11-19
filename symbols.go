package main

const (
	RtEntry         = "rt.entry"
	RtAlloc         = "rt.alloc"
	RtWrite         = "rt.write"
	RtPanic         = "rt.panic"
	RtConcat        = "rt.concat"
	RtItoa          = "rt.itoa"
	RtStreq         = "rt.streq"
	RtPanicRange    = "rt.panicRange"
	RtPanicOverflow = "rt.panicOverflow"
	RtPanicDiv      = "rt.panicDiv"
	RtPanicShift    = "rt.panicShift"
	RtHeap          = "g.heap"
	RtStdout        = "g.stdout"
	RtStderr        = "g.stderr"
	RtDll           = "kernel32.dll"
)

var RtImports = []string{"GetProcessHeap", "HeapAlloc", "GetStdHandle", "WriteFile", "ExitProcess"}
