package main

import (
	"path/filepath"
	"slices"
)

// handWrittenMembers are the members of a file of maxon's library that minimaxon's library writes by hand, in a file of the
// converted file's module, by the file's name: a top-level function by its name, a method as `Type.member`, which the
// hand-written file declares in an extension of the type. They are left out of the conversion, with the comments above
// them. HttpClient's send and the functions that hand its socket on are written so (stdlib/http/clientSend.maxon), since a
// socket is a linear value here, which every path must close, where maxon's closes when it dies.
var handWrittenMembers = map[string][]string{
	"HttpClient.maxon": {"HttpClient.send", "transportOver", "boundByTheDeadline"},
}

// dropHandWrittenMembers leaves out the members of handWrittenMembers of the file.
func dropHandWrittenMembers(file string, lines []sourceLine) {
	members := handWrittenMembers[filepath.Base(file)]
	if len(members) == 0 {
		return
	}
	owner := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		if l.indent == "" {
			owner = declaredName(l.code)
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" {
			continue
		}
		name := l.code[declIdx+1].text
		if l.indent == "\t" {
			name = owner + "." + name
		} else if l.indent != "" {
			continue
		}
		if !slices.Contains(members, name) {
			continue
		}
		end := memberEnd(lines, i)
		for j := commentsAbove(lines, i); j <= end; j++ {
			lines[j].removed = true
		}
		i = end
	}
}
