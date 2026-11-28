package main

import "strings"

// A status number of the library's StatusCode is a final status's, 200 to 999, where maxon's starts at 100: an informational
// status (1xx) is never an answer's, and only the server's connection sends one (100 Continue), so no answer can carry one
// and no response a client gives its caller does (README, "Where it differs from Maxon"). narrowStatusNumbers writes the
// alias's range so, and drops the arm of statusCodeOf that would map the informational numbers, which the range now excludes.
const (
	statusNumberAlias     = "publictypealiasHttpStatusNumber=int(100to999)"
	finalStatusLow        = "200"
	informationalStatuses = "100upto200or"
)

func narrowStatusNumbers(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed {
			continue
		}
		switch codeText(lines[i].code) {
		case statusNumberAlias:
			for j := range lines[i].code {
				if lines[i].code[j].text == "100" {
					lines[i].code[j].text = finalStatusLow
				}
			}
		case informationalStatuses:
			lines[i].removed = true
		}
	}
}

// codeText is a line's code with its spaces left out.
func codeText(code []token) string {
	var b strings.Builder
	for _, t := range code {
		if t.kind != tokSpace {
			b.WriteString(t.text)
		}
	}
	return b.String()
}
