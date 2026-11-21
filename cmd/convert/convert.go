package main

import (
	"fmt"
	"minimaxon/internal/syntax"
	"slices"
	"strings"
)

const (
	utf8BOM          = "\xef\xbb\xbf"
	defaultIndentLen = 4
	todoPrefix       = "// TODO: "
)

// Warning records an unsupported construct or manual follow-up item found during conversion.
type Warning struct {
	File    string
	Line    int
	Message string
}

func (w Warning) String() string {
	return fmt.Sprintf("%s:%d: warning: %s", w.File, w.Line, w.Message)
}

// Result holds the converted source, any warnings emitted, and whether minimaxon's strict formatter accepted the output.
type Result struct {
	Text      string
	Warnings  []Warning
	Formatted bool
}

type sourceLine struct {
	indent   string
	code     []token
	trailing string
	comment  string
	blank    bool
	origLine int
	todos    []string
	removed  bool
}

// Convert converts existing Maxon source code to minimaxon syntax and layout.
func Convert(file, src string) Result {
	text := normalizeText(src)
	if strings.TrimSpace(text) == "" {
		return Result{Text: "", Formatted: true}
	}

	text = convertBlockComments(text)
	text = joinBracketLines(text)

	lines := splitLines(text)
	var warnings []Warning
	addWarn := func(line *sourceLine, msg string) {
		if slices.Contains(line.todos, msg) {
			return
		}
		line.todos = append(line.todos, msg)
		warnings = append(warnings, Warning{File: file, Line: line.origLine, Message: msg})
	}

	joinEndOtherwise(lines)
	rewriteDestructors(lines)
	transformLines(lines, addWarn)

	candidate := renderFallbackLayout(lines)
	if formatted, errs := syntax.Format(file, candidate); len(errs) == 0 {
		return Result{Text: formatted, Warnings: warnings, Formatted: true}
	}
	return Result{Text: candidate, Warnings: warnings, Formatted: false}
}

// normalizeText strips BOM, converts CRLF/CR to LF, converts leading space indentation to tabs, and trims trailing spaces.
func normalizeText(src string) string {
	src = strings.TrimPrefix(src, utf8BOM)
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")

	rawLines := strings.Split(src, "\n")
	spaceUnit := detectSpaceIndent(rawLines)

	var out strings.Builder
	out.Grow(len(src))
	for i, line := range rawLines {
		trimmedRight := strings.TrimRight(line, " \t")
		if trimmedRight != "" {
			out.WriteString(convertLeadingSpaces(trimmedRight, spaceUnit))
		}
		if i < len(rawLines)-1 {
			out.WriteByte('\n')
		}
	}
	res := out.String()
	if res != "" && !strings.HasSuffix(res, "\n") {
		res += "\n"
	}
	return res
}

func detectSpaceIndent(lines []string) int {
	minSpaces := 0
	for _, line := range lines {
		if len(line) == 0 || line[0] != ' ' {
			continue
		}
		spaces := 0
		for spaces < len(line) && line[spaces] == ' ' {
			spaces++
		}
		if spaces == len(line) {
			continue
		}
		if minSpaces == 0 || spaces < minSpaces {
			minSpaces = spaces
		}
	}
	if minSpaces >= 2 {
		return minSpaces
	}
	return defaultIndentLen
}

func convertLeadingSpaces(line string, spaceUnit int) string {
	i := 0
	tabs := 0
	spaces := 0
	for i < len(line) {
		switch line[i] {
		case '\t':
			if spaces > 0 {
				tabs += (spaces + spaceUnit - 1) / spaceUnit
				spaces = 0
			}
			tabs++
			i++
		case ' ':
			spaces++
			i++
		default:
			goto done
		}
	}
done:
	if spaces > 0 {
		tabs += (spaces + spaceUnit - 1) / spaceUnit
	}
	if tabs == 0 {
		return line[i:]
	}
	return strings.Repeat("\t", tabs) + line[i:]
}

// convertBlockComments rewrites /* ... */ comments into // line comments, since minimaxon rejects block comments.
func convertBlockComments(src string) string {
	if !strings.Contains(src, "/*") {
		return src
	}
	toks := lex(src)
	var out strings.Builder
	out.Grow(len(src))

	// Process line by line in the token stream so embedded block comments can be hoisted above their code line.
	i := 0
	for i < len(toks) {
		lineStart := i
		for i < len(toks) && toks[i].kind != tokNewline {
			i++
		}
		lineEnd := i
		hasNewline := i < len(toks) && toks[i].kind == tokNewline
		if hasNewline {
			i++
		}

		lineToks := toks[lineStart:lineEnd]
		hasBlock := false
		for _, t := range lineToks {
			if t.kind == tokBlockComment {
				hasBlock = true
				break
			}
		}
		if !hasBlock {
			for _, t := range lineToks {
				out.WriteString(t.text)
			}
			if hasNewline {
				out.WriteByte('\n')
			}
			continue
		}

		indent := ""
		if len(lineToks) > 0 && lineToks[0].kind == tokSpace {
			indent = lineToks[0].text
		}

		hasCode := false
		for _, t := range lineToks {
			if t.kind != tokSpace && t.kind != tokBlockComment && t.kind != tokLineComment {
				hasCode = true
				break
			}
		}

		if !hasCode {
			for _, t := range lineToks {
				if t.kind == tokBlockComment {
					for _, cl := range blockCommentLines(t.text) {
						out.WriteString(indent)
						out.WriteString(cl)
						out.WriteByte('\n')
					}
				} else if t.kind == tokLineComment {
					out.WriteString(indent)
					out.WriteString(t.text)
					out.WriteByte('\n')
				}
			}
			continue
		}

		var hoisted []string
		var codeBuf strings.Builder
		for idx, t := range lineToks {
			if t.kind != tokBlockComment {
				codeBuf.WriteString(t.text)
				continue
			}
			isTrailing := true
			for k := idx + 1; k < len(lineToks); k++ {
				if lineToks[k].kind != tokSpace {
					isTrailing = false
					break
				}
			}
			clines := blockCommentLines(t.text)
			if isTrailing && len(clines) == 1 {
				if codeBuf.Len() > 0 && codeBuf.String()[codeBuf.Len()-1] != ' ' {
					codeBuf.WriteByte(' ')
				}
				codeBuf.WriteString(clines[0])
			} else {
				hoisted = append(hoisted, clines...)
				if codeBuf.Len() > 0 && codeBuf.String()[codeBuf.Len()-1] != ' ' {
					codeBuf.WriteByte(' ')
				}
			}
		}
		for _, cl := range hoisted {
			out.WriteString(indent)
			out.WriteString(cl)
			out.WriteByte('\n')
		}
		out.WriteString(strings.TrimRight(codeBuf.String(), " \t"))
		if hasNewline {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func blockCommentLines(raw string) []string {
	inner := strings.TrimPrefix(raw, "/*")
	inner = strings.TrimSuffix(inner, "*/")
	parts := strings.Split(inner, "\n")
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		trimmed = strings.TrimPrefix(trimmed, "*")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			lines = append(lines, "//")
		} else {
			lines = append(lines, "// "+trimmed)
		}
	}
	return lines
}

// joinBracketLines collapses line breaks inside (), [] and {} onto one line, preserving nested match/otherwise blocks.
func joinBracketLines(src string) string {
	toks := lex(src)
	var out strings.Builder
	out.Grow(len(src))

	bracketDepth := 0
	blockInBracket := 0
	var hoistedComments []string
	stmtStartPos := 0
	stmtIndent := ""
	atLineStart := true

	flushHoisted := func() {
		if len(hoistedComments) == 0 {
			return
		}
		current := out.String()
		before := current[:stmtStartPos]
		after := current[stmtStartPos:]
		out.Reset()
		out.WriteString(before)
		for _, c := range hoistedComments {
			out.WriteString(stmtIndent)
			out.WriteString(c)
			out.WriteByte('\n')
		}
		out.WriteString(after)
		hoistedComments = hoistedComments[:0]
	}

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if atLineStart && bracketDepth == 0 {
			stmtStartPos = out.Len()
			if t.kind == tokSpace {
				stmtIndent = t.text
			} else {
				stmtIndent = ""
			}
		}

		if t.kind == tokPunct {
			switch t.text {
			case "(", "[", "{":
				bracketDepth++
			case ")", "]", "}":
				if bracketDepth > 0 {
					bracketDepth--
				}
			}
		}

		if bracketDepth > 0 {
			if t.kind == tokIdent && (t.text == "match" || t.text == "otherwise") && lineEndsWithLabel(toks, i) {
				blockInBracket++
			} else if blockInBracket > 0 && t.kind == tokIdent && t.text == "end" {
				blockInBracket--
			}
		}

		if bracketDepth > 0 && blockInBracket == 0 {
			switch t.kind {
			case tokLineComment:
				hoistedComments = append(hoistedComments, strings.TrimSpace(t.text))
				continue
			case tokNewline:
				// Skip leading whitespace on the continuation line and join with a single space.
				for i+1 < len(toks) && (toks[i+1].kind == tokSpace || toks[i+1].kind == tokNewline) {
					i++
				}
				if i+1 < len(toks) && toks[i+1].kind == tokLineComment {
					continue
				}
				cur := out.String()
				if len(cur) > 0 && cur[len(cur)-1] != ' ' && cur[len(cur)-1] != '\t' && cur[len(cur)-1] != '\n' {
					out.WriteByte(' ')
				}
				atLineStart = false
				continue
			}
		}

		out.WriteString(t.text)
		if t.kind == tokNewline {
			if bracketDepth == 0 {
				flushHoisted()
			}
			atLineStart = true
		} else if t.kind != tokSpace {
			atLineStart = false
		}
	}
	flushHoisted()
	return out.String()
}

func lineEndsWithLabel(toks []token, from int) bool {
	lastCode := -1
	for i := from; i < len(toks) && toks[i].kind != tokNewline; i++ {
		if toks[i].kind != tokSpace && toks[i].kind != tokLineComment {
			lastCode = i
		}
	}
	return lastCode >= 0 && toks[lastCode].kind == tokLabel
}

func splitLines(src string) []sourceLine {
	toks := lex(src)
	var lines []sourceLine
	lineNum := 1
	i := 0
	for i < len(toks) {
		start := i
		for i < len(toks) && toks[i].kind != tokNewline {
			i++
		}
		end := i
		if i < len(toks) && toks[i].kind == tokNewline {
			i++
		}

		sl := sourceLine{origLine: lineNum}
		lineNum++
		seg := toks[start:end]
		if len(seg) > 0 && seg[0].kind == tokSpace {
			sl.indent = seg[0].text
			seg = seg[1:]
		}
		for _, t := range seg {
			switch t.kind {
			case tokSpace:
			case tokLineComment:
				if len(sl.code) == 0 {
					sl.comment = strings.TrimSpace(t.text)
				} else {
					sl.trailing = strings.TrimSpace(t.text)
				}
			default:
				sl.code = append(sl.code, t)
			}
		}
		if len(sl.code) == 0 && sl.comment == "" {
			sl.blank = true
		}
		lines = append(lines, sl)
	}
	return lines
}

// joinEndOtherwise joins old Maxon's `end 'label'\notherwise ...` onto one line as minimaxon requires.
func joinEndOtherwise(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed || len(lines[i].code) == 0 || lines[i].code[0].text != "end" {
			continue
		}
		// Only join when `end` does not already have `else` or `otherwise` on the same line.
		if len(lines[i].code) > 2 {
			continue
		}
		next := nextCodeLine(lines, i+1)
		if next < 0 || lines[next].indent != lines[i].indent {
			continue
		}
		if len(lines[next].code) > 0 && lines[next].code[0].text == "otherwise" {
			lines[i].code = append(lines[i].code, lines[next].code...)
			if lines[next].trailing != "" && lines[i].trailing == "" {
				lines[i].trailing = lines[next].trailing
			}
			lines[next].removed = true
		}
	}
}

func nextCodeLine(lines []sourceLine, from int) int {
	for i := from; i < len(lines); i++ {
		if !lines[i].removed && len(lines[i].code) > 0 {
			return i
		}
	}
	return -1
}

// rewriteDestructors turns `consuming self` into `destructs self` and removes `destruct self` statements
// inside methods that call `destruct self`.
func rewriteDestructors(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed || len(lines[i].code) == 0 {
			continue
		}
		fnIdx := funcKeywordIndex(lines[i].code)
		if fnIdx < 0 {
			continue
		}
		lp := findToken(lines[i].code, fnIdx+1, "(")
		if lp < 0 || lp+2 >= len(lines[i].code) {
			continue
		}
		if lines[i].code[lp+1].text != "consuming" || lines[i].code[lp+2].text != "self" {
			continue
		}

		// Scan body up to the matching `end` at the same indentation.
		fnIndent := lines[i].indent
		hasDestruct := false
		for j := i + 1; j < len(lines); j++ {
			if lines[j].removed || len(lines[j].code) == 0 {
				continue
			}
			if lines[j].indent == fnIndent && lines[j].code[0].text == "end" {
				break
			}
			if len(lines[j].code) == 2 && lines[j].code[0].text == "destruct" && lines[j].code[1].text == "self" {
				hasDestruct = true
				lines[j].removed = true
			}
		}
		if hasDestruct {
			lines[i].code[lp+1].text = "destructs"
		} else {
			lines[i].code[lp+1].text = "consumes"
		}
	}
}

func transformLines(lines []sourceLine, warn func(*sourceLine, string)) {
	type blockKind int
	const (
		blockOther blockKind = iota
		blockType
		blockEnum
		blockUnion
		blockInterface
	)
	type openBlock struct {
		indent string
		kind   blockKind
	}
	var stack []openBlock

	currentBlock := func() blockKind {
		if len(stack) == 0 {
			return blockOther
		}
		return stack[len(stack)-1].kind
	}

	for i := range lines {
		line := &lines[i]
		if line.removed || len(line.code) == 0 {
			continue
		}

		// Pop blocks closed by `end` at matching indentation.
		if line.code[0].text == "end" {
			for len(stack) > 0 && len(stack[len(stack)-1].indent) >= len(line.indent) {
				stack = stack[:len(stack)-1]
			}
		}

		first := line.code[0]
		switch first.kind {
		case tokShebang:
			warn(line, "shebang lines are not supported in minimaxon")
			continue
		case tokHashDirective:
			warn(line, "conditional compilation directives (#if/#else/#endif) are not supported in minimaxon")
			continue
		case tokAnnot:
			warn(line, fmt.Sprintf("%s annotations are not supported in minimaxon", first.text))
		}

		// Determine declaration keyword (skipping optional visibility / static / @annot).
		declIdx := skipModifiers(line.code)
		declWord := ""
		if declIdx < len(line.code) && line.code[declIdx].kind == tokIdent {
			declWord = line.code[declIdx].text
		}

		switch declWord {
		case "type":
			transformTypeOrUnionHeader(line, declIdx, warn)
			stack = append(stack, openBlock{indent: line.indent, kind: blockType})
			continue
		case "union":
			transformTypeOrUnionHeader(line, declIdx, warn)
			stack = append(stack, openBlock{indent: line.indent, kind: blockUnion})
			continue
		case "enum":
			transformEnumHeader(line, declIdx, warn)
			stack = append(stack, openBlock{indent: line.indent, kind: blockEnum})
			continue
		case "interface":
			transformInterfaceHeader(line, declIdx, warn)
			stack = append(stack, openBlock{indent: line.indent, kind: blockInterface})
			continue
		case "function":
			if !isClosureLine(line.code, declIdx) {
				transformFunctionHeader(line, declIdx, currentBlock() == blockInterface, warn)
				if currentBlock() != blockInterface {
					stack = append(stack, openBlock{indent: line.indent, kind: blockOther})
				}
				continue
			}
		case "typealias":
			if currentBlock() == blockInterface || currentBlock() == blockType {
				warn(line, "nested typealias declarations are not supported in minimaxon")
			}
			transformTypealias(line, declIdx, warn)
			continue
		case "extension":
			transformWhereInSlice(line.code, declIdx+1, line, warn)
			warn(line, "extension blocks are not supported in minimaxon")
			stack = append(stack, openBlock{indent: line.indent, kind: blockOther})
			continue
		case "test":
			if len(line.code) >= 2 && line.code[1].kind == tokLabel {
				warn(line, "test declarations are not supported in minimaxon")
				stack = append(stack, openBlock{indent: line.indent, kind: blockOther})
				continue
			}
		case "default":
			if line.indent == "" && len(line.code) >= 3 && line.code[2].text == "=" {
				warn(line, "top-level default declarations are not supported in minimaxon")
				continue
			}
		case "let", "var":
			if hasStaticModifier(line.code, declIdx) {
				warn(line, "static fields (static let) are not supported in minimaxon")
			}
		}

		// Inside enum/union body: check case raw values.
		if currentBlock() == blockEnum {
			checkEnumCase(line, warn)
		} else if currentBlock() == blockUnion {
			checkUnionCase(line, warn)
		}

		// Statement and expression transformations.
		transformStmtAndExpr(line, warn)
	}
}

func skipModifiers(toks []token) int {
	i := 0
	for i < len(toks) {
		if toks[i].kind == tokAnnot {
			i++
			continue
		}
		if toks[i].kind == tokIdent && (toks[i].text == "export" || toks[i].text == "public" || toks[i].text == "static") {
			i++
			continue
		}
		break
	}
	return i
}

func hasStaticModifier(toks []token, before int) bool {
	for i := range before {
		if toks[i].kind == tokIdent && toks[i].text == "static" {
			return true
		}
	}
	return false
}

func funcKeywordIndex(toks []token) int {
	idx := skipModifiers(toks)
	if idx < len(toks) && toks[idx].kind == tokIdent && toks[idx].text == "function" {
		return idx
	}
	return -1
}

func isClosureLine(toks []token, fnIdx int) bool {
	// A closure is `function(...) gives ...` without a name after `function`.
	return fnIdx+1 < len(toks) && toks[fnIdx+1].text == "("
}

func findToken(toks []token, from int, text string) int {
	for i := from; i < len(toks); i++ {
		if toks[i].text == text {
			return i
		}
	}
	return -1
}

func findMatchingParen(toks []token, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(toks); i++ {
		switch toks[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func transformTypeOrUnionHeader(line *sourceLine, kwIdx int, warn func(*sourceLine, string)) {
	toks := line.code
	// `type Foo is unique` -> `type Foo is noncopyable`
	if kwIdx+3 < len(toks) && toks[kwIdx+2].text == "is" && toks[kwIdx+3].text == "unique" {
		toks[kwIdx+3].text = "noncopyable"
	}
	transformWhereInSlice(toks, kwIdx+2, line, warn)
}

func transformEnumHeader(line *sourceLine, kwIdx int, warn func(*sourceLine, string)) {
	transformWhereInSlice(line.code, kwIdx+2, line, warn)
}

func transformInterfaceHeader(line *sourceLine, kwIdx int, warn func(*sourceLine, string)) {
	for i := kwIdx + 2; i < len(line.code); i++ {
		if line.code[i].kind == tokIdent && line.code[i].text == "extends" {
			warn(line, "interface inheritance (extends) is not supported in minimaxon")
		}
		if line.code[i].kind == tokIdent && line.code[i].text == "uses" {
			warn(line, "generic interfaces (uses on interface) are not supported in minimaxon")
		}
	}
}

// transformWhereInSlice rewrites `where T is Bound` into `where T implements Bound`.
func transformWhereInSlice(toks []token, from int, line *sourceLine, warn func(*sourceLine, string)) {
	whereIdx := -1
	for i := from; i < len(toks); i++ {
		if toks[i].kind == tokIdent && toks[i].text == "where" {
			whereIdx = i
			break
		}
	}
	if whereIdx < 0 {
		return
	}
	depth := 0
	for i := whereIdx + 1; i < len(toks); i++ {
		switch toks[i].text {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		case "is":
			if depth == 0 && i > whereIdx+1 && toks[i-1].kind == tokIdent {
				toks[i].text = "implements"
			}
		case "sendable", "isolated":
			if depth == 0 {
				warn(line, fmt.Sprintf("region bound '%s' in where clause is not supported in minimaxon", toks[i].text))
			}
		}
	}
}

func transformFunctionHeader(line *sourceLine, fnIdx int, inInterface bool, warn func(*sourceLine, string)) {
	if inInterface && hasStaticModifier(line.code, fnIdx) {
		warn(line, "static functions in interfaces are not supported in minimaxon")
	}

	toks := line.code
	lp := findToken(toks, fnIdx+1, "(")
	if lp < 0 {
		return
	}
	rp := findMatchingParen(toks, lp)
	if rp < 0 {
		return
	}

	// Rewrite parameter list between lp+1 and rp.
	newParams := rewriteParamList(toks[lp+1:rp], line, warn)
	rebuilt := make([]token, 0, len(toks)+4)
	rebuilt = append(rebuilt, toks[:lp+1]...)
	rebuilt = append(rebuilt, newParams...)
	rebuilt = append(rebuilt, toks[rp:]...)
	line.code = rebuilt

	// Inspect clauses after ')'.
	after := lp + 1 + len(newParams) + 1
	transformWhereInSlice(line.code, after, line, warn)
	for i := after; i < len(line.code); i++ {
		t := line.code[i]
		if t.kind != tokIdent {
			continue
		}
		switch t.text {
		case "returns":
			if i+1 < len(line.code) && isCapabilityWord(line.code[i+1].text) {
				warn(line, fmt.Sprintf("return type capability '%s' is not supported in minimaxon", line.code[i+1].text))
			}
			if i+1 < len(line.code) && line.code[i+1].text == "(" {
				warn(line, "tuple return types are not supported in minimaxon")
			}
			if i+1 < len(line.code) && line.code[i+1].text == "float" {
				warn(line, "float types are not supported in minimaxon")
			}
		}
	}
}

func rewriteParamList(params []token, line *sourceLine, warn func(*sourceLine, string)) []token {
	if len(params) == 0 {
		return params
	}
	// Split params by top-level commas.
	var groups [][]token
	depth := 0
	start := 0
	for i, t := range params {
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				groups = append(groups, params[start:i])
				start = i + 1
			}
		}
	}
	groups = append(groups, params[start:])

	var out []token
	for gIdx, g := range groups {
		if gIdx > 0 {
			out = append(out, token{kind: tokPunct, text: ","})
		}
		if len(g) == 0 {
			continue
		}
		// Receiver parameter: `mutable self`, `consuming self`, `isolated self`, `immutable self`
		if gIdx == 0 && len(g) == 2 && g[1].text == "self" {
			switch g[0].text {
			case "mutable":
				g[0].text = "mutates"
			case "consuming":
				g[0].text = "consumes"
			case "isolated", "immutable":
				warn(line, fmt.Sprintf("receiver capability '%s self' is not supported in minimaxon", g[0].text))
			}
			out = append(out, g...)
			continue
		}

		// Check for default parameter value `= ...`
		for _, t := range g {
			if t.text == "=" {
				warn(line, "default parameter values are not supported in minimaxon")
				break
			}
		}

		// Check `name mutable Type...` -> `mutates name Type...`
		if len(g) >= 3 && g[0].kind == tokIdent && g[1].kind == tokIdent && g[1].text == "mutable" {
			rewritten := make([]token, 0, len(g))
			rewritten = append(rewritten, token{kind: tokIdent, text: "mutates"})
			rewritten = append(rewritten, g[0])
			rewritten = append(rewritten, g[2:]...)
			g = rewritten
		}

		// Check for capability qualifiers or float/tuple in parameter type.
		nameIdx := 0
		if len(g) >= 3 && (g[0].text == "mutates" || g[0].text == "consumes" || g[0].text == "sets") {
			nameIdx = 1
		}
		for k := nameIdx + 1; k < len(g); k++ {
			if g[k].kind == tokIdent && isCapabilityWord(g[k].text) {
				warn(line, fmt.Sprintf("parameter capability '%s' is not supported in minimaxon", g[k].text))
			}
			if g[k].kind == tokIdent && g[k].text == "float" {
				warn(line, "float types are not supported in minimaxon")
			}
		}
		if nameIdx < len(g) && g[nameIdx].kind == tokIdent && isNewReservedWord(g[nameIdx].text) {
			warn(line, fmt.Sprintf("'%s' is a reserved keyword in minimaxon; rename it", g[nameIdx].text))
		}
		out = append(out, g...)
	}
	return out
}

func transformTypealias(line *sourceLine, kwIdx int, warn func(*sourceLine, string)) {
	toks := line.code
	eqIdx := findToken(toks, kwIdx+1, "=")
	if eqIdx < 0 || eqIdx+1 >= len(toks) {
		return
	}
	rhs := toks[eqIdx+1:]

	// Check unsupported typealias RHS forms.
	switch rhs[0].text {
	case "float":
		warn(line, "float typealiases are not supported in minimaxon")
	case "bits":
		if len(rhs) > 1 && rhs[1].text == "(" {
			warn(line, "bits(...) typealiases are not supported in minimaxon")
		}
	case "function":
		warn(line, "function typealiases are not supported in minimaxon")
	case "(":
		warn(line, "tuple typealiases are not supported in minimaxon")
	case "isolated", "immutable":
		warn(line, fmt.Sprintf("capability typealias '%s' is not supported in minimaxon", rhs[0].text))
	}

	// Check alias subtyping `implements`.
	implIdx := -1
	for i := eqIdx + 1; i < len(toks); i++ {
		if toks[i].kind == tokIdent && toks[i].text == "implements" {
			implIdx = i
			warn(line, "alias subtyping (implements on typealias) is not supported in minimaxon")
			break
		}
	}

	limit := len(toks)
	if implIdx >= 0 {
		limit = implIdx
	}
	withIdx := -1
	for i := eqIdx + 1; i < limit; i++ {
		if toks[i].kind == tokIdent && toks[i].text == "with" {
			withIdx = i
			break
		}
	}
	if withIdx >= 0 && withIdx+1 < limit {
		if toks[withIdx+1].kind == tokInt {
			warn(line, "fixed-size generic type arguments (e.g. FixedArray with N T) are not supported in minimaxon")
		}
		for i := withIdx + 1; i < limit; i++ {
			if toks[i].kind == tokIdent && isCapabilityWord(toks[i].text) {
				warn(line, fmt.Sprintf("type argument capability '%s' is not supported in minimaxon", toks[i].text))
			}
		}
	}
}

func checkEnumCase(line *sourceLine, warn func(*sourceLine, string)) {
	eqIdx := findToken(line.code, 0, "=")
	if eqIdx < 0 || eqIdx+1 >= len(line.code) {
		return
	}
	val := line.code[eqIdx+1]
	if val.text == "-" && eqIdx+2 < len(line.code) {
		val = line.code[eqIdx+2]
	}
	if val.kind != tokInt {
		warn(line, "non-integer enum raw values are not supported in minimaxon")
	}
}

func checkUnionCase(line *sourceLine, warn func(*sourceLine, string)) {
	// Skip lines that are methods or end.
	if line.code[0].text == "end" || funcKeywordIndex(line.code) >= 0 {
		return
	}
	depth := 0
	for _, t := range line.code {
		switch t.text {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		case "=":
			if depth == 0 {
				warn(line, "explicit raw values on union cases are not supported in minimaxon")
			}
		}
		if t.kind == tokIdent && isCapabilityWord(t.text) {
			warn(line, fmt.Sprintf("capability qualifier '%s' on union payload is not supported in minimaxon", t.text))
		}
	}
}

func transformStmtAndExpr(line *sourceLine, warn func(*sourceLine, string)) {
	toks := line.code

	// Rewrite `freeze <unary_expr>` -> `freeze(<unary_expr>)` and `swap <target> with <expr>` -> `exchange(<target>, value: <expr>)`.
	toks = rewriteRegionExprs(toks, line, warn)
	line.code = toks

	// Statement-head checks.
	if len(toks) > 0 {
		switch toks[0].text {
		case "if":
			if len(toks) > 1 && (toks[1].text == "try" || ((toks[1].text == "let" || toks[1].text == "var") && hasTokenBeforeLabel(toks, "try"))) {
				warn(line, "if try is not supported in minimaxon")
			}
		case "modify":
			if toks[len(toks)-1].kind == tokLabel {
				warn(line, "modify blocks are not supported in minimaxon")
			}
		case "acquire":
			if toks[len(toks)-1].kind == tokLabel {
				warn(line, "acquire blocks are not supported in minimaxon; use when with cowns")
			}
		case "for":
			if len(toks) > 1 && toks[1].text == "(" {
				warn(line, "tuple loop variables in for loops are not supported in minimaxon")
			}
			if !hasRangeKeyword(toks) {
				warn(line, "iterator-based for loops are not supported in minimaxon; use 'for i in a to|upto b'")
			}
		case "let", "var":
			if len(toks) > 1 && toks[1].text == "(" {
				warn(line, "tuple destructuring declarations are not supported in minimaxon")
			}
			if len(toks) > 1 && toks[1].kind == tokIdent && isNewReservedWord(toks[1].text) {
				warn(line, fmt.Sprintf("'%s' is a reserved keyword in minimaxon; rename it", toks[1].text))
			}
		case "(":
			if hasTopLevelAssign(toks) {
				warn(line, "tuple assignment is not supported in minimaxon")
			}
		case "end":
			if len(toks) >= 3 && toks[len(toks)-1].kind != tokLabel {
				for _, t := range toks {
					if t.text == "otherwise" {
						// Check for terminal `end 'x' otherwise panic(...)` or `otherwise throws ...`.
						if hasToken(toks, "panic") || hasToken(toks, "throws") {
							warn(line, "terminal 'otherwise panic/throws' on a try block is not supported in minimaxon; use a labelled otherwise block")
						}
						break
					}
				}
			}
		}
	}

	// Token-level checks across the line.
	for i, t := range toks {
		switch t.kind {
		case tokFloat:
			warn(line, "float literals are not supported in minimaxon")
		case tokByteString:
			warn(line, "byte string literals are not supported in minimaxon")
		case tokString:
			if hasFormatSpecifier(t.text) {
				warn(line, "string interpolation format specifiers are not supported in minimaxon")
			}
		case tokLabel:
			if isCharLiteralPosition(toks, i) {
				warn(line, "character literals are not supported in minimaxon")
			}
		case tokPunct:
			if t.text == "[" && isCollectionLiteralBracket(toks, i) {
				warn(line, "array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert")
			}
		case tokIdent:
			switch t.text {
			case "async", "await":
				if i == 0 || toks[i-1].text != "." {
					warn(line, fmt.Sprintf("'%s' is not supported in minimaxon; use cown/when concurrency", t.text))
				}
			case "spawn":
				if (i == 0 || toks[i-1].text != ".") && i+1 < len(toks) && (toks[i+1].kind == tokIdent || toks[i+1].text == "(") {
					warn(line, "spawn services are not supported in minimaxon; use cown/when concurrency")
				}
			case "sizeof", "countof":
				if i+1 < len(toks) && toks[i+1].text == "(" {
					warn(line, fmt.Sprintf("%s(...) is not supported in minimaxon", t.text))
				}
			case "from":
				if i > 0 && toks[i-1].kind == tokIdent && (i < 2 || toks[i-2].text != ".") {
					warn(line, "'from' expressions are not supported in minimaxon")
				}
			case "fallthrough":
				if i > 0 && toks[i-1].text == "and" {
					warn(line, "'and fallthrough' in match arms is not supported in minimaxon")
				}
			case "throws":
				warn(line, "'throws' in match arms or otherwise clauses is not supported in minimaxon")
			case "otherwise":
				// Check for block-form `otherwise 'label'` on a `try <expr>` expression (not a `try 'label'` block).
				if toks[0].text != "end" && toks[len(toks)-1].kind == tokLabel {
					warn(line, "block-form 'otherwise' on a try expression is not supported in minimaxon; use a try block")
				}
			case "function":
				if i+1 < len(toks) && toks[i+1].text == "(" {
					warn(line, "closures are not supported in minimaxon")
				}
			case "reply":
				if i+3 < len(toks) && toks[i+1].text == "." && toks[i+2].text == "later" && toks[i+3].text == "(" {
					warn(line, "reply.later() is not supported in minimaxon")
				}
			case "is":
				if i == 0 || toks[i-1].text != "." {
					warn(line, "'is' / 'is not' comparison is not supported in minimaxon")
				}
			case "isolated", "immutable":
				if i > 0 && toks[i-1].text == "as" {
					warn(line, fmt.Sprintf("region capability '%s' is not supported in minimaxon", t.text))
				}
			case "min":
				if i+1 < len(toks) && (toks[i+1].text == "to" || toks[i+1].text == "upto") && (i == 0 || toks[i-1].text != ".") {
					warn(line, "open-ended 'min to/upto' match range patterns are not supported in minimaxon")
				}
			case "max":
				if i > 0 && toks[i-1].text == "to" {
					warn(line, "open-ended 'to max' match range patterns are not supported in minimaxon")
				}
			case "float":
				if i > 0 && toks[i-1].text == "as" {
					warn(line, "float types are not supported in minimaxon")
				}
			}
		}
	}
}

// rewriteRegionExprs rewrites old Maxon `freeze expr` -> `freeze(expr)` and `swap target with expr` -> `exchange(target, value: expr)`.
func rewriteRegionExprs(toks []token, line *sourceLine, warn func(*sourceLine, string)) []token {
	var out []token
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		isMember := i > 0 && toks[i-1].text == "."
		if t.kind == tokIdent && !isMember {
			switch t.text {
			case "freeze":
				if i+1 < len(toks) && isRegionOperandStart(toks[i+1]) {
					end := parseUnaryEnd(toks, i+1)
					out = append(out, t, token{kind: tokPunct, text: "("})
					out = append(out, toks[i+1:end]...)
					out = append(out, token{kind: tokPunct, text: ")"})
					i = end - 1
					continue
				}
			case "swap":
				if i+1 < len(toks) && isRegionOperandStart(toks[i+1]) {
					targetEnd := parsePostfixEnd(toks, i+1)
					if targetEnd < len(toks) && toks[targetEnd].text == "with" && targetEnd+1 < len(toks) {
						valEnd := parseExprEnd(toks, targetEnd+1)
						out = append(out, token{kind: tokIdent, text: "exchange"}, token{kind: tokPunct, text: "("})
						out = append(out, toks[i+1:targetEnd]...)
						out = append(out, token{kind: tokPunct, text: ","}, token{kind: tokIdent, text: "value"}, token{kind: tokPunct, text: ":"})
						out = append(out, toks[targetEnd+1:valEnd]...)
						out = append(out, token{kind: tokPunct, text: ")"})
						i = valEnd - 1
						continue
					}
				}
			case "merge":
				if i+1 < len(toks) && isRegionOperandStart(toks[i+1]) {
					warn(line, "'merge' region expressions are not supported in minimaxon")
				}
			}
		}
		out = append(out, t)
	}
	return out
}

func isRegionOperandStart(t token) bool {
	return t.kind == tokIdent && t.text != "with" && t.text != "then" && t.text != "gives"
}

func parseUnaryEnd(toks []token, from int) int {
	i := from
	for i < len(toks) && (toks[i].text == "-" || toks[i].text == "not") {
		i++
	}
	return parsePostfixEnd(toks, i)
}

func parsePostfixEnd(toks []token, from int) int {
	if from >= len(toks) {
		return from
	}
	i := from
	if toks[i].text == "(" {
		closeIdx := findMatchingParen(toks, i)
		if closeIdx < 0 {
			return len(toks)
		}
		i = closeIdx + 1
	} else {
		i++
	}
	for i < len(toks) {
		switch toks[i].text {
		case ".":
			if i+1 < len(toks) {
				i += 2
				continue
			}
			return i
		case "(":
			closeIdx := findMatchingParen(toks, i)
			if closeIdx < 0 {
				return len(toks)
			}
			i = closeIdx + 1
		case "[":
			depth := 0
			for i < len(toks) {
				if toks[i].text == "[" {
					depth++
				} else if toks[i].text == "]" {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
		case "{":
			depth := 0
			for i < len(toks) {
				if toks[i].text == "{" {
					depth++
				} else if toks[i].text == "}" {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
		default:
			return i
		}
	}
	return i
}

func parseExprEnd(toks []token, from int) int {
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

func isCapabilityWord(word string) bool {
	return word == "isolated" || word == "immutable"
}

func isNewReservedWord(word string) bool {
	switch word {
	case "mutates", "consumes", "sets", "destructs", "noncopyable", "when":
		return true
	}
	return false
}

func hasToken(toks []token, text string) bool {
	for _, t := range toks {
		if t.text == text {
			return true
		}
	}
	return false
}

func hasTokenBeforeLabel(toks []token, text string) bool {
	for _, t := range toks {
		if t.kind == tokLabel {
			return false
		}
		if t.text == text {
			return true
		}
	}
	return false
}

func hasRangeKeyword(toks []token) bool {
	depth := 0
	for _, t := range toks {
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case "to", "upto":
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func hasTopLevelAssign(toks []token) bool {
	depth := 0
	for _, t := range toks {
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case "=":
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// hasFormatSpecifier reports whether a string literal contains an interpolation with a top-level ':spec'.
func hasFormatSpecifier(s string) bool {
	i, n := 1, len(s)-1
	for i < n {
		switch s[i] {
		case '\\':
			i += 2
		case '{':
			end := scanInterp(s, i)
			if interpHasColon(s[i+1 : end-1]) {
				return true
			}
			i = end
		default:
			i++
		}
	}
	return false
}

func interpHasColon(expr string) bool {
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '"':
			i = scanString(expr, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func isCharLiteralPosition(toks []token, idx int) bool {
	// Labels appear at the end of block headers (`if ... 'lbl'`, `while ... 'lbl'`, `for ... 'lbl'`, `match ... 'lbl'`,
	// `try 'lbl'`, `else 'lbl'`, `otherwise ... 'lbl'`, `when ... 'lbl'`, `modify ... 'lbl'`, `acquire ... 'lbl'`),
	// or right after `end`, `break`, `continue`, or `test`.
	if idx > 0 {
		switch toks[idx-1].text {
		case "end", "break", "continue", "else", "otherwise", "try", "test":
			return false
		}
	}
	if idx == len(toks)-1 {
		switch toks[0].text {
		case "if", "while", "for", "match", "try", "else", "otherwise", "when", "modify", "acquire", "end", "test":
			return false
		}
		// Also `let x = try ... otherwise 'lbl'` or `end 'a' else 'b'`.
		for _, t := range toks[:idx] {
			if t.text == "otherwise" || t.text == "else" || t.text == "match" {
				return false
			}
		}
	}
	return true
}

func isCollectionLiteralBracket(toks []token, idx int) bool {
	if idx == 0 {
		return true
	}
	prev := toks[idx-1]
	// Subscript follows an identifier, `self`, `)`, or `]`.
	if prev.kind == tokIdent && !isExprPrefixKeyword(prev.text) {
		return false
	}
	if prev.text == ")" || prev.text == "]" {
		return false
	}
	return true
}

func isExprPrefixKeyword(word string) bool {
	switch word {
	case "return", "let", "var", "if", "else", "while", "for", "in", "to", "upto",
		"and", "or", "xor", "not", "mod", "shl", "shr", "gives", "then",
		"try", "otherwise", "throw", "throws", "match", "as", "from":
		return true
	}
	return false
}

// renderFallbackLayout formats lines in minimaxon's layout (spacing, blank lines, comment indentation)
// so both clean files (before syntax.Format) and files with unsupported constructs get consistent layout.
func renderFallbackLayout(lines []sourceLine) string {
	// Expand inline TODOs into comment-only lines right before their code line.
	var expanded []sourceLine
	for _, l := range lines {
		if l.removed {
			continue
		}
		for _, todo := range l.todos {
			expanded = append(expanded, sourceLine{
				indent:  l.indent,
				comment: todoPrefix + todo,
			})
		}
		l.todos = nil
		expanded = append(expanded, l)
	}

	// Align comment-only lines to the indentation of the next code line.
	for i := len(expanded) - 1; i >= 0; i-- {
		if expanded[i].comment != "" && len(expanded[i].code) == 0 {
			if next := nextCodeLine(expanded, i+1); next >= 0 {
				expanded[i].indent = expanded[next].indent
			} else {
				expanded[i].indent = ""
			}
		}
	}

	// Track enclosing declaration kind to enforce blank-line rules.
	type container int
	const (
		topLevel container = iota
		inTypeLike
		inInterface
		inFunc
	)
	type frame struct {
		indent string
		kind   container
		seen   bool
	}
	var stack []frame

	current := func() *frame {
		if len(stack) == 0 {
			return nil
		}
		return &stack[len(stack)-1]
	}

	var out strings.Builder
	hasWritten := false
	pendingBlank := false

	writeBlank := func() {
		if hasWritten && !strings.HasSuffix(out.String(), "\n\n") {
			out.WriteByte('\n')
		}
	}

	for i := 0; i < len(expanded); i++ {
		l := expanded[i]
		if l.blank {
			pendingBlank = true
			continue
		}

		// Find the code line this comment group precedes (if comment-only).
		isCommentOnly := l.comment != "" && len(l.code) == 0
		nextCode := -1
		if isCommentOnly {
			nextCode = nextCodeLine(expanded, i+1)
		}

		// Determine if a blank line is required before this line (or comment group).
		if isCommentOnly {
			prevWasComment := i > 0 && expanded[i-1].comment != "" && len(expanded[i-1].code) == 0 && !pendingBlank
			if !prevWasComment {
				if nextCode < 0 {
					// Trailing comments at EOF get one blank line after the last declaration.
					writeBlank()
				} else if expanded[nextCode].indent == "" && len(stack) == 0 {
					// Comments before a top-level declaration get one blank line (except at file start).
					if hasWritten {
						writeBlank()
					}
				} else if f := current(); f != nil && f.kind == inTypeLike && f.seen && isMethodLine(expanded[nextCode]) {
					// Comments before a method inside a type/enum/union go after the blank line.
					writeBlank()
				} else if pendingBlank && i > 0 && expanded[i-1].comment != "" {
					// Keep paragraph break between comment blocks where blank lines are allowed.
					if len(stack) == 0 {
						writeBlank()
					}
				}
			}
			pendingBlank = false
			out.WriteString(l.indent)
			out.WriteString(l.comment)
			out.WriteByte('\n')
			hasWritten = true
			continue
		}

		// Code line.
		if l.code[0].text == "end" {
			for len(stack) > 0 && len(stack[len(stack)-1].indent) >= len(l.indent) {
				stack = stack[:len(stack)-1]
			}
		}

		prevWasComment := i > 0 && expanded[i-1].comment != "" && len(expanded[i-1].code) == 0
		if !prevWasComment {
			if l.indent == "" && len(stack) == 0 && l.code[0].text != "end" {
				if hasWritten {
					writeBlank()
				}
			} else if f := current(); f != nil && f.kind == inTypeLike && f.seen && isMethodLine(l) {
				writeBlank()
			}
		}
		pendingBlank = false

		if f := current(); f != nil && len(l.indent) > len(f.indent) {
			f.seen = true
		}

		out.WriteString(l.indent)
		out.WriteString(renderCodeTokens(l.code))
		if l.trailing != "" {
			out.WriteByte(' ')
			out.WriteString(l.trailing)
		}
		out.WriteByte('\n')
		hasWritten = true

		// Push new container if this line opens one.
		declIdx := skipModifiers(l.code)
		if declIdx < len(l.code) && l.code[declIdx].kind == tokIdent {
			switch l.code[declIdx].text {
			case "type", "enum", "union":
				stack = append(stack, frame{indent: l.indent, kind: inTypeLike})
			case "interface":
				stack = append(stack, frame{indent: l.indent, kind: inInterface})
			case "function":
				if !isClosureLine(l.code, declIdx) {
					if f := current(); f == nil || f.kind != inInterface {
						stack = append(stack, frame{indent: l.indent, kind: inFunc})
					}
				}
			}
		}
	}
	return out.String()
}

func isMethodLine(l sourceLine) bool {
	idx := skipModifiers(l.code)
	return idx < len(l.code) && l.code[idx].kind == tokIdent && l.code[idx].text == "function" && !isClosureLine(l.code, idx)
}

// renderCodeTokens formats a line's code tokens using minimaxon's exact token spacing rules.
func renderCodeTokens(toks []token) string {
	if len(toks) == 0 {
		return ""
	}
	var out strings.Builder
	for i, t := range toks {
		if i > 0 && needsSpace(toks, i-1, i) {
			out.WriteByte(' ')
		}
		out.WriteString(t.text)
	}
	return out.String()
}

func needsSpace(toks []token, prevIdx, curIdx int) bool {
	prev := toks[prevIdx]
	cur := toks[curIdx]

	// Never space after these.
	switch prev.text {
	case "(", "{", ".", "[":
		return false
	case "-":
		if isUnaryMinus(toks, prevIdx) {
			return false
		}
	}

	// Never space before these.
	switch cur.text {
	case ")", "}", ",", ":", ".", "]":
		return false
	case "{":
		return false
	case "[":
		return isCollectionLiteralBracket(toks, curIdx)
	case "(":
		// Join '(' to callee/declaration name, `int`, `panic`, `sizeof`, `countof`, `Self`, or `)` / `]`.
		if prev.text == ")" || prev.text == "]" {
			return false
		}
		if prev.kind == tokIdent && !spaceBeforeParenKeyword(prev.text) {
			return false
		}
		return true
	}

	return true
}

func isUnaryMinus(toks []token, idx int) bool {
	if idx == 0 {
		return true
	}
	prev := toks[idx-1]
	if prev.kind == tokPunct {
		switch prev.text {
		case "(", "[", "{", ",", ":", "=", "+", "-", "*", "/", "==", "!=", "<", "<=", ">", ">=":
			return true
		}
	}
	if prev.kind == tokIdent && isExprPrefixKeyword(prev.text) {
		return true
	}
	return false
}

func spaceBeforeParenKeyword(word string) bool {
	switch word {
	case "if", "else", "while", "for", "in", "to", "upto", "return", "try", "otherwise",
		"with", "and", "or", "xor", "not", "mod", "shl", "shr", "gives", "then",
		"throw", "throws", "match", "as", "let", "var", "mutates", "consumes", "sets",
		"returns", "where", "implements", "uses", "from", "spawn", "await", "async", "when":
		return true
	}
	return false
}
