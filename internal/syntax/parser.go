package syntax

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"strings"
)

//go:generate go run minimaxon/internal/grammargen

// Parser holds what the generated parse functions (parser_gen.go) share: the module's source, its token stream,
// the position in it, and the file the parser is in.
type Parser struct {
	src  *Source
	toks []Token
	// tree holds the nodes and lists the lists being built; an interpolation's parser shares both with the parser of the
	// string it is in.
	tree      *ast.Tree
	lists     *listStacks
	decls     *listStack[ast.Decl]
	p         int
	fileIndex int
	Errors    []*diag.CompileError
	// spare is a token buffer an interpolation's parser borrows, so each one does not allocate its own.
	spare []Token
	// parts and strParts are reused by each string literal's conversion.
	parts    []StrPart
	strParts ast.StrPartsBuilder
	// interpolation reads the interpolations of the strings this parser reads.
	interpolation *Parser

	// The layout: where the last token consumed ends and what it was, whether the next token joins it with no
	// space between them, and which blank lines may come before the next line.
	prevEnd  int
	prevKind T
	joinNext bool
	blanks   blankRule
	// format is set when the parser formats: where it would refuse a layout, it writes the layout the rules require.
	format *formatter
}

// blankRule says which blank lines may come before a line: ParseProgram sets the rule between declarations, and a
// function inside a declaration has its own.
type blankRule int

const (
	// noBlanks allows none: the line is inside a declaration.
	noBlanks blankRule = iota
	// firstInFile allows blank lines between comment paragraphs, but not before the first line of the file.
	firstInFile
	// afterDecl requires one blank line right after the declaration before, and allows one between comment
	// paragraphs.
	afterDecl
	// beforeFunction is afterDecl for a method or an interface's function that is not the first line of its body: a
	// blank line sets each function apart, as between top-level declarations.
	beforeFunction
)

// NewParser parses the text of the module with handle 0, the project root; a module of a project is parsed by a Scratch,
// which is told its handle.
func NewParser(src *Source, toks []Token) *Parser { return newParser(src, toks, &Scratch{}, 0) }

func newParser(src *Source, toks []Token, scratch *Scratch, module ast.ModuleID) *Parser {
	p := &Parser{src: src, toks: toks, tree: ast.NewTree(module, src.Text, len(toks)), lists: &scratch.lists, decls: &scratch.decls,
		prevKind: TFileEnd, blanks: firstInFile}
	if len(src.Files) > 0 {
		p.prevEnd = src.Files[0].Begin
	}
	return p
}

func (p *Parser) fileNumber() int { return min(p.fileIndex, len(p.src.Files)-1) }

func (p *Parser) file() string { return p.src.Files[p.fileNumber()].File }

func (p *Parser) lineCol(t *Token) (int, int) { return p.src.lineCol(p.fileNumber(), int(t.Start)) }

// text is a token's text: a substring of the module's source.
func (p *Parser) text(t Token) string { return p.src.Text[t.Start:t.End()] }

func (p *Parser) cur() *Token { return &p.toks[p.p] }

func (p *Parser) peekTok() *Token { return &p.toks[min(p.p+1, len(p.toks)-1)] }

// next consumes the current token, after checking the layout before it.
func (p *Parser) next() Token {
	t := p.toks[p.p]
	p.layout(&t)
	return p.skip()
}

// layout checks the gap before t, or in a formatting parser writes the gap the rules require there and then t.
func (p *Parser) layout(t *Token) {
	if p.format != nil {
		p.formatToken(t)
		return
	}
	p.checkLayout(t)
}

// skip consumes the current token without checking its layout, as error recovery does.
func (p *Parser) skip() Token {
	t := p.toks[p.p]
	if p.p < len(p.toks)-1 {
		p.p++
	}
	p.prevEnd, p.prevKind = lexemeEnd(t), t.Kind()
	p.joinNext = t.Kind() < numTokens && tokenNoSpaceAfter[t.Kind()]
	return t
}

func (p *Parser) at(k T) bool { return p.toks[p.p].Kind() == k }

func (p *Parser) in(set *[numTokens]bool) bool { return set[p.toks[p.p].Kind()] }

func (p *Parser) peekIs(k T) bool { return p.peekTok().Kind() == k }

func (p *Parser) peekIn(set *[numTokens]bool) bool { return set[p.peekTok().Kind()] }

func (p *Parser) atEndOfInput() bool { return p.at(TFileEnd) || p.at(TEof) }

// lexemeStart and lexemeEnd are where a token's source text begins and ends: a label's token leaves out its
// quotes.
func lexemeStart(t Token) int {
	if t.Kind() == TLabel {
		return int(t.Start) - 1
	}
	return int(t.Start)
}

func lexemeEnd(t Token) int {
	if t.Kind() == TLabel {
		return t.End() + 1
	}
	return t.End()
}

// startsLine reports whether a token of kind is the last thing before a new line's first token.
func startsLine(kind T) bool {
	return kind == TNewline || kind == TIndent || kind == TDedent || kind == TFileEnd
}

// gap is what the layout puts before a token.
type gap int

const (
	// gapLines is the start of a line: only the blank lines and comment lines before it are left to decide.
	gapLines gap = iota
	// gapEndOfLine is before a newline: nothing, or a comment after the code.
	gapEndOfLine
	// gapNone joins the token to the one before it.
	gapNone
	// gapSpace is one space.
	gapSpace
)

// gapBefore is the one place that says what separates a token of kind from the one before it: within a line, none
// where it, or the token before it, joins its neighbour, and one space otherwise. The checker refuses another gap and
// the formatter writes this one.
func (p *Parser) gapBefore(kind T) gap {
	switch {
	case startsLine(p.prevKind):
		return gapLines
	case kind == TNewline:
		return gapEndOfLine
	case p.joinNext || tokenNoSpaceBefore[kind]:
		return gapNone
	}
	return gapSpace
}

// checkLayout checks the space before t. Within a line the lexer has made every gap one space or none; here the
// grammar decides which. At the start of a line the lexer has checked the indentation, and only the blank lines
// before it are left to check.
func (p *Parser) checkLayout(t *Token) {
	start := lexemeStart(*t)
	switch p.gapBefore(t.Kind()) {
	case gapLines:
		p.blankLines(start)
	case gapEndOfLine:
		// The gap may hold a comment after the code, which the lexer has checked.
	case gapNone:
		if start > p.prevEnd {
			msg := "expected no space before '" + p.describe(t) + "'"
			panic(p.src.errorAt(diag.ErrExpectedToken, msg, p.fileNumber(), p.prevEnd))
		}
	case gapSpace:
		if start == p.prevEnd {
			panic(p.layoutError(t, "expected a space before '"+p.describe(t)+"'"))
		}
	}
}

func (p *Parser) layoutError(t *Token, msg string) *diag.CompileError {
	line, col := p.lineCol(t)
	return diag.NewError(diag.ErrExpectedToken, msg, p.file(), line, col)
}

// blankFault is how the blank lines before a line break the rule: a blank line that is not allowed (a formatter
// removes it), or one that is missing (a formatter adds it).
type blankFault int

const (
	noFault blankFault = iota
	extraBlank
	missingBlank
)

const (
	blankInBody           = "a blank line is only allowed between declarations, and before a function of a type, an enum or a union"
	blankBeforeFunction   = "expected a blank line before this function, which is not the first line of its type, enum or union"
	blankAfterDeclaration = "expected a blank line after the declaration above"
)

// lineFault is the fault of the line that would come after kept lines under rule, blank or not, the one before it
// being lastBlank.
func lineFault(rule blankRule, kept int, blank, lastBlank bool) (blankFault, string) {
	switch {
	case blank && rule == noBlanks:
		return extraBlank, blankInBody
	case blank && kept == 0 && rule == firstInFile:
		return extraBlank, "the file starts with a blank line"
	case blank && lastBlank:
		return extraBlank, "two blank lines in a row; keep one"
	case !blank && kept == 0 && rule == afterDecl:
		return missingBlank, blankAfterDeclaration
	case !blank && kept == 0 && rule == beforeFunction:
		return missingBlank, blankBeforeFunction
	}
	return noFault, ""
}

// endFault is the fault of the gap's end, when kept lines have come before the line that starts it, the last one
// blank or not, and that line is the end of the file or not.
func endFault(rule blankRule, kept int, lastBlank, atFileEnd bool) (blankFault, string) {
	switch {
	case rule == afterDecl && kept == 0 && !atFileEnd:
		return missingBlank, blankAfterDeclaration
	case rule == beforeFunction && kept == 0:
		return missingBlank, blankBeforeFunction
	case lastBlank && atFileEnd:
		return extraBlank, "the file ends with a blank line"
	}
	return noFault, ""
}

// blankLines checks the blank lines and comment lines between the last line and the one that starts at lineStart,
// against the rule ParseProgram set for it. A formatting parser writes the lines the rule allows instead of
// refusing the others.
func (p *Parser) blankLines(lineStart int) {
	rule := p.blanks
	p.blanks = noBlanks
	if rule == noBlanks && p.prevKind == TNewline && p.startsFunction() {
		rule = beforeFunction
	}
	gap := p.src.Text[p.prevEnd:lineStart]
	at := p.prevEnd
	kept := 0
	lastBlank := false
	depth := 0
	if p.format != nil {
		depth = p.depthAfterIndentTokens()
	}
	for {
		end := strings.IndexByte(gap, '\n')
		if end < 0 {
			break
		}
		line := gap[:end]
		blank := isBlankLine(line)
		if fault, msg := lineFault(rule, kept, blank, lastBlank); fault != noFault {
			p.refuse(msg, at)
			if fault == extraBlank {
				gap, at = gap[end+1:], at+end+1
				continue
			}
			p.format.blankLine()
			kept++
		}
		if p.format != nil {
			p.format.gapLine(line, blank, depth)
		}
		kept++
		lastBlank = blank
		gap, at = gap[end+1:], at+end+1
	}
	if fault, msg := endFault(rule, kept, lastBlank, p.at(TFileEnd)); fault != noFault {
		if fault == extraBlank {
			p.refuse(msg, at-1)
			p.format.dropBlankLine()
		} else {
			p.refuse(msg, at)
			p.format.blankLine()
		}
	}
}

// refuse is the syntax error for a gap that breaks the layout. A formatting parser carries on, to write the layout the
// rules require instead.
func (p *Parser) refuse(msg string, at int) {
	if p.format == nil {
		panic(p.src.errorAt(diag.ErrExpectedToken, msg, p.fileNumber(), at))
	}
}

// isBlankLine is whether a line holds nothing but whitespace, which only a line the formatter reads can.
func isBlankLine(line string) bool {
	for i := range len(line) {
		if c := line[i]; c != ' ' && c != '\t' && c != '\n' {
			return false
		}
	}
	return true
}

// startsFunction is whether the line starting at the current token defines a function inside a declaration: a method
// or a static function. No statement starts with these words, so inside a function body it is never true. An
// interface's functions are signatures, one line each with no body, and are listed without blank lines between them.
func (p *Parser) startsFunction() bool {
	switch p.cur().Kind() {
	case TKw_function, TKw_static:
	case TKw_export, TKw_public:
		if next := p.peekTok().Kind(); next != TKw_function && next != TKw_static {
			return false
		}
	default:
		return false
	}
	return p.lineOpensBody()
}

// lineOpensBody is whether the line starting at the current token is followed by an indented body.
func (p *Parser) lineOpensBody() bool {
	for i := p.p; i+1 < len(p.toks); i++ {
		if p.toks[i].Kind() == TNewline {
			return p.toks[i+1].Kind() == TIndent
		}
	}
	return false
}

// describe is how a syntax error shows a token: its text, or a name for a token whose text is not shown.
func (p *Parser) describe(t *Token) string {
	switch t.Kind() {
	case TNewline:
		return newlineText
	case TFileEnd:
		return fileEndText
	case TEof:
		return eofText
	case TString:
		return stringText
	}
	if t.Len() == 0 {
		return tokenDescription[t.Kind()]
	}
	return p.text(*t)
}

// expected is the syntax error at the current token, naming what the grammar allows there.
func (p *Parser) expected(what string) *diag.CompileError {
	t := p.cur()
	line, col := p.lineCol(t)
	if p.atEndOfInput() {
		return diag.NewError(diag.ErrUnexpectedEnd, diag.UnexpectedEof(), p.file(), line, col)
	}
	return diag.NewError(diag.ErrExpectedToken, diag.ExpectedToken(what, p.describe(t)), p.file(), line, col)
}

func (p *Parser) expect(k T, what string) Token {
	if !p.at(k) {
		panic(p.expected(what))
	}
	return p.next()
}

// textRef is what a record holds for the name or label that a token is.
func (p *Parser) textRef(t Token) uint32 { return ast.TextRef(t.Start) }

// takeList moves the numbers pushed since mark from their stack into the tree's list array, as the list they make.
func takeList[V ast.Viewer](p *Parser, stack *listStack[uint32], mark int) ast.List[V] {
	return ast.ListAt[V](p.tree, p.takeIDs(stack, mark))
}

// takeTexts is takeList for a list of names.
func takeTexts(p *Parser, stack *listStack[uint32], mark int) ast.TextList {
	return ast.TextListAt(p.tree, p.takeIDs(stack, mark))
}

func (p *Parser) takeIDs(stack *listStack[uint32], mark int) uint32 {
	ref := p.tree.AppendList(stack.items[mark:])
	stack.items = stack.items[:mark]
	return ref
}

// ParseProgram parses every declaration of a module's files. A syntax error abandons the rest of its file, so
// each file reports its own first error.
func (p *Parser) ParseProgram() *ast.Program {
	declsMark := p.decls.mark()
	decls, failed := 0, false
	for !p.at(TEof) {
		if p.at(TFileEnd) {
			if !failed {
				diag.Guard(&p.Errors, func() {
					p.blanks = firstInFile
					if decls > 0 {
						p.blanks = afterDecl
					}
					p.checkLayout(p.cur())
				})
			}
			p.skip()
			p.fileIndex++
			decls, failed = 0, false
			continue
		}
		errors := len(p.Errors)
		diag.Guard(&p.Errors, func() {
			p.blanks = firstInFile
			if decls > 0 {
				p.blanks = afterDecl
			}
			p.decls.push(p.parseDecl())
			p.expect(TNewline, "the end of the line")
		})
		decls++
		if len(p.Errors) != errors {
			failed = true
			p.lists.reset()
			for !p.atEndOfInput() {
				p.skip()
			}
		}
	}
	return &ast.Program{Decls: p.decls.take(declsMark), Tree: p.tree, Positions: p.src.Positions}
}

// The conversions below turn a token into a field that is not its text, as Maxon.g4 declares.

func (p *Parser) intLitMagnitude(t Token) ast.I128 { return p.integer(t) }

func (p *Parser) boundValue(t Token) ast.I128 { return p.integer(t) }

func (p *Parser) integer(t Token) ast.I128 {
	value, _ := p.src.readInteger(p.fileNumber(), int(t.Start), t.End())
	return value
}

// strLitParts reads the string literal that t is and gives the parts it holds in the tree: a run of the module's text
// is its offset and length, text that an escape changed is a string of its own, and an interpolation is the
// expression that its text parses to.
func (p *Parser) strLitParts(t Token) uint32 {
	p.parts = p.parts[:0]
	p.src.readString(p.fileNumber(), int(t.Start), &p.parts)
	p.strParts.Reset()
	var formatted []string
	for _, part := range p.parts {
		switch part := part.(type) {
		case TextPart:
			if part.Raw {
				p.strParts.Run(uint32(part.Begin), uint32(len(part.Text)))
			} else {
				p.strParts.Decoded(p.tree, part.Text)
			}
		case ExprPart:
			// The sub-parser borrows the spare buffer; an interpolation nested inside it finds none and allocates.
			toks := lexInterpolation(p.src, p.fileNumber(), part.Begin, part.End, p.spare, p.format != nil)
			p.spare = nil
			// The interpolation's first token follows its '{' with no space between them.
			sub := p.interpolationParser()
			*sub = Parser{src: p.src, toks: toks, tree: p.tree, lists: p.lists, fileIndex: p.fileNumber(),
				prevEnd: part.Begin, prevKind: TLBrace, joinNext: true,
				spare: sub.spare, parts: sub.parts, strParts: sub.strParts, interpolation: sub.interpolation}
			if p.format != nil {
				sub.format = &formatter{}
			}
			p.strParts.Interpolation(sub.parseInterpolation())
			if p.format != nil {
				formatted = append(formatted, string(sub.format.out))
			}
			p.spare = sub.toks[:0]
		default:
			panic(diag.Internal("unknown string part %T", part))
		}
	}
	if p.format != nil {
		p.format.replaceString(p.src.Text, t, p.parts, formatted)
	}
	return p.strParts.Finish(p.tree)
}

// interpolationParser is the parser of the interpolations of p's strings. Each interpolation reads in it after the one before,
// so it is made once, and an interpolation nested in an interpolation is read by the one it holds.
func (p *Parser) interpolationParser() *Parser {
	if p.interpolation == nil {
		p.interpolation = &Parser{}
	}
	return p.interpolation
}

func (p *Parser) parseInterpolation() ast.Expr {
	e := p.parseExpr()
	if !p.atEndOfInput() {
		panic(p.expected("the end of the interpolation"))
	}
	return e
}
