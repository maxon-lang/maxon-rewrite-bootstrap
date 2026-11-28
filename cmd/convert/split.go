package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A split table says which declarations of a converted file belong to another library module than the file's own: the
// library's layout is by what a programmer uses, while maxon's files hold a type together with the conformances that the
// built-in types need to meet an interface, and the conformances sit with the interfaces' module (README, "Modules"). A line
// is `file | module | declaration [| member ...]`, the fields separated by `|`:
//   - file is the file's name, as stdlib/files.txt gives it;
//   - declaration is the text its first line starts with, after its `public`, `export` and `static` words;
//   - with no member, the declaration moves whole, with the comments just above it;
//   - with members (functions and typealiases, by name) only those move, into a declaration with the same header in the
//     other module, and what stays has the header without its `implements` clause, since a type's conformance is declared
//     once;
//   - what moves and has no visibility of its own becomes `export` (exported).
//
// What a file holds that no line names stays in the file's module. Every line must match, so a table that no longer fits the
// file it names fails the conversion instead of leaving a declaration where it was.
type route struct {
	file    string
	module  string
	header  string
	members []string
}

const (
	routeSeparator = "|"
	routeComment   = "#"
	memberIndent   = "\t"
	endMark        = "end '"
	conditionMark  = "#if"
	conditionEnd   = "#endif"
)

// blockWords are the words that start a declaration that has a body, which ends with `end 'name'`.
var blockWords = []string{"function", "type", serviceKeyword, "enum", "union", "interface", "extension", "test"}

// modifierWords precede the word that starts a declaration or a member.
var modifierWords = []string{"public", "export", "static"}

func readRoutes(path string) ([]route, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var routes []route
	for n, line := range strings.Split(string(text), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), routeComment) {
			continue
		}
		fields := strings.Split(line, routeSeparator)
		if len(fields) < 3 || len(fields) > 4 {
			return nil, fmt.Errorf("%s:%d: a line is 'file | module | declaration' with an optional '| members'", path, n+1)
		}
		r := route{file: strings.TrimSpace(fields[0]), module: strings.TrimSpace(fields[1]), header: strings.TrimSpace(fields[2])}
		if len(fields) == 4 {
			r.members = strings.Fields(fields[3])
		}
		routes = append(routes, r)
	}
	return routes, nil
}

// unit is a declaration of a file with the comments that lead it, or a run of comments no declaration follows.
type unit struct {
	gap    int // blank lines before it
	lines  []string
	header string // its first line that is code, without the indent, modifiers or `implements` clause removed
	body   int    // index in lines of the first line after the header
}

// splitDeclarations moves the declarations of text that the routes for file name to the module's text: the text that stays
// and, for each module in the order its first route is in, the text that moves there.
func splitDeclarations(file, text string, routes []route) (string, []string, map[string]string, error) {
	var mine []route
	for _, r := range routes {
		if r.file == file {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return text, nil, nil, nil
	}
	units := segment(strings.Split(strings.TrimSuffix(text, "\n"), "\n"), "")
	var order []string
	moved := map[string][]unit{}
	for _, r := range mine {
		hit := false
		for i := range units {
			if units[i].header == "" || !strings.HasPrefix(declarationOf(units[i].header), r.header) {
				continue
			}
			hit = true
			if len(r.members) == 0 {
				moved[r.module] = append(moved[r.module], exported(units[i]))
				units[i] = unit{}
				continue
			}
			kept, taken, err := splitMembers(units[i], r.members)
			if err != nil {
				return "", nil, nil, fmt.Errorf("%s: %s: %v", file, r.header, err)
			}
			moved[r.module] = append(moved[r.module], taken)
			units[i] = kept
		}
		if !hit {
			return "", nil, nil, fmt.Errorf("%s: no declaration starts with %q: fix the split table", file, r.header)
		}
		if _, seen := moved[r.module]; seen && !containsString(order, r.module) {
			order = append(order, r.module)
		}
	}
	texts := map[string]string{}
	for module, parts := range moved {
		texts[module] = joinUnits(parts)
	}
	return joinUnits(units), order, texts, nil
}

func containsString(list []string, s string) bool {
	return slices.Contains(list, s)
}

// joinUnits is the units that are left, in order, each after the blank lines it had, and the first after none.
func joinUnits(units []unit) string {
	var out []string
	for _, u := range units {
		if len(u.lines) == 0 {
			continue
		}
		if len(out) > 0 {
			gap := max(u.gap, 1)
			for range gap {
				out = append(out, "")
			}
		}
		out = append(out, u.lines...)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// exported is the unit u declared `export` where it has no visibility of its own: a declaration that moves to another module
// leaves the module-private scope of the file it came from, so what the module it left calls, or what calls it, needs the
// next visibility, which reaches the whole library and no program. An extension has none of its own.
func exported(u unit) unit {
	if u.header == "" || u.body == 0 || firstWord(declarationOf(u.header)) == "extension" {
		return u
	}
	at := u.body - 1
	line := u.lines[at]
	indent := line[:len(line)-len(strings.TrimLeft(line, "\t"))]
	rest := strings.TrimPrefix(line, indent)
	if strings.HasPrefix(rest, "public ") || strings.HasPrefix(rest, "export ") {
		return u
	}
	lines := append([]string(nil), u.lines...)
	lines[at] = indent + "export " + rest
	u.lines = lines
	u.header = "export " + u.header
	return u
}

// declarationOf is the header without the words that modify it.
func declarationOf(header string) string {
	for {
		word, rest, _ := strings.Cut(header, " ")
		if !containsString(modifierWords, word) {
			return header
		}
		header = rest
	}
}

// segment cuts lines, which are laid at the indent, into units. A line that is indented deeper or starts a closing bracket
// continues the unit before it.
func segment(lines []string, indent string) []unit {
	var units []unit
	gap := 0
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			gap++
			i++
			continue
		}
		u := unit{gap: gap}
		gap = 0
		for i < len(lines) && isLeading(lines[i], indent) {
			u.lines = append(u.lines, lines[i])
			i++
		}
		if i == len(lines) || strings.TrimSpace(lines[i]) == "" {
			units = append(units, u)
			continue
		}
		u.body = len(u.lines) + 1
		u.header = strings.TrimPrefix(lines[i], indent)
		u.lines = append(u.lines, lines[i])
		i++
		closing := ""
		switch word := firstWord(declarationOf(u.header)); {
		case strings.HasPrefix(u.header, conditionMark):
			closing = indent + conditionEnd
		case containsString(blockWords, word):
			closing = indent + endMark
		}
		if closing != "" {
			for i < len(lines) && !strings.HasPrefix(lines[i], closing) {
				u.lines = append(u.lines, lines[i])
				i++
			}
			if i < len(lines) {
				u.lines = append(u.lines, lines[i])
				i++
			}
		}
		for i < len(lines) && isContinuation(lines[i], indent) {
			u.lines = append(u.lines, lines[i])
			i++
		}
		units = append(units, u)
	}
	return units
}

func firstWord(s string) string {
	word, _, _ := strings.Cut(s, " ")
	if before, _, ok := strings.Cut(word, "("); ok {
		return before
	}
	return word
}

// isLeading is whether line, at the indent, is a comment or an annotation that leads a declaration.
func isLeading(line, indent string) bool {
	rest, ok := strings.CutPrefix(line, indent)
	return ok && (strings.HasPrefix(rest, "//") || strings.HasPrefix(rest, "@"))
}

func isContinuation(line, indent string) bool {
	rest, ok := strings.CutPrefix(line, indent)
	if !ok || rest == "" {
		return false
	}
	return rest[0] == ' ' || rest[0] == '\t' || strings.ContainsRune(")]}", rune(rest[0]))
}

// splitMembers cuts the members that names lists out of the extension or type u: kept is u without them, and without the
// implements clause of its header, taken is u with only them. Every name must be a member.
func splitMembers(u unit, names []string) (kept, taken unit, err error) {
	last := len(u.lines) - 1
	if u.body == 0 || last < u.body || !strings.HasPrefix(u.lines[last], endMark) {
		return unit{}, unit{}, fmt.Errorf("it is not a declaration with a body")
	}
	members := segment(u.lines[u.body:last], memberIndent)
	var stay, go_ []unit
	found := map[string]bool{}
	for _, m := range members {
		name := memberName(m)
		if name != "" && containsString(names, name) {
			found[name] = true
			go_ = append(go_, exported(m))
		} else {
			stay = append(stay, m)
		}
	}
	for _, n := range names {
		if !found[n] {
			return unit{}, unit{}, fmt.Errorf("it has no member %q", n)
		}
	}
	lead := u.lines[:u.body]
	taken = unit{gap: u.gap, header: u.header, body: u.body, lines: assemble(lead, go_, u.lines[last])}
	if len(stay) == 0 {
		return unit{}, taken, nil
	}
	keptLead := append([]string(nil), lead...)
	keptLead[len(keptLead)-1] = withoutImplements(keptLead[len(keptLead)-1])
	kept = unit{gap: u.gap, header: withoutImplements(u.header), body: u.body, lines: assemble(keptLead, stay, u.lines[last])}
	return kept, taken, nil
}

// assemble is a declaration's lines: its leading lines, each member after the blank lines it had, and its end.
func assemble(lead []string, members []unit, end string) []string {
	out := append([]string(nil), lead...)
	for i, m := range members {
		if i > 0 {
			for range m.gap {
				out = append(out, "")
			}
		}
		out = append(out, m.lines...)
	}
	return append(out, end)
}

// memberName is the name that a function or typealias member declares, or "" for any other.
func memberName(m unit) string {
	if m.header == "" {
		return ""
	}
	decl := declarationOf(m.header)
	word, rest, _ := strings.Cut(decl, " ")
	if word != "function" && word != "typealias" {
		return ""
	}
	end := strings.IndexAny(rest, "( =")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// withoutImplements drops the implements clause of a header: from ` implements` to its ` where` or its end.
func withoutImplements(header string) string {
	at := strings.Index(header, " implements ")
	if at < 0 {
		return header
	}
	if where := strings.Index(header, " where "); where >= 0 && where < at {
		return header
	}
	if where := strings.Index(header[at:], " where "); where >= 0 {
		return header[:at] + header[at+where:]
	}
	return header[:at]
}

// writeSplit writes what a file's split gives under out: the text that stays as out/name, and each module's as
// out/<module>/name.
func writeSplit(out, name string, order []string, texts map[string]string) error {
	for _, module := range order {
		path := filepath.Join(out, module, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(texts[module]), 0o644); err != nil {
			return err
		}
	}
	return nil
}
