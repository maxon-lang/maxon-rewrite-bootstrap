package main

import "strings"

// A mailbox has no capacity in minimaxon (README, "Where it differs from Maxon"): a spawn names none, a send never waits for
// room in it, and a send fails only when the service has stopped, the runtime slowing a sender of a service that is behind
// instead. So `spawn(capacity: n)` is a plain spawn, a send's `sendTimeout:` goes, and an arm of the case `mailboxFull`,
// which no send fails with, goes too. A program that names the case or the capacity anywhere else, or a test whose
// expectation does, is about the full mailbox, and gets this warning, which skips it.
const mailboxWarning = "language difference: a mailbox has no capacity, so a spawn names none, no send fails mailboxFull or waits with sendTimeout:, and the runtime slows a sender of a service that is behind instead (README, Where it differs from Maxon: Services)"

const (
	mailboxFullCase   = "mailboxFull"
	sendTimeoutLabel  = "sendTimeout"
	capacityLabel     = "capacity"
	mailboxCapacityTy = "MailboxCapacity"
	spawnWord         = "spawn"
)

// dropMailboxCapacity rewrites each line's capacities, timed sends and mailboxFull arms, and warns on the line of any other
// use of them.
func dropMailboxCapacity(lines []sourceLine, warn func(*sourceLine, string)) {
	for i := 0; i < len(lines); i++ {
		line := &lines[i]
		if line.removed || len(line.code) == 0 {
			continue
		}
		line.code = dropSpawnCapacities(line.code)
		line.code = dropSendTimeouts(line.code)
		if end, ok := mailboxFullArmEnd(line.code); ok {
			switch {
			case end < len(line.code) && line.code[end].text == ",":
				line.code = append(line.code[:0:0], line.code[end+1:]...)
			case end < len(line.code) && line.code[end].text == "then":
				line.removed = true
				continue
			case end < len(line.code) && line.code[end].kind == tokLabel:
				last := blockEndLine(lines, i, line.code[end].text)
				for j := i; j <= last; j++ {
					lines[j].removed = true
				}
				i = last
				continue
			}
		}
		for _, t := range line.code {
			if t.kind == tokIdent && (t.text == mailboxFullCase || t.text == mailboxCapacityTy || t.text == sendTimeoutLabel) {
				warn(line, mailboxWarning)
				break
			}
		}
	}
}

// dropSpawnCapacities leaves out the `(capacity: n)` of each spawn.
func dropSpawnCapacities(toks []token) []token {
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].text != spawnWord || toks[i+1].text != "(" || toks[i+2].text != capacityLabel || toks[i+3].text != ":" {
			continue
		}
		closing := findMatchingParen(toks, i+1)
		if closing < 0 {
			return toks
		}
		toks = append(toks[:i+1:i+1], toks[closing+1:]...)
	}
	return toks
}

// dropSendTimeouts leaves out each `sendTimeout: d` argument, with the comma that joins it to the others.
func dropSendTimeouts(toks []token) []token {
	for i := 1; i+1 < len(toks); i++ {
		if toks[i].text != sendTimeoutLabel || toks[i+1].text != ":" || (toks[i-1].text != "," && toks[i-1].text != "(") {
			continue
		}
		end := argumentEnd(toks, i+2)
		start := i
		if toks[i-1].text == "," {
			start = i - 1
		} else if end < len(toks) && toks[end].text == "," {
			end++
		}
		toks = append(toks[:start:start], toks[end:]...)
		i = start
	}
	return toks
}

// argumentEnd is the index of the comma or closing bracket that ends the argument starting at from.
func argumentEnd(toks []token, from int) int {
	depth := 0
	for i := from; i < len(toks); i++ {
		switch toks[i].text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return i
			}
			depth--
		case ",":
			if depth == 0 {
				return i
			}
		}
	}
	return len(toks)
}

// mailboxFullArmEnd is, for a line that starts an arm with the pattern mailboxFull (qualified or not, with or without its
// payload), the index of the token after the pattern.
func mailboxFullArmEnd(toks []token) (int, bool) {
	i := 0
	for i+1 < len(toks) && toks[i].kind == tokIdent && toks[i].text != mailboxFullCase && toks[i+1].text == "." {
		i += 2
	}
	if i >= len(toks) || toks[i].text != mailboxFullCase {
		return 0, false
	}
	i++
	if i < len(toks) && toks[i].text == "(" {
		closing := findMatchingParen(toks, i)
		if closing < 0 {
			return 0, false
		}
		i = closing + 1
	}
	return i, true
}

// blockEndLine is the index of the line that closes the block a line opens with the label.
func blockEndLine(lines []sourceLine, open int, label string) int {
	for j := open + 1; j < len(lines); j++ {
		code := lines[j].code
		if len(code) >= 2 && code[0].text == "end" && strings.TrimSpace(code[1].text) == label {
			return j
		}
	}
	return open
}

// expectationNamesMailbox is whether an expectation block of a test names what a mailbox without capacity does not have.
func expectationNamesMailbox(block []string) bool {
	for _, line := range block {
		if strings.Contains(line, mailboxFullCase) || strings.Contains(line, sendTimeoutLabel) || strings.Contains(line, mailboxCapacityTy) {
			return true
		}
	}
	return false
}
