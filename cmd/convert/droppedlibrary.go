package main

import "slices"

// maxon's SharedValue, a mutable cell that every thread reads and writes, is not in minimaxon (README, "Where it differs from
// Maxon": there is no SharedValue): a setting that changes lives in one service and changing it is a message, and a reader
// on a hot path keeps its own copy, refreshed by a message. A spec test whose program names it is about the cell, and is
// skipped with this warning, a permanent one.
const sharedValueWarning = "language difference: maxon's SharedValue, a mutable cell that every thread reads and writes, is dropped: a setting that changes lives in one service and changing it is a message, and a reader that needs it on a hot path keeps its own copy, refreshed by a message (README, Where it differs from Maxon: there is no SharedValue)"

// maxon's Log sits on SharedValue (a LogSink's levels) and its design here is not decided. A spec test whose program names
// one of its types is skipped with this warning, a temporary one, and it takes the place of sharedValueWarning on a test that
// has both, since what the test waits for is the library.
const logWarning = "not yet: Log is not designed yet (README, Services: not yet)"

// maxon's TraceCapture is not brought over (README, Where it differs from Maxon). A program that names one of its types is
// warned with this, and a spec test whose program does is left out of the converted spec file whole, whatever else it names.
const traceCaptureWarning = "language difference: TraceCapture is not brought over from maxon (README, Where it differs from Maxon)"

// logLibraryTypes are the types that maxon's Log.maxon and LogHandlers.maxon declare for a program to name.
var logLibraryTypes = []string{
	"Log", "Logger", "LogSink", "LogLevel", "LogLevels", "LogLevelVar", "LogRecord", "LogField", "LogFieldArray", "LogAttr",
	"LogAttrArray", "LogValue", "LogInteger", "LogReal", "LogGroupPath", "LogRank", "LogHandler", "LogStream",
	"LogHandlerOptions", "TextLogHandler", "JsonLogHandler",
}

// traceCaptureTypes are the types that maxon's TraceCapture.maxon declares for a program to name.
var traceCaptureTypes = []string{"TraceCapture", "TraceKeyArray"}

// sharedValueType is the cell's type.
const sharedValueType = "SharedValue"

// ownLogTypes are the types of logLibraryTypes and traceCaptureTypes that a program declares itself, which it keeps: a
// test's own `type Log` is no use of the library's.
func ownLogTypes(lines []sourceLine) []string {
	var own []string
	for _, name := range slices.Concat(logLibraryTypes, traceCaptureTypes) {
		if declaresType(lines, name) {
			own = append(own, name)
		}
	}
	return own
}

// warnDroppedLibrary warns on the line of each use of SharedValue and of a type of the Log library or of TraceCapture's that
// the program does not declare itself (own).
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
			case slices.Contains(own, t.text):
			case slices.Contains(logLibraryTypes, t.text):
				warn(line, logWarning)
			case slices.Contains(traceCaptureTypes, t.text):
				warn(line, traceCaptureWarning)
			}
		}
	}
}
