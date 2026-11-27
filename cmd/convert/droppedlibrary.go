package main

import "slices"

// maxon's SharedValue, a mutable cell that every thread reads and writes, is not in minimaxon (README, "Where it differs from
// Maxon": there is no SharedValue): a setting that changes lives in one service and changing it is a message, and a reader
// on a hot path keeps its own copy, refreshed by a message. A spec test whose program names it is about the cell, and is
// skipped with this warning, a permanent one.
const sharedValueWarning = "language difference: maxon's SharedValue, a mutable cell that every thread reads and writes, is dropped: a setting that changes lives in one service and changing it is a message, and a reader that needs it on a hot path keeps its own copy, refreshed by a message (README, Where it differs from Maxon: there is no SharedValue)"

// maxon's Log and TraceCapture sit on SharedValue (a LogSink's levels, TraceCapture's switch) and minimaxon has neither: their
// design is deferred until the owner decides how logging goes. A spec test whose program names one of their types is skipped
// with this warning, a temporary one, and it takes the place of sharedValueWarning on a test that has both, since what the
// test waits for is the library.
const logWarning = "not yet: maxon's Log and TraceCapture are not designed, and they sit on SharedValue, which minimaxon drops (README, Services: not yet)"

// logLibraryTypes are the types that maxon's Log.maxon, LogHandlers.maxon and TraceCapture.maxon declare for a program to name.
var logLibraryTypes = []string{
	"Log", "Logger", "LogSink", "LogLevel", "LogLevels", "LogLevelVar", "LogRecord", "LogField", "LogFieldArray", "LogAttr",
	"LogAttrArray", "LogValue", "LogInteger", "LogReal", "LogGroupPath", "LogRank", "LogHandler", "LogStream",
	"LogHandlerOptions", "TextLogHandler", "JsonLogHandler", "TraceCapture", "TraceKeyArray",
}

// sharedValueType is the cell's type.
const sharedValueType = "SharedValue"

// ownLogTypes are the types of logLibraryTypes that a program declares itself, which it keeps: a test's own `type Log` is no
// use of the library's.
func ownLogTypes(lines []sourceLine) []string {
	var own []string
	for _, name := range logLibraryTypes {
		if declaresType(lines, name) {
			own = append(own, name)
		}
	}
	return own
}

// warnDroppedLibrary warns on the line of each use of SharedValue and of a type of the Log library that the program does not
// declare itself (own).
func warnDroppedLibrary(lines []sourceLine, own []string, warn func(*sourceLine, string)) {
	for i := range lines {
		line := &lines[i]
		if line.removed {
			continue
		}
		for k, t := range line.code {
			if t.kind != tokIdent || k > 0 && line.code[k-1].text == "." {
				continue
			}
			switch {
			case t.text == sharedValueType:
				warn(line, sharedValueWarning)
			case slices.Contains(logLibraryTypes, t.text) && !slices.Contains(own, t.text):
				warn(line, logWarning)
			}
		}
	}
}
