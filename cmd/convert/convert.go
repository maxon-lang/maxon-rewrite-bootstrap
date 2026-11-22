package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// Result holds the converted source, any warnings emitted, and whether minimaxon's strict formatter accepted the output;
// when it did not, Rejected is the formatter's first error.
type Result struct {
	Text      string
	Warnings  []Warning
	Formatted bool
	Rejected  string
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

// inputKind is what a converted text is: a source file of maxon's library or tools, or the program of a spec test, which
// may declare a name that minimaxon has built in, as any program may.
type inputKind int

const (
	sourceFile inputKind = iota
	specProgram
)

// Convert converts existing Maxon source code to minimaxon syntax and layout.
func Convert(file, src string, kind inputKind) Result {
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

	if kind == sourceFile {
		if isLibraryHelper(file) {
			exportPublicModifiers(lines)
		} else if root := libraryRoot(file); root != "" {
			exportMembersNamingHelpers(lines, helperTypes(root))
			keepBuiltinConformances(lines, libraryInterfaces(root))
			dropLiteralMarkers(lines)
		}
		dropBuiltinDeclarations(lines)
		addInterfaceWheres(lines)
		lendRequirements(lines)
		dropReplacedMembers(lines)
		dropSelfClones(lines)
	}
	joinEndOtherwise(lines)
	rewriteDestructors(lines)
	rewriteWithIterator(lines, addWarn)
	transformLines(lines, addWarn)

	candidate := renderFallbackLayout(lines)
	laid := formatter.format(candidate)
	return Result{Text: laid.text, Warnings: warnings, Formatted: laid.formatted, Rejected: laid.rejected}
}

// normalizeText strips BOM, converts CRLF/CR to LF, converts leading space indentation to tabs, indents each line by
// its nesting depth, and trims trailing spaces.
func normalizeText(src string) string {
	src = strings.TrimPrefix(src, utf8BOM)
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")

	rawLines := strings.Split(src, "\n")
	spaceUnit := detectSpaceIndent(rawLines)

	lines := make([]string, len(rawLines))
	for i, line := range rawLines {
		if trimmedRight := strings.TrimRight(line, " \t"); trimmedRight != "" {
			lines[i] = convertLeadingSpaces(trimmedRight, spaceUnit)
		}
	}
	alignOrChains(lines)
	indentByDepth(lines)
	res := strings.Join(lines, "\n")
	if res != "" && !strings.HasSuffix(res, "\n") {
		res += "\n"
	}
	return res
}

// indentByDepth gives each code line one tab per enclosing indentation level, however many tabs maxon's author used
// for a level or for the whole block: maxon accepts any consistent indentation, minimaxon's formatter exactly one tab
// per level. Comment-only lines are left alone; they are aligned to the code that follows them later.
func indentByDepth(lines []string) {
	var open []int
	for i, line := range lines {
		tabs := len(line) - len(strings.TrimLeft(line, "\t"))
		if tabs == len(line) || strings.HasPrefix(line[tabs:], "//") {
			continue
		}
		for len(open) > 0 && open[len(open)-1] > tabs {
			open = open[:len(open)-1]
		}
		if len(open) == 0 || open[len(open)-1] < tabs {
			open = append(open, tabs)
		}
		lines[i] = strings.Repeat("\t", len(open)-1) + line[tabs:]
	}
}

// alignOrChains puts the alternatives of a match arm's `or` chain at the indentation of the first one: maxon reads any
// indentation after the `or` that ends a line, minimaxon's layout is the alternatives one under another.
func alignOrChains(lines []string) {
	chainIndent := ""
	chained := false
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, "	")
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		indent := line[:len(line)-len(trimmed)]
		if chained {
			lines[i] = chainIndent + trimmed
			indent = chainIndent
		}
		chained = endsWithOr(trimmed)
		chainIndent = indent
	}
}

// endsWithOr reports whether the last token of the line, ignoring a comment, is the word `or`.
func endsWithOr(line string) bool {
	last := token{kind: tokOther}
	for _, t := range lex(line) {
		if t.kind != tokSpace && t.kind != tokLineComment && t.kind != tokNewline {
			last = t
		}
	}
	return last.kind == tokIdent && last.text == "or"
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

// opensWithEnd reports whether the next token after from, past blanks and line breaks, is the keyword end.
func opensWithEnd(toks []token, from int) bool {
	for from < len(toks) && (toks[from].kind == tokSpace || toks[from].kind == tokNewline) {
		from++
	}
	return from < len(toks) && toks[from].kind == tokIdent && toks[from].text == "end"
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
				if opensWithEnd(toks, i+1) {
					// A line that ends a block cannot continue an expression, so the bracket was never closed.
					bracketDepth = 0
					break
				}
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

// blockDeclWords are the words of a top-level declaration that runs to its own `end`, and lineDeclWords those of one
// that is a line.
var (
	blockDeclWords = []string{"type", "union", "enum", "interface", "function"}
	lineDeclWords  = []string{"typealias", "let", "var"}
)

// dropBuiltinDeclarations leaves out each top-level declaration of a name that minimaxon has built in, with the comment
// above it: maxon's library declares what the compiler declares here, and a second declaration would be another
// declaration of the name, which the code written against the built-in never meets.
func dropBuiltinDeclarations(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].kind != tokIdent || !builtinNames[l.code[declIdx+1].text] {
			continue
		}
		last := i
		switch word := l.code[declIdx].text; {
		case slices.Contains(blockDeclWords, word):
			last = topLevelEnd(lines, i)
		case slices.Contains(lineDeclWords, word):
		default:
			continue
		}
		for k := i - 1; k >= 0 && len(lines[k].code) == 0 && lines[k].comment != ""; k-- {
			lines[k].removed = true
		}
		for k := i; k <= last; k++ {
			lines[k].removed = true
		}
		i = last
	}
}

// topLevelEnd is the line of the `end` that closes the top-level declaration that line start opens.
func topLevelEnd(lines []sourceLine, start int) int {
	for k := start + 1; k < len(lines); k++ {
		if len(lines[k].code) > 0 && lines[k].indent == "" && lines[k].code[0].text == "end" {
			return k
		}
	}
	panic(fmt.Sprintf("line %d: the declaration has no end", lines[start].origLine))
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
		blockExtension
	)
	type openMatch struct {
		indent  string
		subject string
	}
	type openBlock struct {
		indent string
		kind   blockKind
		// generic is whether the block is a type with type parameters, and params are their names.
		generic bool
		params  []string
	}
	var stack []openBlock
	var matches []openMatch
	handlerNames := map[string]bool{}
	arities := interfaceArities(lines)

	currentBlock := func() blockKind {
		if len(stack) == 0 {
			return blockOther
		}
		return stack[len(stack)-1].kind
	}
	inGenericType := func() bool {
		return len(stack) > 0 && (stack[len(stack)-1].kind == blockType || stack[len(stack)-1].kind == blockExtension || stack[len(stack)-1].kind == blockInterface) && stack[len(stack)-1].generic
	}
	generics := genericTypes(lines)
	typeParamsInScope := func(header []token) []string {
		var names []string
		for _, b := range stack {
			names = append(names, b.params...)
		}
		return append(names, usesNames(header)...)
	}

	for i := range lines {
		line := &lines[i]
		if line.removed || len(line.code) == 0 {
			continue
		}
		line.code = stripTypeCapabilities(line.code)

		// Pop blocks closed by `end` at matching indentation.
		if line.code[0].text == "end" && !namesACase(line.code, currentBlock() == blockEnum) {
			for len(stack) > 0 && len(stack[len(stack)-1].indent) >= len(line.indent) {
				stack = stack[:len(stack)-1]
			}
			for len(matches) > 0 && len(matches[len(matches)-1].indent) >= len(line.indent) {
				matches = matches[:len(matches)-1]
			}
		}
		noteHandlerNames(line.code, handlerNames)
		if subject := matchSubject(line.code); subject != "" {
			matches = append(matches, openMatch{indent: line.indent, subject: subject})
		}
		if len(matches) > 0 && handlerNames[matches[len(matches)-1].subject] && isQualifiedArm(line.code) {
			warn(line, "a handler's match on the combined error of several errors, by qualified case names, is not supported in minimaxon")
		}
		if opensBlockInBrackets(line.code) {
			warn(line, "a block inside brackets is not supported in minimaxon: a line break inside parentheses is a syntax error; bind the block's value on a line of its own")
		}
		if namesACase(line.code, currentBlock() == blockEnum) {
			warn(line, keywordNameMessage(line.code[0].text))
		}

		first := line.code[0]
		switch first.kind {
		case tokShebang:
			warn(line, "shebang lines are not supported in minimaxon")
			continue
		case tokHashDirective:
			// minimaxon reads the directives as maxon does; the code of a branch converts as the code around it.
			continue
		case tokAnnot:
			if first.text != "@heap" {
				warn(line, fmt.Sprintf("%s annotations are not supported in minimaxon", first.text))
			}
		}

		// Determine declaration keyword (skipping optional visibility / static / @annot).
		declIdx := skipModifiers(line.code)
		declWord := ""
		if declIdx < len(line.code) && line.code[declIdx].kind == tokIdent {
			declWord = line.code[declIdx].text
		}

		switch declWord {
		case "type", "union", "enum", "extension", "function":
			line.code = parenthesizeAssociatedTypes(line.code, arities)
		}
		switch declWord {
		case "type":
			transformTypeOrUnionHeader(line, declIdx, warn)
			stack = append(stack, openBlock{indent: line.indent, kind: blockType, generic: hasUsesClause(line.code[declIdx:]), params: usesNames(line.code[declIdx:])})
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
			stack = append(stack, openBlock{indent: line.indent, kind: blockInterface, generic: hasUsesClause(line.code[declIdx:]), params: usesNames(line.code[declIdx:])})
			continue
		case "function":
			if !isClosureLine(line.code, declIdx) {
				transformFunctionHeader(line, declIdx, typeParamsInScope(line.code[declIdx:]), warn)
				if currentBlock() != blockInterface {
					stack = append(stack, openBlock{indent: line.indent, kind: blockOther})
				}
				continue
			}
		case "typealias":
			switch currentBlock() {
			case blockEnum, blockUnion:
				warn(line, "typealias declarations in an enum or union are not supported in minimaxon")
			case blockType, blockExtension, blockInterface:
				if inGenericType() && isRangeTypealias(line.code, declIdx) {
					warn(line, "ranged typealiases in a generic type are not supported in minimaxon: each instance has a type of its own")
				}
			case blockOther:
			}
			transformTypealias(line, declIdx, warn)
			continue
		case "extension":
			transformWhereInSlice(line.code, declIdx+1, line, warn)
			warnUnsupportedExtension(line, declIdx, warn)
			stack = append(stack, openBlock{indent: line.indent, kind: blockExtension, generic: extendsGeneric(line.code, declIdx, generics)})
			continue
		case "test":
			if len(line.code) >= 2 && line.code[1].kind == tokLabel {
				stack = append(stack, openBlock{indent: line.indent, kind: blockOther})
				continue
			}
		case "default":
			if line.indent == "" && len(line.code) >= 3 && line.code[2].text == "=" {
				warn(line, "top-level default declarations are not supported in minimaxon")
				continue
			}
		case "let", "var":
			if hasStaticModifier(line.code, declIdx) && (line.code[declIdx].text == "var" || currentBlock() != blockType) {
				warn(line, "static fields are supported in minimaxon only as 'static let' in a type body")
			}
		}

		// Inside a union body: check the payload of each case.
		if currentBlock() == blockUnion {
			checkUnionCase(line, warn)
		}

		// Statement and expression transformations.
		transformStmtAndExpr(line, warn)
	}
}

// armEnders are the words that follow an arm's patterns.
var armEnders = []string{"then", "gives", "or", "panic", "throws"}

// namesACase reports whether a line starting with `end` names a case of that word instead of closing a block: an enum's
// case alone on its line, a union case or a match arm's pattern, which no block's end has after it but a label.
func namesACase(toks []token, inEnum bool) bool {
	if toks[0].text != "end" || toks[0].kind != tokIdent {
		return false
	}
	if len(toks) == 1 {
		return inEnum
	}
	next := toks[1]
	return next.text == "(" || (next.kind == tokIdent && slices.Contains(armEnders, next.text))
}

// noteHandlerNames records the names that `otherwise (e)` and `else (e)` bind to an error.
func noteHandlerNames(toks []token, names map[string]bool) {
	for i := 0; i+3 < len(toks); i++ {
		if (toks[i].text == "otherwise" || toks[i].text == "else") && toks[i+1].text == "(" && toks[i+2].kind == tokIdent && toks[i+3].text == ")" {
			names[toks[i+2].text] = true
		}
	}
}

// matchSubject is the name a line `match name 'label'` matches on, or "" when the line opens no match on a name.
func matchSubject(toks []token) string {
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].text == "match" && toks[i].kind == tokIdent {
			if toks[i+1].kind == tokIdent && toks[i+2].kind == tokLabel {
				return toks[i+1].text
			}
			return ""
		}
	}
	return ""
}

// isQualifiedArm reports whether a line starts a match arm whose first pattern is `Type.case`, with the bindings of a
// union's payload or without.
func isQualifiedArm(toks []token) bool {
	if len(toks) < 4 || toks[0].kind != tokIdent || toks[1].text != "." || toks[2].kind != tokIdent {
		return false
	}
	next := 3
	if toks[next].text == "(" {
		closing := findMatchingParen(toks, next)
		if closing < 0 {
			return false
		}
		next = closing + 1
	}
	return next < len(toks) && toks[next].kind == tokIdent && slices.Contains(armEnders, toks[next].text)
}

// opensBlockInBrackets reports whether a line ends in a label with a bracket still open, which only a block that
// joinBracketLines kept on several lines does.
func opensBlockInBrackets(toks []token) bool {
	depth := 0
	for _, t := range toks {
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
	}
	return depth > 0 && toks[len(toks)-1].kind == tokLabel
}

// unsupportedExtensionTargets are the names an extension cannot extend in minimaxon although it may name them: library
// types and interfaces that it has no counterpart of, or that it does not extend.
var unsupportedExtensionTargets = []string{"Set", "Log", "Logger", "CivilDate", "ParseError", "byte", "Character", "Error"}

// builtinGenericTypes are the library types that an extension may name and that take type arguments.
var builtinGenericTypes = []string{"Array", "Map", "List", "Pool", "Handle", "Frozen", "Cown", "FixedArray"}

// genericTypes are the types and unions the lines declare with type parameters, and the generic library types.
func genericTypes(lines []sourceLine) []string {
	names := slices.Clone(builtinGenericTypes)
	for i := range lines {
		line := &lines[i]
		if line.removed || len(line.code) == 0 {
			continue
		}
		idx := skipModifiers(line.code)
		if idx+1 < len(line.code) && line.code[idx].kind == tokIdent && (line.code[idx].text == "type" || line.code[idx].text == "union") && hasUsesClause(line.code[idx:]) {
			names = append(names, line.code[idx+1].text)
		}
	}
	return names
}

// extendsGeneric is whether the extension at declIdx extends one of the generic types.
func extendsGeneric(toks []token, declIdx int, generics []string) bool {
	return declIdx+1 < len(toks) && slices.Contains(generics, toks[declIdx+1].text)
}

// interfaceArities are the number of associated types of each generic interface the lines declare.
func interfaceArities(lines []sourceLine) map[string]int {
	arities := map[string]int{}
	for i := range lines {
		line := &lines[i]
		if line.removed || len(line.code) == 0 {
			continue
		}
		idx := skipModifiers(line.code)
		if idx+1 >= len(line.code) || line.code[idx].kind != tokIdent || line.code[idx].text != "interface" {
			continue
		}
		usesIdx := findToken(line.code, idx+2, "uses")
		if usesIdx < 0 {
			continue
		}
		count := 0
		for _, t := range line.code[usesIdx+1:] {
			if t.kind == tokIdent && t.text == "extends" {
				break
			}
			if t.kind == tokIdent {
				count++
			}
		}
		if count > 0 {
			arities[line.code[idx+1].text] = count
		}
	}
	return arities
}

// parenthesizeAssociatedTypes writes the associated types that an 'implements' or a bound binds of an interface with several
// in parentheses, where maxon also takes them as a bare list as long as the interface's arity, and parenthesizes a tuple that
// is the one associated type of an interface, which maxon writes as a parenthesized list: minimaxon's type arguments are one
// type or a parenthesized list.
func parenthesizeAssociatedTypes(toks []token, arities map[string]int) []token {
	var out []token
	for i := 0; i < len(toks); i++ {
		out = append(out, toks[i])
		if toks[i].text != "with" || i == 0 || i+1 >= len(toks) {
			continue
		}
		arity, ok := arities[toks[i-1].text]
		if !ok {
			continue
		}
		if toks[i+1].text == "(" {
			closing := findMatchingParen(toks, i+1)
			if arity == 1 && closing > 0 && len(splitTopLevel(toks[i+2:closing])) > 1 {
				out = append(out, token{kind: tokPunct, text: "("})
				out = append(out, toks[i+1:closing+1]...)
				out = append(out, token{kind: tokPunct, text: ")"})
				i = closing
			}
			continue
		}
		if arity == 1 {
			continue
		}
		end, items := i+1, 0
		for depth := 0; end < len(toks); end++ {
			t := toks[end]
			if depth == 0 && t.kind == tokIdent && (t.text == "where" || t.text == "and" || t.text == "returns" || t.text == "throws") {
				break
			}
			if t.text == "(" {
				depth++
			} else if t.text == ")" {
				if depth == 0 {
					break
				}
				depth--
			} else if t.text == "," && depth == 0 {
				items++
				if items == arity {
					break
				}
			}
		}
		if items+1 < arity {
			continue
		}
		out = append(out, token{kind: tokPunct, text: "("})
		out = append(out, toks[i+1:end]...)
		out = append(out, token{kind: tokPunct, text: ")"})
		i = end - 1
	}
	return out
}

// warnUnsupportedExtension warns about the parts of an extension header that minimaxon does not have: an extension of a
// library type it has no counterpart of, and a conformance with type arguments. A 'with' after the 'where' of a
// conditional extension binds a bound's associated types, not the header's.
func warnUnsupportedExtension(line *sourceLine, declIdx int, warn func(*sourceLine, string)) {
	toks := line.code
	if declIdx+1 >= len(toks) {
		return
	}
	target := toks[declIdx+1].text
	if slices.Contains(unsupportedExtensionTargets, target) {
		warn(line, fmt.Sprintf("extensions of '%s' are not supported in minimaxon", target))
	}
	for _, tok := range toks[declIdx+2:] {
		switch tok.text {
		case "where":
			return
		case "with":
			warn(line, "extension headers with type arguments are not supported in minimaxon")
			return
		}
	}
}

// libraryRootDir and helpersDir place a file of maxon's library helpers: maxon's programs see every name of the library
// bare, but here a program sees only what the library declares public, and a helper is for the library's own files.
const (
	libraryRootDir = "stdlib"
	helpersDir     = "helpers"
)

// isLibraryHelper reports whether file lies in maxon's stdlib/helpers/.
func isLibraryHelper(file string) bool {
	parts := strings.Split(filepath.ToSlash(file), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == libraryRootDir && parts[i+1] == helpersDir {
			return true
		}
	}
	return false
}

// exportPublicModifiers makes each public of a helper export: a helper module is a direct child of the library's root,
// so export reaches the whole library and no program.
func exportPublicModifiers(lines []sourceLine) {
	for i := range lines {
		makeExport(&lines[i])
	}
}

// makeExport writes export for the public of a declaration's line, and says whether it had one.
func makeExport(l *sourceLine) bool {
	for k := range skipModifiers(l.code) {
		if l.code[k].kind == tokIdent && l.code[k].text == "public" {
			l.code[k].text = "export"
			return true
		}
	}
	return false
}

// libraryRoot is the directory of maxon's library that file lies in, or "" for a file outside it.
func libraryRoot(file string) string {
	parts := strings.Split(filepath.ToSlash(file), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] == libraryRootDir {
			return filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		}
	}
	return ""
}

// helperDeclaration is a public declaration of a type or a typealias at the top of a helper's file.
var helperDeclaration = regexp.MustCompile(`^public\s+(?:type|enum|union|interface|typealias)\s+(\w+)`)

// helperTypesByRoot holds, for each library root read so far, the names of the types its helpers declare.
var helperTypesByRoot = map[string]map[string]bool{}

// helperTypes are the names of the types and typealiases that the helpers of the library at root declare public, which
// are export here (exportPublicModifiers): visible to the whole library and to no program.
func helperTypes(root string) map[string]bool {
	if names, ok := helperTypesByRoot[root]; ok {
		return names
	}
	names := map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(root, helpersDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".maxon" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if m := helperDeclaration.FindStringSubmatch(line); m != nil {
				names[m[1]] = true
			}
		}
		return nil
	})
	helperTypesByRoot[root] = names
	return names
}

// interfaceDeclaration and requirementDeclaration are a public interface of the library's root and one of its requirements.
var (
	interfaceDeclaration   = regexp.MustCompile(`^public\s+interface\s+(\w+)`)
	requirementDeclaration = regexp.MustCompile(`^\s+(static\s+)?function\s+(\w+)\s*\(`)
)

// libraryInterfacesByRoot holds, for each library root read so far, the requirements of the interfaces it declares.
var libraryInterfacesByRoot = map[string]map[string][]string{}

// libraryInterfaces are the public interfaces that the files at the root of the library at root declare, each with the
// names of its requirements, a static one's after "static ".
func libraryInterfaces(root string) map[string][]string {
	if found, ok := libraryInterfacesByRoot[root]; ok {
		return found
	}
	found := map[string][]string{}
	files, _ := filepath.Glob(filepath.Join(root, "*.maxon"))
	slices.Sort(files)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		current := ""
		for line := range strings.SplitSeq(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			if m := interfaceDeclaration.FindStringSubmatch(line); m != nil {
				current = m[1]
				found[current] = nil
				continue
			}
			if current == "" {
				continue
			}
			if strings.HasPrefix(line, "end ") {
				current = ""
				continue
			}
			if m := requirementDeclaration.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "\t\t") {
				found[current] = append(found[current], strings.TrimSpace(m[1]+m[2]))
			}
		}
	}
	libraryInterfacesByRoot[root] = found
	return found
}

// cloneRequirement is the requirement that the copy every Copyable value has meets here (x.clone()): no type declares a
// method of that name, so a conformance that requires it is not one a built-in type can declare.
const cloneRequirement = "clone"

// staticRequirement starts the name of a requirement that is a static function (libraryInterfaces).
const staticRequirement = "static "

// keepBuiltinConformances converts a file of the library that declares a type minimaxon builds in, whose methods the
// compiler declares (String), into that type's conformances alone: an extension of the type that implements the
// interfaces of its header that the library declares and that take no type arguments, with each method they require
// that the built-in type lacks, as the file writes it, and the file's declarations that those methods name. The rest of
// the file is the built-in's own implementation, which minimaxon has natively. A conformance whose requirements neither
// the built-in nor the file meets, that requires clone, or that requires a static function, which makes a value of the
// type where the compiler makes the built-in's, is left out.
func keepBuiltinConformances(lines []sourceLine, interfaces map[string][]string) {
	header := -1
	name := ""
	for i := range lines {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "type" {
			continue
		}
		candidate := l.code[declIdx+1].text
		if _, ok := builtinMethods[candidate]; ok && !builtinNames[candidate] {
			header, name = i, candidate
			break
		}
	}
	if header < 0 {
		return
	}
	last := topLevelEnd(lines, header)
	declared := memberMethods(lines, header, last)
	var kept []string
	var needed []string
	for _, item := range implementedInterfaces(lines[header].code) {
		reqs, ok := interfaces[item.name]
		if !ok || item.hasArgs || slices.Contains(reqs, cloneRequirement) || slices.ContainsFunc(reqs, func(r string) bool { return strings.HasPrefix(r, staticRequirement) }) {
			continue
		}
		var mine []string
		met := true
		for _, req := range reqs {
			if slices.Contains(builtinMethods[name], req) {
				continue
			}
			if _, ok := declared[req]; !ok {
				met = false
				break
			}
			mine = append(mine, req)
		}
		if !met {
			continue
		}
		kept = append(kept, item.name)
		for _, m := range mine {
			if !slices.Contains(needed, m) {
				needed = append(needed, m)
			}
		}
	}
	keep := make([]bool, len(lines))
	keep[header], keep[last] = true, true
	for _, m := range needed {
		span := declared[m]
		for k := span.first; k <= span.last; k++ {
			keep[k] = true
		}
	}
	code := []token{{kind: tokIdent, text: "extension"}, {kind: tokIdent, text: name}}
	for i, iface := range kept {
		if i == 0 {
			code = append(code, token{kind: tokIdent, text: "implements"})
		} else {
			code = append(code, token{kind: tokPunct, text: ","})
		}
		code = append(code, token{kind: tokIdent, text: iface})
	}
	lines[header].code = code
	keepNamedDeclarations(lines, keep, header, last)
	for k := range lines {
		if !keep[k] {
			lines[k].removed = true
		}
	}
}

// lineSpan is the lines of a declaration, the comments above it first.
type lineSpan struct{ first, last int }

// memberMethods are the methods that the body of the type declared from header to last declares, by name.
func memberMethods(lines []sourceLine, header, last int) map[string]lineSpan {
	methods := map[string]lineSpan{}
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "\t" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" {
			continue
		}
		end := blockEnd(lines, k, l.indent)
		methods[l.code[declIdx+1].text] = lineSpan{first: commentsAbove(lines, k), last: end}
		k = end
	}
	return methods
}

// blockEnd is the line of the `end` at the indentation indent that closes the block that line start opens.
func blockEnd(lines []sourceLine, start int, indent string) int {
	for k := start + 1; k < len(lines); k++ {
		if len(lines[k].code) > 0 && lines[k].indent == indent && lines[k].code[0].text == "end" {
			return k
		}
	}
	return len(lines) - 1
}

// commentsAbove is the first of the comment lines right above the line at, or at itself where there are none.
func commentsAbove(lines []sourceLine, at int) int {
	first := at
	for first > 0 && len(lines[first-1].code) == 0 && lines[first-1].comment != "" {
		first--
	}
	return first
}

// keepNamedDeclarations keeps, besides what keep holds, each top-level declaration outside the type from header to last
// that a kept line names, and those that they name in turn.
func keepNamedDeclarations(lines []sourceLine, keep []bool, header, last int) {
	type topLevel struct {
		name string
		span lineSpan
	}
	var decls []topLevel
	for k := 0; k < len(lines); k++ {
		l := &lines[k]
		if k >= header && k <= last || l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) {
			continue
		}
		end := k
		if slices.Contains(blockDeclWords, l.code[declIdx].text) {
			end = topLevelEnd(lines, k)
		}
		decls = append(decls, topLevel{name: l.code[declIdx+1].text, span: lineSpan{first: commentsAbove(lines, k), last: end}})
		k = end
	}
	for changed := true; changed; {
		changed = false
		for _, d := range decls {
			if keep[d.span.last] || !namedByKept(lines, keep, d.name) {
				continue
			}
			for k := d.span.first; k <= d.span.last; k++ {
				keep[k] = true
			}
			changed = true
		}
	}
}

// namedByKept is whether a kept line names name.
func namedByKept(lines []sourceLine, keep []bool, name string) bool {
	for k := range lines {
		if keep[k] && slices.ContainsFunc(lines[k].code, func(t token) bool { return t.kind == tokIdent && t.text == name }) {
			return true
		}
	}
	return false
}

// literalMarker names the marker interfaces by which maxon's compiler finds the type a literal makes (BuiltinStringLiteral,
// BuiltinDictionaryLiteral and the others of maxon's Builtins.maxon): minimaxon's literals make its built-in types, and a
// map literal the library's Map, so a library type does not implement one.
var literalMarker = regexp.MustCompile(`^Builtin\w*Literal$`)

// dropLiteralMarkers leaves the literal markers out of the implements clause of each type the lines declare.
func dropLiteralMarkers(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		declIdx := skipModifiers(l.code)
		if l.removed || declIdx >= len(l.code) || l.code[declIdx].text != "type" {
			continue
		}
		at := findToken(l.code, declIdx, "implements")
		if at < 0 {
			continue
		}
		end := len(l.code)
		var items [][]token
		depth, from := 0, at+1
		for k := at + 1; k < len(l.code); k++ {
			t := l.code[k]
			if depth == 0 && t.kind == tokIdent && (t.text == "where" || t.text == "uses") {
				end = k
				break
			}
			switch {
			case t.text == "(":
				depth++
			case t.text == ")":
				depth--
			case depth == 0 && t.text == ",":
				items = append(items, l.code[from:k])
				from = k + 1
			}
		}
		items = append(items, l.code[from:end])
		var kept [][]token
		for _, item := range items {
			if len(item) > 0 && !literalMarker.MatchString(item[0].text) {
				kept = append(kept, item)
			}
		}
		if len(kept) == len(items) {
			continue
		}
		out := slices.Clone(l.code[:at])
		for j, item := range kept {
			if j == 0 {
				out = append(out, l.code[at])
			} else {
				out = append(out, token{kind: tokPunct, text: ","})
			}
			out = append(out, item...)
		}
		l.code = append(out, l.code[end:]...)
	}
}

// implementedInterface is an interface that a type's header implements, and whether it gives it type arguments.
type implementedInterface struct {
	name    string
	hasArgs bool
}

// implementedInterfaces are the interfaces of the implements clause of a type's header.
func implementedInterfaces(code []token) []implementedInterface {
	at := findToken(code, 0, "implements")
	if at < 0 {
		return nil
	}
	var items []implementedInterface
	depth := 0
	start := true
	for i := at + 1; i < len(code); i++ {
		t := code[i]
		switch {
		case t.text == "(":
			depth++
		case t.text == ")":
			depth--
		case depth == 0 && t.text == ",":
			start = true
			continue
		case depth == 0 && t.kind == tokIdent && (t.text == "where" || t.text == "uses"):
			return items
		}
		if start && t.kind == tokIdent {
			items = append(items, implementedInterface{name: t.text})
			start = false
			continue
		}
		if t.kind == tokIdent && t.text == "with" && len(items) > 0 {
			items[len(items)-1].hasArgs = true
		}
	}
	return items
}

// exportMembersNamingHelpers makes export each public declaration of a file of the library whose parameters, or whose
// target if it is a typealias, name a type that is less visible than public: a helper's, or a typealias that this rule
// made export. A signature may not name a less visible type there (E3167), while its result may (README, "Modules").
// A typealias declared in a type is named bare only in that type, so it counts there alone.
func exportMembersNamingHelpers(lines []sourceLine, helpers map[string]bool) {
	topLevel := map[string]bool{}
	for name := range helpers {
		topLevel[name] = true
	}
	for {
		changed := false
		var members map[string]bool
		for i := range lines {
			l := &lines[i]
			if l.removed || len(l.code) == 0 {
				continue
			}
			if l.indent == "" {
				members = map[string]bool{}
			}
			hidden := func(name string) bool { return topLevel[name] || members[name] }
			declIdx := skipModifiers(l.code)
			if declIdx >= len(l.code) || !slices.ContainsFunc(l.code[:declIdx], func(t token) bool { return t.text == "public" }) {
				continue
			}
			var named []token
			switch l.code[declIdx].text {
			case "typealias":
				if eq := findToken(l.code, declIdx+1, "="); eq >= 0 {
					named = l.code[eq+1:]
				}
			case "function":
				if lp := findToken(l.code, declIdx+1, "("); lp >= 0 {
					if rp := findMatchingParen(l.code, lp); rp >= 0 {
						named = l.code[lp+1 : rp]
					}
				}
			}
			if !slices.ContainsFunc(named, func(t token) bool { return t.kind == tokIdent && hidden(t.text) }) || !makeExport(l) {
				continue
			}
			changed = true
			if l.code[declIdx].text == "typealias" && declIdx+1 < len(l.code) {
				if l.indent == "" {
					topLevel[l.code[declIdx+1].text] = true
				} else {
					members[l.code[declIdx+1].text] = true
				}
			}
		}
		if !changed {
			return
		}
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

// hasUsesClause reports whether a type declaration's header declares type parameters.
func hasUsesClause(header []token) bool {
	return slices.ContainsFunc(header, func(t token) bool { return t.kind == tokIdent && t.text == "uses" })
}

// isRangeTypealias reports whether the typealias at kwIdx names an integer range, as against an instance of a generic
// type.
func isRangeTypealias(toks []token, kwIdx int) bool {
	eqIdx := findToken(toks, kwIdx+1, "=")
	return eqIdx >= 0 && eqIdx+1 < len(toks) && toks[eqIdx+1].text == "int"
}

func transformEnumHeader(line *sourceLine, kwIdx int, warn func(*sourceLine, string)) {
	transformWhereInSlice(line.code, kwIdx+2, line, warn)
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

func transformFunctionHeader(line *sourceLine, fnIdx int, typeParams []string, warn func(*sourceLine, string)) {
	toks := line.code
	if fnIdx+1 < len(toks) && isKeywordName(toks[fnIdx+1].text) {
		warn(line, keywordNameMessage(toks[fnIdx+1].text))
	}
	lp := findToken(toks, fnIdx+1, "(")
	if lp < 0 {
		return
	}
	rp := findMatchingParen(toks, lp)
	if rp < 0 {
		return
	}

	// Rewrite parameter list between lp+1 and rp.
	newParams := rewriteParamList(toks[lp+1:rp], typeParams, line, warn)
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
				line.code = slices.Concat(line.code[:i+1], capabilityType(line.code[i+1].text, line.code[i+2:], typeParams), line.code[i+2:])
			}
		}
	}
}

// capabilityType is what maxon's capability word before the type that typ starts becomes here, where what crosses
// between threads is decided by ownership (README, "Where it differs from Maxon"): `isolated T`, a value no one else
// reaches, is an owned T, and `immutable T`, frozen data that threads share, is a `frozen with T`. `immutable T` of a
// bare type parameter in scope is a plain T: maxon means a value the callee will not change and may share, which a
// parameter here is already, and a generic body cannot give a frozen value of a T it holds, so `frozen with T` would
// refuse every call from one. A service, which alone takes frozen values across threads, has no type parameters.
func capabilityType(word string, typ []token, typeParams []string) []token {
	if word == "immutable" && !namesBareTypeParam(typ, typeParams) {
		return []token{{kind: tokIdent, text: "frozen"}, {kind: tokIdent, text: "with"}}
	}
	return nil
}

// namesBareTypeParam is whether typ, the tokens from a type on, starts with a type parameter alone: a name of
// typeParams that no `with` or `.` follows.
func namesBareTypeParam(typ []token, typeParams []string) bool {
	if len(typ) == 0 || typ[0].kind != tokIdent || !slices.Contains(typeParams, typ[0].text) {
		return false
	}
	return len(typ) == 1 || (typ[1].text != "with" && typ[1].text != ".")
}

// usesNames are the type parameters that the `uses` clause of a declaration's header names.
func usesNames(header []token) []string {
	at := findToken(header, 0, "uses")
	if at < 0 {
		return nil
	}
	var names []string
	for i := at + 1; i < len(header) && header[i].kind == tokIdent && !isDeclClauseWord(header[i].text); i++ {
		names = append(names, header[i].text)
		if i+1 >= len(header) || header[i+1].text != "," {
			break
		}
		i++
	}
	return names
}

// isDeclClauseWord is whether word starts a clause of a declaration's header.
func isDeclClauseWord(word string) bool {
	return word == "implements" || word == "is" || word == "where" || word == "throws" || word == "returns"
}

// parameterModes are the words before a parameter's name that say how the function takes it.
var parameterModes = []string{"mutates", "consumes", "sets"}

func rewriteParamList(params []token, typeParams []string, line *sourceLine, warn func(*sourceLine, string)) []token {
	if len(params) == 0 {
		return params
	}
	var out []token
	for gIdx, g := range splitTopLevel(params) {
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

		// A caller-location default expands at the call site, which minimaxon has no form for.
		for _, t := range g {
			if t.text == "__line__" || t.text == "__file__" {
				warn(line, fmt.Sprintf("the caller-location default '%s' is not supported in minimaxon", t.text))
			}
		}

		if g[0].text == "_" && len(g) > 2 {
			for _, t := range g {
				if t.text == "=" {
					warn(line, "a defaulted discard parameter ('_') is not supported in minimaxon")
					break
				}
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

		nameIdx := 0
		if len(g) >= 3 && slices.Contains(parameterModes, g[0].text) {
			nameIdx = 1
		}
		// `name isolated T` takes a value that moves in, and `name immutable T` shares a frozen one.
		if typeAt := nameIdx + 1; typeAt+1 < len(g) && g[typeAt].kind == tokIdent && isCapabilityWord(g[typeAt].text) {
			word := g[typeAt].text
			g = slices.Concat(g[:typeAt:typeAt], capabilityType(word, g[typeAt+1:], typeParams), g[typeAt+1:])
			if word == "isolated" && nameIdx == 0 {
				g = slices.Concat([]token{{kind: tokIdent, text: "consumes"}}, g)
				nameIdx = 1
			}
		}
		for k := nameIdx + 1; k < len(g); k++ {
			if g[k].kind == tokIdent && isCapabilityWord(g[k].text) {
				warn(line, fmt.Sprintf("parameter capability '%s' is not supported in minimaxon", g[k].text))
			}
		}
		if nameIdx < len(g) && g[nameIdx].kind == tokIdent && isKeywordName(g[nameIdx].text) {
			warn(line, keywordNameMessage(g[nameIdx].text))
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

	if rhs[0].text == "function" {
		line.code = append(toks[:eqIdx+1:eqIdx+1], rewriteFunctionType(rhs, line, warn)...)
	}
}

// stripTypeCapabilities leaves out the capability word before a type wherever one is written in a nested position: a type
// argument (`Array with immutable Key`, `Map with (immutable K, V)`), a tuple's element (`(immutable Key, Value)`), a
// field's or a variable's type, the target of a cast or a typealias and a function type's parts. A value here has one
// owner, so one that nothing else changes is what every owned value already is (README, "Where it differs from Maxon").
// The word on a function's parameter or result is kept, for capabilityType to map, since what a parameter takes and a
// result gives decides what crosses between threads. A receiver's `immutable self` and a region bound of a `where` are no
// type, and are left for their warnings, and a capability word that names a function or a variable (`isolated(x)`,
// `not immutable`) is a name.
func stripTypeCapabilities(toks []token) []token {
	declIdx := skipModifiers(toks)
	typealias := declIdx < len(toks) && toks[declIdx].text == "typealias"
	kept := signatureCapabilities(toks)
	out := toks[:0:0]
	for i, t := range toks {
		if t.kind == tokIdent && isCapabilityWord(t.text) && !kept[i] && i+1 < len(toks) && len(out) > 0 && beforeType(out[len(out)-1], toks[i+1], typealias) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// signatureCapabilities marks the capability words of a named function's header that qualify a parameter's own type
// (`name isolated T`) or the type after `returns`: the positions transformFunctionHeader maps.
func signatureCapabilities(toks []token) []bool {
	kept := make([]bool, len(toks))
	fnIdx := funcKeywordIndex(toks)
	if fnIdx < 0 || isClosureLine(toks, fnIdx) {
		return kept
	}
	lp := findToken(toks, fnIdx+1, "(")
	if lp < 0 {
		return kept
	}
	rp := findMatchingParen(toks, lp)
	if rp < 0 {
		return kept
	}
	markCapability := func(at int) {
		if at+1 < len(toks) && toks[at].kind == tokIdent && isCapabilityWord(toks[at].text) {
			kept[at] = true
		}
	}
	groupStart := lp + 1
	depth := 0
	for i := lp + 1; i <= rp; i++ {
		switch toks[i].text {
		case "(":
			depth++
		case ")":
			depth--
		}
		if i != rp && (depth != 0 || toks[i].text != ",") {
			continue
		}
		typeAt := groupStart + 1
		if toks[groupStart].kind == tokIdent && slices.Contains(parameterModes, toks[groupStart].text) ||
			i-groupStart >= 3 && toks[groupStart+1].text == "mutable" {
			typeAt++
		}
		if typeAt < i {
			markCapability(typeAt)
		}
		groupStart = i + 1
	}
	for i := rp + 1; i+1 < len(toks); i++ {
		if toks[i].kind == tokIdent && toks[i].text == "returns" {
			markCapability(i + 1)
		}
	}
	return kept
}

// beforeType is whether a capability word between prev and next qualifies a type: after a word that a type follows, or
// between a parameter's name and its type, or first in a tuple's or a list's element, where a call's argument would be
// a name rather than a type's.
func beforeType(prev token, next token, typealias bool) bool {
	switch prev.text {
	case "with", "as", "returns":
		return startsType(next)
	case "=":
		return typealias && startsType(next)
	case "(", ",":
		return next.kind == tokIdent && startsType(next)
	}
	return prev.kind == tokIdent && (prev.text == "_" || !reservedWords[prev.text]) && next.kind == tokIdent && startsType(next)
}

// startsType is whether a token can begin a type: a tuple's parenthesis, or a name, which a keyword is not unless it
// names a type.
func startsType(t token) bool {
	if t.text == "(" {
		return true
	}
	return t.kind == tokIdent && (t.text == "int" || t.text == "function" || !reservedWords[t.text])
}

func checkUnionCase(line *sourceLine, warn func(*sourceLine, string)) {
	// Skip lines that are methods or end.
	if line.code[0].text == "end" || funcKeywordIndex(line.code) >= 0 {
		return
	}
	for _, t := range line.code {
		if t.kind == tokIdent && isCapabilityWord(t.text) {
			warn(line, fmt.Sprintf("capability qualifier '%s' on union payload is not supported in minimaxon", t.text))
		}
	}
}

func transformStmtAndExpr(line *sourceLine, warn func(*sourceLine, string)) {
	toks := line.code

	// Rewrite `freeze <unary_expr>` -> `freeze(<unary_expr>)` and `swap <target> with <expr>` -> `exchange(<target>, value: <expr>)`.
	toks = rewriteRegionExprs(toks, line, warn)
	toks = rewriteArmThrows(toks)
	toks = rewriteDiscards(toks)
	toks = rewriteClosureParams(toks, line, warn)
	line.code = toks

	// Statement-head checks.
	if len(toks) > 0 {
		switch toks[0].text {
		case "modify":
			if toks[len(toks)-1].kind == tokLabel {
				warn(line, "modify blocks are not supported in minimaxon")
			}
		case "acquire":
			if toks[len(toks)-1].kind == tokLabel {
				warn(line, "acquire blocks are not supported in minimaxon; use when with cowns")
			}
		case "for":
			if !hasRangeKeyword(toks) && !iteratesSupported(toks) {
				warn(line, "for loops over the characters of a string, its grapheme clusters, pieces or lines are not supported in minimaxon; loop over an array or a view")
			}
		case "let", "var":
			if len(toks) > 1 && toks[1].kind == tokIdent && isKeywordName(toks[1].text) {
				warn(line, keywordNameMessage(toks[1].text))
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
		case tokByteString:
			if !strings.HasSuffix(t.text, "\"") || len(t.text) < 3 {
				warn(line, "a byte string literal that spans lines is not supported in minimaxon")
			}
		case tokLabel:
			if isCharLiteralPosition(toks, i) {
				warn(line, "character literals are not supported in minimaxon")
			}
		case tokPunct:
			if t.text == "[" && isCollectionLiteralBracket(toks, i) && i+1 < len(toks) && toks[i+1].text == "]" {
				warn(line, "an empty array literal is not supported in minimaxon, as in maxon: write Array.create(0) under a typealias of the array")
			}
		case tokIdent:
			switch t.text {
			case "int":
				if i+2 < len(toks) && toks[i+1].text == "." && toks[i+2].text == "fromString" {
					warn(line, "'int.fromString' is not supported in minimaxon")
				}
			case "from":
				if isTypeFromValue(toks, i) {
					warn(line, "'Type from value' takes an array literal or a string literal in minimaxon, not another value")
				}
				if i > 0 && toks[i-1].text == "Set" {
					warn(line, fmt.Sprintf("'%s' is not supported in minimaxon", toks[i-1].text))
				}
			case "otherwise":
				if label := handlerLabelIndex(toks, i); toks[0].text != "end" && label >= 0 && label != len(toks)-1 {
					warn(line, "a block handler ('otherwise (e) 'label'') inside brackets is not supported in minimaxon; bind the value on a line of its own")
				}
			case "reply":
				if i+3 < len(toks) && toks[i+1].text == "." && toks[i+2].text == "later" && toks[i+3].text == "(" {
					warn(line, "reply.later() is not supported in minimaxon")
				}
			case "min":
				if i+1 < len(toks) && (toks[i+1].text == "to" || toks[i+1].text == "upto") && (i == 0 || toks[i-1].text != ".") {
					warn(line, "open-ended 'min to/upto' match range patterns are not supported in minimaxon")
				}
			case "max":
				if i > 0 && toks[i-1].text == "to" {
					warn(line, "open-ended 'to max' match range patterns are not supported in minimaxon")
				}
			}
		}
	}
}

// rewriteArmThrows turns a match arm `pattern throws E` into `pattern then throw E`, the one form minimaxon's arms have.
// rewriteDiscards drops the let of a discard binding, which minimaxon spells `_ = value`, at the start of a statement
// and as the statement of a match arm.
// rewriteClosureParams writes the parameters of each closure on the line, `function(...) gives ...`, as a function's are
// written: `n mutable T` becomes `mutates n T`.
func rewriteClosureParams(toks []token, line *sourceLine, warn func(*sourceLine, string)) []token {
	out := slices.Clone(toks)
	for i := 0; i+1 < len(out); i++ {
		if out[i].text != "function" || out[i+1].text != "(" {
			continue
		}
		closeIdx := findMatchingParen(out, i+1)
		if closeIdx < 0 {
			return out
		}
		params := rewriteParamList(slices.Clone(out[i+2:closeIdx]), nil, line, warn)
		rest := slices.Clone(out[closeIdx:])
		out = append(append(out[:i+2:i+2], params...), rest...)
	}
	return out
}

// rewriteFunctionType writes the target of a function typealias, `function(...) returns R`, with minimaxon's
// parameters: a parameter is its convention and its type, so `mutable T` becomes `mutates T` and a parameter's name is
// left out.
func rewriteFunctionType(rhs []token, line *sourceLine, warn func(*sourceLine, string)) []token {
	if len(rhs) < 2 || rhs[1].text != "(" {
		return rhs
	}
	closeIdx := findMatchingParen(rhs, 1)
	if closeIdx < 0 {
		return rhs
	}
	var params []token
	for gi, g := range splitTopLevel(rhs[2:closeIdx]) {
		if gi > 0 {
			params = append(params, token{kind: tokPunct, text: ","})
		}
		if len(g) >= 2 && g[0].kind == tokIdent && g[1].kind == tokIdent && g[1].text != "with" && g[0].text != "mutable" {
			g = g[1:]
		}
		if len(g) >= 1 && g[0].text == "mutable" {
			g = append([]token{{kind: tokIdent, text: "mutates"}}, g[1:]...)
		}
		params = append(params, g...)
	}
	out := append(slices.Clone(rhs[:2]), params...)
	return append(out, rhs[closeIdx:]...)
}

// splitTopLevel splits tokens at the commas outside brackets.
func splitTopLevel(toks []token) [][]token {
	if len(toks) == 0 {
		return nil
	}
	var groups [][]token
	depth, start := 0, 0
	for i, t := range toks {
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				groups = append(groups, toks[start:i])
				start = i + 1
			}
		}
	}
	return append(groups, toks[start:])
}

func rewriteDiscards(toks []token) []token {
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].text == "let" && toks[i+1].text == "_" && toks[i+2].text == "=" && (i == 0 || toks[i-1].text == "then") {
			return slices.Delete(slices.Clone(toks), i, i+1)
		}
	}
	return toks
}

func rewriteArmThrows(toks []token) []token {
	if toks[0].text == "end" || hasToken(toks, "function") {
		return toks
	}
	var out []token
	for i, t := range toks {
		if t.kind == tokIdent && t.text == "throws" && i > 0 && toks[i-1].text != "default" {
			out = append(out, token{kind: tokIdent, text: "then"}, token{kind: tokIdent, text: "throw"})
			continue
		}
		out = append(out, t)
	}
	return out
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

// handlerLabelIndex gives the index of the block label that follows the `otherwise` at idx as its handler, with or
// without the binding of the error, or -1. A label further along the line belongs to the statement that holds the
// expression. The handler block starts on the next line, so the label ends the line unless the expression is in brackets.
func handlerLabelIndex(toks []token, idx int) int {
	next := idx + 1
	if next+2 < len(toks) && toks[next].text == "(" && toks[next+2].text == ")" {
		next += 3
	}
	if next < len(toks) && toks[next].kind == tokLabel {
		return next
	}
	return -1
}

// isTypeFromValue reports whether the `from` at idx follows a type name and is followed by anything but the array literal
// or the string literal that minimaxon writes a type from.
func isTypeFromValue(toks []token, idx int) bool {
	if idx == 0 || toks[idx-1].kind != tokIdent || idx+1 >= len(toks) || toks[idx+1].text == "[" || toks[idx+1].kind == tokString {
		return false
	}
	prev := toks[idx-1].text
	return prev[0] >= 'A' && prev[0] <= 'Z' && (idx < 2 || toks[idx-2].text != ".")
}

func isCapabilityWord(word string) bool {
	return word == "isolated" || word == "immutable"
}

// isKeywordName reports whether word is a keyword of minimaxon, other than the discard `_` that a declaration may name.
func isKeywordName(word string) bool {
	return word != "_" && !slices.Contains(nameKeywords, word) && reservedWords[word]
}

// nameKeywords are the keywords that are still names wherever a name is required, as they are in Maxon: `from` follows a
// type only to write `Type from value`, and `to` follows a bound or an expression only to close a range.
var nameKeywords = []string{"from", "to"}

// keywordNameMessage words the warning for a name that minimaxon reserves and Maxon does not. A parameter may be
// declared with one, but its body cannot read it.
func keywordNameMessage(word string) string {
	return fmt.Sprintf("'%s' is a keyword in minimaxon and cannot be used as a name; rename it", word)
}

func hasToken(toks []token, text string) bool {
	for _, t := range toks {
		if t.text == text {
			return true
		}
	}
	return false
}

// unsupportedIterations are the members whose result a for loop cannot walk in minimaxon, which iterates arrays, the
// entries of a map, an enum's cases and names, cursors, and the views of a string's bytes, code points and UTF-16 units:
// a string's characters, grapheme clusters and pieces, maxon's cursors and the cases of a
// union. A name is a member only after a dot, and a method only when called, so a variable or a field of that name is
// left to the checker.
var unsupportedIterations = map[string]bool{
	"graphemes": true, "characters": true, "split": true, "lines": true,
	"cursor": true, "createIterator": true, unionCasesMember: true,
}

// maxon's pair loop is `for (iter, x) in xs.withIterator()`, whose iterator the body asks for its index(); minimaxon's is
// `for (i, x) in xs.enumerated()`, which binds the index itself (README, "Where it differs from Maxon").
const (
	withIteratorMethod  = "withIterator"
	enumeratedMethod    = "enumerated"
	iteratorIndexMethod = "index"
)

// iteratorUseMessage is the warning for a use of withIterator()'s iterator that is not its index(), which has no
// equivalent: the loop binds the index and nothing else.
const iteratorUseMessage = "a use of withIterator()'s iterator other than its index() is not supported in minimaxon: enumerated() binds the index alone"

// rewriteWithIterator turns each `for (iter, x) in xs.withIterator()` into `for (iter, x) in xs.enumerated()` and each
// `iter.index()` in its body into `iter`, the index that the loop now binds under the iterator's name. Any other use of the
// iterator is left as it is and warned of. A loop's body is the lines indented deeper than its `for`.
func rewriteWithIterator(lines []sourceLine, warn func(*sourceLine, string)) {
	type openLoop struct {
		indent   string
		iterator string
	}
	var open []openLoop
	for i := range lines {
		line := &lines[i]
		if line.removed || len(line.code) == 0 {
			continue
		}
		for len(open) > 0 && len(open[len(open)-1].indent) >= len(line.indent) {
			open = open[:len(open)-1]
		}
		for k, loop := range open {
			if !slices.ContainsFunc(open[k+1:], func(inner openLoop) bool { return inner.iterator == loop.iterator }) {
				line.code = rewriteIteratorUses(line, loop.iterator, warn)
			}
		}
		call := withIteratorCall(line.code)
		if call < 0 {
			continue
		}
		line.code[call].text = enumeratedMethod
		if iterator := line.code[2].text; iterator != "_" {
			open = append(open, openLoop{indent: line.indent, iterator: iterator})
		}
	}
}

// withIteratorCall is the index of the `withIterator` of a `for (iter, x) in xs.withIterator() 'label'` line, or -1 when
// the line is no such loop.
func withIteratorCall(toks []token) int {
	n := len(toks)
	if n < 9 || toks[0].text != "for" || toks[1].text != "(" || toks[2].kind != tokIdent || toks[3].text != "," || toks[n-1].kind != tokLabel {
		return -1
	}
	call := n - 4
	if toks[call-1].text != "." || toks[call].text != withIteratorMethod || toks[call+1].text != "(" || toks[call+2].text != ")" {
		return -1
	}
	return call
}

// rewriteIteratorUses is the line with each `iterator.index()` made `iterator`, warning of any other use of the name. The
// names a `for` binds, before its `in`, are no use of it.
func rewriteIteratorUses(line *sourceLine, iterator string, warn func(*sourceLine, string)) []token {
	toks := line.code
	from := 0
	if toks[0].text == "for" {
		from = findToken(toks, 0, "in")
		if from < 0 {
			from = len(toks)
		}
	}
	out := slices.Clone(toks[:from])
	for k := from; k < len(toks); k++ {
		t := toks[k]
		if t.kind == tokString {
			text, other := rewriteIteratorInString(t.text, iterator)
			if other {
				warn(line, iteratorUseMessage)
			}
			t.text = text
			out = append(out, t)
			continue
		}
		if t.kind != tokIdent || t.text != iterator || (k > 0 && toks[k-1].text == ".") {
			out = append(out, t)
			continue
		}
		if k+4 < len(toks) && toks[k+1].text == "." && toks[k+2].text == iteratorIndexMethod && toks[k+3].text == "(" && toks[k+4].text == ")" {
			out = append(out, t)
			k += 4
			continue
		}
		warn(line, iteratorUseMessage)
		out = append(out, t)
	}
	return out
}

// rewriteIteratorInString is rewriteIteratorUses for the interpolations of a string literal, which the lexer keeps in the
// string's one token: each `iterator.index()` in braces becomes `iterator`, and other is whether the name is used there
// otherwise.
func rewriteIteratorInString(text, iterator string) (string, bool) {
	indexCall := "." + iteratorIndexMethod + "()"
	var b strings.Builder
	depth := 0
	other := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\\' && depth == 0 && i+1 < len(text):
			b.WriteByte(c)
			b.WriteByte(text[i+1])
			i++
			continue
		case c == '{':
			depth++
		case c == '}' && depth > 0:
			depth--
		case depth > 0 && strings.HasPrefix(text[i:], iterator) && (i == 0 || !isIdentPart(text[i-1]) && text[i-1] != '.') && (i+len(iterator) == len(text) || !isIdentPart(text[i+len(iterator)])):
			b.WriteString(iterator)
			i += len(iterator) - 1
			if strings.HasPrefix(text[i+1:], indexCall) {
				i += len(indexCall)
			} else {
				other = true
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), other
}

// unionCasesMember is the one unsupported member that is a property, which a loop writes without a call.
const unionCasesMember = "unionCases"

// iteratesSupported is whether the source of a `for x in source` loop may be something minimaxon walks: the converter
// cannot see its type, so it refuses only the sources it can tell are something else, a string literal on its own (a loop
// over its characters) and a member in unsupportedIterations, written after a dot as a name or a call.
func iteratesSupported(toks []token) bool {
	in := -1
	for i, t := range toks {
		if t.kind == tokIdent && t.text == "in" {
			in = i
			break
		}
	}
	if in < 0 {
		return false
	}
	end := len(toks)
	if end > 0 && toks[end-1].kind == tokLabel {
		end--
	}
	source := toks[in+1 : end]
	if len(source) == 0 {
		return false
	}
	if len(source) == 1 && source[0].kind == tokString {
		return false
	}
	for i := 1; i < len(source); i++ {
		if source[i-1].text != "." || !unsupportedIterations[source[i].text] {
			continue
		}
		if source[i].text == unionCasesMember || (i+1 < len(source) && source[i+1].text == "(") {
			return false
		}
	}
	return true
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
// so both clean files (before the compiler formats them) and files with unsupported constructs get consistent layout.
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
		if prev.kind == tokIdent && (!spaceBeforeParenKeyword(prev.text) || (prevIdx > 0 && (toks[prevIdx-1].text == "." || toks[prevIdx-1].text == "function"))) {
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
		"returns", "where", "implements", "uses", "from", "await", "async", "when":
		return true
	}
	return false
}
