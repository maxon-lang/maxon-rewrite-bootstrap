package syntax

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

func isCapability(word string) bool { return word == "isolated" || word == "immutable" }

func sizedTypeRange(name string) (lo, hi ast.I128, ok bool) {
	switch name {
	case "u8":
		return ast.Zero, ast.I64(255), true
	case "u16":
		return ast.Zero, ast.I64(65535), true
	case "u32":
		return ast.Zero, ast.I64(4294967295), true
	case "u64":
		return ast.Zero, ast.U64Max, true
	case "i8":
		return ast.I64(-128), ast.I64(127), true
	case "i16":
		return ast.I64(-32768), ast.I64(32767), true
	case "i32":
		return ast.I64(-2147483648), ast.I64(2147483647), true
	case "i64":
		return ast.I64Min, ast.I64Max, true
	}
	return ast.Zero, ast.Zero, false
}

var binaryLevels = [][]string{
	{"or"},
	{"xor"},
	{"and"},
	{"==", "!=", "<", "<=", ">", ">="},
	{"shl", "shr"},
	{"+", "-"},
	{"*", "/", "mod"},
}

const comparisonLevel = 3

type Parser struct {
	toks      []Token
	files     []string
	p         int
	fileIndex int
	Errors    []*diag.CompileError
}

func NewParser(toks []Token, files []string) *Parser {
	return &Parser{toks: toks, files: files}
}

func (p *Parser) file() string { return p.files[min(p.fileIndex, len(p.files)-1)] }

func (p *Parser) cur() *Token { return &p.toks[p.p] }

func (p *Parser) peekTok() *Token { return &p.toks[min(p.p+1, len(p.toks)-1)] }

func (p *Parser) next() Token {
	t := p.toks[p.p]
	if p.p < len(p.toks)-1 {
		p.p++
	}
	return t
}

func (p *Parser) at(k T) bool { return p.cur().Kind == k }

func (p *Parser) atWord(w string) bool { return p.cur().Is(w) }

func (p *Parser) accept(k T) bool {
	if !p.at(k) {
		return false
	}
	p.next()
	return true
}

func (p *Parser) acceptWord(w string) bool {
	if !p.atWord(w) {
		return false
	}
	p.next()
	return true
}

func (p *Parser) errorAt(t *Token, code, msg string) *diag.CompileError {
	return diag.NewError(code, msg, p.file(), t.Line, t.Col)
}

func (p *Parser) atEndOfInput() bool {
	k := p.cur().Kind
	return k == TFileEnd || k == TEof
}

func (p *Parser) unexpected(expected string) *diag.CompileError {
	t := p.cur()
	if p.atEndOfInput() {
		return p.errorAt(t, diag.ErrUnexpectedEnd, "unexpected end of file, expected "+expected)
	}
	return p.errorAt(t, diag.ErrUnexpectedToken, fmt.Sprintf("expected %s, found '%s'", expected, t.Text))
}

func (p *Parser) unsupported(what string) *diag.CompileError {
	return p.errorAt(p.cur(), diag.ErrUnsupported, what+" is not supported by minimaxon")
}

func (p *Parser) expect(k T, what string) Token {
	if !p.at(k) {
		if p.atEndOfInput() {
			panic(p.unexpected(what))
		}
		panic(p.errorAt(p.cur(), diag.ErrExpectedToken, fmt.Sprintf("expected %s, found '%s'", what, p.cur().Text)))
	}
	return p.next()
}

func (p *Parser) expectWord(w string) Token {
	if !p.atWord(w) {
		if p.atEndOfInput() {
			panic(p.unexpected("'" + w + "'"))
		}
		panic(p.errorAt(p.cur(), diag.ErrExpectedToken, fmt.Sprintf("expected '%s', found '%s'", w, p.cur().Text)))
	}
	return p.next()
}

func (p *Parser) expectName(what string) string {
	if !p.at(TIdent) || ast.IsReserved(p.cur().Text) {
		panic(p.unexpected(what))
	}
	return p.next().Text
}

func place[N ast.Node](p *Parser, node N, t *Token) N {
	pos := node.P()
	pos.File = p.file()
	pos.Line = t.Line
	pos.Col = t.Col
	return node
}

func newExprBase() ast.ExprBase { return ast.ExprBase{Type: ast.VoidTy} }

func (p *Parser) skipNewlines() {
	for p.at(TNewline) {
		p.next()
	}
}

func (p *Parser) expectNewline() {
	if p.at(TEof) || p.at(TFileEnd) {
		return
	}
	p.expect(TNewline, "end of line")
}

func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{}
	p.skipNewlines()
	for !p.at(TEof) {
		if p.accept(TFileEnd) {
			p.fileIndex++
			p.skipNewlines()
			continue
		}
		failed := len(p.Errors)
		diag.Guard(&p.Errors, func() {
			prog.Decls = append(prog.Decls, p.parseTopDecl())
			p.expectNewline()
			p.skipNewlines()
		})
		if len(p.Errors) != failed {
			for !p.at(TFileEnd) && !p.at(TEof) {
				p.next()
			}
		}
	}
	return prog
}

func (p *Parser) parseVisibility() string {
	if p.atWord("export") || p.atWord("public") {
		return p.next().Text
	}
	return ""
}

func (p *Parser) parseTopDecl() ast.Decl {
	vis := p.parseVisibility()
	t := p.cur()
	switch {
	case p.atWord("function"):
		return p.parseFunction(vis, false, false)
	case p.atWord("type"):
		return p.parseType(vis)
	case p.atWord("enum"):
		return p.parseEnum(vis)
	case p.atWord("union"):
		return p.parseUnion(vis)
	case p.atWord("typealias"):
		return p.parseAlias(vis)
	case p.atWord("let"):
		p.next()
		c := place(p, &ast.ConstDecl{DeclBase: ast.DeclBase{Visibility: vis}}, t)
		c.Name = p.expectName("a constant name")
		p.expect(TAssign, "'='")
		c.Value = p.ParseExpr()
		return c
	case p.atWord("interface") || p.atWord("extension") || p.atWord("default") || p.atWord("test"):
		panic(p.unsupported("'" + p.cur().Text + "'"))
	}
	panic(p.unexpected("a declaration"))
}

func isReceiverWord(word string) bool {
	return word == "mutable" || word == "immutable" || word == "isolated" || word == "consuming"
}

func (p *Parser) parseFunction(vis string, inType, isStatic bool) *ast.FuncDecl {
	t := p.expectWord("function")
	f := place(p, &ast.FuncDecl{DeclBase: ast.DeclBase{Visibility: vis}, IsStatic: isStatic, ReturnType: ast.VoidTy}, &t)
	f.Name = p.expectName("a function name")
	p.expect(TLParen, "'('")

	if !p.at(TRParen) {
		if p.at(TIdent) && p.peekTok().Is("self") && isReceiverWord(p.cur().Text) {
			if !inType || isStatic {
				panic(p.errorAt(p.cur(), diag.ErrUnexpectedToken, "only an instance method takes a receiver parameter"))
			}
			f.Receiver = p.next().Text
			p.next()
			if !p.accept(TComma) && !p.at(TRParen) {
				panic(p.unexpected("',' or ')'"))
			}
		}
		for !p.at(TRParen) {
			f.Params = append(f.Params, p.parseParam())
			if !p.accept(TComma) {
				break
			}
		}
	}
	p.expect(TRParen, "')'")

	if p.acceptWord("returns") {
		f.Returns = p.parseTypeRef()
	}
	if p.atWord("throws") {
		panic(p.unsupported("'throws'"))
	}

	p.expectNewline()
	f.Body = p.parseBlock(&t)
	p.expectEnd(f.Name)
	return f
}

func (p *Parser) parseParam() *ast.Param {
	t := p.cur()
	prm := place(p, &ast.Param{}, t)
	if !p.at(TIdent) || (ast.IsReserved(p.cur().Text) && p.cur().Text != "_") {
		panic(p.unexpected("a parameter name"))
	}
	prm.Name = p.next().Text
	if p.atWord("mutable") && p.peekTok().Kind == TIdent {
		p.next()
		prm.Mutable = true
	}
	prm.Type = p.parseTypeRef()
	if p.at(TAssign) {
		panic(p.unsupported("a default parameter value"))
	}
	return prm
}

func (p *Parser) parseTypeRef() *ast.TypeRef {
	t := p.cur()
	tr := place(p, &ast.TypeRef{}, t)
	if p.at(TIdent) && isCapability(p.cur().Text) && p.peekTok().Kind == TIdent {
		tr.Capability = p.next().Text
	}
	if p.at(TLParen) {
		panic(p.unsupported("a tuple type"))
	}
	if !p.at(TIdent) {
		panic(p.errorAt(p.cur(), diag.ErrExpectedType, fmt.Sprintf("expected a type, found '%s'", p.cur().Text)))
	}
	tr.Name = p.next().Text
	for p.accept(TDot) {
		tr.Qualifier = append(tr.Qualifier, tr.Name)
		tr.Name = p.expectName("a type name")
	}
	if p.atWord("with") {
		panic(p.unsupported("a generic type"))
	}
	return tr
}

func (p *Parser) expectEnd(label string) {
	p.expectWord("end")
	if p.at(TQuote) {
		q := p.next()
		if q.Text != label {
			panic(p.errorAt(&q, diag.ErrMismatchedEndLabel, fmt.Sprintf("'end' names '%s', but the block is '%s'", q.Text, label)))
		}
	}
}

func (p *Parser) expectLabel() string { return p.expect(TQuote, "a block label").Text }

func (p *Parser) parseBlock(opener *Token) []ast.Stmt {
	var list []ast.Stmt
	p.skipNewlines()
	for !p.atWord("end") {
		if p.at(TEof) || p.at(TFileEnd) {
			panic(p.unexpected("'end'"))
		}
		list = append(list, p.parseStatement())
		if p.atWord("end") {
			break
		}
		p.expectNewline()
		p.skipNewlines()
	}
	if len(list) == 0 {
		panic(p.errorAt(opener, diag.ErrEmptyBlock, "a block must contain at least one statement"))
	}
	return list
}

func (p *Parser) parseType(vis string) *ast.TypeDecl {
	t := p.expectWord("type")
	d := place(p, &ast.TypeDecl{TypeDeclBase: ast.TypeDeclBase{DeclBase: ast.DeclBase{Visibility: vis}}}, &t)
	d.Name = p.expectName("a type name")
	if p.atWord("is") || p.atWord("uses") || p.atWord("implements") || p.atWord("where") {
		panic(p.unsupported("'" + p.cur().Text + "' on a type"))
	}
	p.expectNewline()
	p.skipNewlines()

	for !p.atWord("end") {
		mvis := p.parseVisibility()
		mt := p.cur()
		switch {
		case p.atWord("var") || p.atWord("let"):
			fd := place(p, &ast.FieldDecl{Visibility: mvis, IsVar: p.next().Text == "var"}, mt)
			fd.Name = p.expectName("a field name")
			if p.acceptWord("as") {
				fd.Type = p.parseTypeRef()
			} else if p.accept(TAssign) {
				fd.Default = p.ParseExpr()
			} else {
				panic(p.unexpected("'as' or '='"))
			}
			d.Fields = append(d.Fields, fd)
		case p.atWord("function"):
			d.Methods = append(d.Methods, p.parseFunction(mvis, true, false))
		case p.atWord("static"):
			p.next()
			if !p.atWord("function") {
				panic(p.unsupported("a static field"))
			}
			d.Methods = append(d.Methods, p.parseFunction(mvis, true, true))
		default:
			panic(p.unexpected("a field or method"))
		}
		p.expectNewline()
		p.skipNewlines()
	}
	p.expectEnd(d.Name)
	return d
}

func (p *Parser) parseEnum(vis string) *ast.EnumDecl {
	t := p.expectWord("enum")
	d := place(p, &ast.EnumDecl{TypeDeclBase: ast.TypeDeclBase{DeclBase: ast.DeclBase{Visibility: vis}}}, &t)
	d.Name = p.expectName("an enum name")
	if p.atWord("implements") {
		panic(p.unsupported("'implements' on an enum"))
	}
	p.expectNewline()
	p.skipNewlines()

	for !p.atWord("end") {
		mvis := p.parseVisibility()
		if p.atWord("function") {
			d.Methods = append(d.Methods, p.parseFunction(mvis, true, false))
		} else {
			ct := p.cur()
			c := place(p, &ast.EnumCaseDecl{Name: p.expectName("an enum case")}, ct)
			if p.accept(TAssign) {
				neg := p.accept(TMinus)
				if !p.at(TInt) {
					panic(p.unsupported("a non-integer enum raw value"))
				}
				v := p.next().IntValue()
				if neg {
					v = v.Neg()
				}
				c.Raw = v
				c.HasRaw = true
			}
			d.Cases = append(d.Cases, c)
		}
		p.expectNewline()
		p.skipNewlines()
	}
	p.expectEnd(d.Name)
	return d
}

func (p *Parser) parseUnion(vis string) *ast.UnionDecl {
	t := p.expectWord("union")
	d := place(p, &ast.UnionDecl{TypeDeclBase: ast.TypeDeclBase{DeclBase: ast.DeclBase{Visibility: vis}}}, &t)
	d.Name = p.expectName("a union name")
	if p.atWord("implements") {
		panic(p.unsupported("'implements' on a union"))
	}
	p.expectNewline()
	p.skipNewlines()

	for !p.atWord("end") {
		mvis := p.parseVisibility()
		if p.atWord("function") {
			d.Methods = append(d.Methods, p.parseFunction(mvis, true, false))
		} else {
			ct := p.cur()
			c := place(p, &ast.UnionCaseDecl{Name: p.expectName("a union case")}, ct)
			if p.accept(TLParen) {
				for !p.at(TRParen) {
					fp := p.parseParam()
					if fp.Mutable {
						panic(p.errorAt(ct, diag.ErrExpectedToken, "a union payload takes no 'mutable'"))
					}
					c.Fields = append(c.Fields, fp)
					if !p.accept(TComma) {
						break
					}
				}
				p.expect(TRParen, "')'")
			}
			if p.at(TAssign) {
				panic(p.unsupported("a union raw value"))
			}
			d.Cases = append(d.Cases, c)
		}
		p.expectNewline()
		p.skipNewlines()
	}
	p.expectEnd(d.Name)
	return d
}

func (p *Parser) parseAlias(vis string) *ast.AliasDecl {
	t := p.expectWord("typealias")
	d := place(p, &ast.AliasDecl{TypeDeclBase: ast.TypeDeclBase{DeclBase: ast.DeclBase{Visibility: vis}}}, &t)
	d.Name = p.expectName("an alias name")
	p.expect(TAssign, "'='")
	if !p.atWord("int") {
		panic(p.unsupported("a typealias other than a ranged int"))
	}
	p.next()
	p.expect(TLParen, "'('")
	d.Min = p.parseBound()
	inclusive := p.atWord("to")
	if !p.acceptWord("to") && !p.acceptWord("upto") {
		panic(p.unexpected("'to' or 'upto'"))
	}
	d.Max = p.parseBound()
	if !inclusive {
		d.Max = d.Max.Sub(ast.One)
	}
	p.expect(TRParen, "')'")
	if p.atWord("implements") {
		panic(p.unsupported("'implements' on a typealias"))
	}
	if d.Min.Gt(d.Max) {
		panic(d.Error(diag.ErrTypeMismatch, fmt.Sprintf("the range of '%s' is empty", d.Name)))
	}
	if d.Min.IsNeg() && d.Max.Gt(ast.I64Max) {
		panic(d.Error(diag.ErrTypeMismatch, "an integer range cannot span both negative values and values above i64.max"))
	}
	return d
}

func (p *Parser) parseBound() ast.I128 {
	neg := p.accept(TMinus)
	var v ast.I128
	if p.at(TInt) {
		v = p.next().IntValue()
	} else if lo, hi, ok := sizedTypeRange(p.cur().Text); p.at(TIdent) && ok {
		p.next()
		p.expect(TDot, "'.'")
		switch p.expectName("'min' or 'max'") {
		case "min":
			v = lo
		case "max":
			v = hi
		default:
			panic(p.unexpected("'min' or 'max'"))
		}
	} else {
		panic(p.unexpected("a range bound"))
	}
	if neg {
		return v.Neg()
	}
	return v
}

func (p *Parser) parseStatement() ast.Stmt {
	t := p.cur()

	if p.atWord("let") || p.atWord("var") {
		s := place(p, &ast.LetStmt{IsVar: p.next().Text == "var"}, t)
		if p.at(TLParen) {
			panic(p.unsupported("tuple destructuring"))
		}
		s.Name = p.expectName("a variable name")
		p.expect(TAssign, "'='")
		s.Init = p.ParseExpr()
		return s
	}

	if p.at(TIdent) && p.cur().Text == "_" && p.peekTok().Kind == TAssign {
		p.next()
		p.next()
		return place(p, &ast.DiscardStmt{Value: p.ParseExpr()}, t)
	}

	if p.atWord("if") {
		return p.parseIf()
	}

	if p.atWord("while") {
		p.next()
		s := place(p, &ast.WhileStmt{Cond: p.ParseExpr()}, t)
		s.Label = p.expectLabel()
		s.Body = p.parseBlock(t)
		p.expectEnd(s.Label)
		return s
	}

	if p.atWord("for") {
		p.next()
		s := place(p, &ast.ForStmt{}, t)
		if p.at(TLParen) {
			panic(p.unsupported("a destructuring for loop"))
		}
		if !p.at(TIdent) || (ast.IsReserved(p.cur().Text) && p.cur().Text != "_") {
			panic(p.unexpected("a loop variable"))
		}
		s.Var = p.next().Text
		p.expectWord("in")
		s.From = p.parseOr()
		s.Inclusive = p.atWord("to")
		if !p.acceptWord("to") && !p.acceptWord("upto") {
			panic(p.unsupported("a for loop over anything but a range"))
		}
		s.To = p.parseOr()
		s.Label = p.expectLabel()
		s.Body = p.parseBlock(t)
		p.expectEnd(s.Label)
		return s
	}

	if p.atWord("match") {
		return place(p, &ast.MatchStmt{Match: p.parseMatchCore()}, t)
	}

	if p.atWord("return") {
		p.next()
		s := place(p, &ast.ReturnStmt{}, t)
		if !p.at(TNewline) && !p.at(TEof) && !p.atWord("end") {
			s.Value = p.ParseExpr()
		}
		return s
	}

	if p.atWord("break") || p.atWord("continue") {
		isBreak := p.next().Text == "break"
		label := ""
		if p.at(TQuote) {
			label = p.next().Text
		}
		if isBreak {
			return place(p, &ast.BreakStmt{Label: label}, t)
		}
		return place(p, &ast.ContinueStmt{Label: label}, t)
	}

	if p.at(TAt) {
		panic(p.unsupported("'@heap'"))
	}
	if p.atWord("try") || p.atWord("throw") {
		panic(p.unsupported("'" + p.cur().Text + "'"))
	}
	if (p.atWord("modify") || p.atWord("acquire")) && p.peekTok().Kind == TIdent {
		panic(p.unsupported("'" + p.cur().Text + "'"))
	}

	e := p.ParseExpr()
	if p.accept(TAssign) {
		switch e.(type) {
		case *ast.NameExpr, *ast.MemberExpr:
		default:
			panic(p.errorAt(t, diag.ErrUnexpectedToken, "only a variable or a field can be assigned"))
		}
		return place(p, &ast.AssignStmt{Target: e, Value: p.ParseExpr()}, t)
	}
	switch e.(type) {
	case *ast.CallExpr, *ast.PanicExpr:
	default:
		panic(p.errorAt(t, diag.ErrUnexpectedToken, "only a call can stand alone as a statement"))
	}
	return place(p, &ast.ExprStmt{E: e}, t)
}

func (p *Parser) parseIf() *ast.IfStmt {
	t := p.expectWord("if")
	if p.atWord("let") || p.atWord("var") || p.atWord("try") {
		panic(p.unsupported("'if let' and 'if try'"))
	}
	s := place(p, &ast.IfStmt{Cond: p.ParseExpr()}, &t)
	s.Label = p.expectLabel()
	s.Then = p.parseBlock(&t)
	p.expectEnd(s.Label)

	if p.atWord("else") {
		et := p.next()
		s.HasElse = true
		if p.atWord("if") {
			s.Else = []ast.Stmt{p.parseIf()}
		} else {
			if p.at(TLParen) {
				panic(p.unsupported("an else binding"))
			}
			label := p.expectLabel()
			s.Else = p.parseBlock(&et)
			p.expectEnd(label)
		}
	}
	return s
}

func (p *Parser) parseMatchCore() *ast.MatchCore {
	t := p.expectWord("match")
	m := place(p, &ast.MatchCore{Subject: p.ParseExpr()}, &t)
	m.Label = p.expectLabel()
	p.expectNewline()
	p.skipNewlines()
	for !p.atWord("end") {
		m.Arms = append(m.Arms, p.parseArm())
		p.expectNewline()
		p.skipNewlines()
	}
	p.expectEnd(m.Label)
	return m
}

func (p *Parser) parseArm() *ast.MatchArm {
	t := p.cur()
	arm := place(p, &ast.MatchArm{}, t)
	if p.acceptWord("default") {
		arm.IsDefault = true
	} else {
		for {
			arm.Patterns = append(arm.Patterns, p.parsePattern())
			if p.atWord("or") && p.peekTok().Kind == TNewline {
				p.next()
				p.skipNewlines()
				continue
			}
			break
		}
	}

	switch {
	case p.acceptWord("then"):
		arm.Kind = ast.ArmThen
		arm.Action = p.parseArmStatement()
		if p.atWord("and") {
			panic(p.unsupported("'and fallthrough'"))
		}
	case p.acceptWord("gives"):
		arm.Kind = ast.ArmGives
		arm.Value = p.ParseExpr()
	case p.atWord("panic"):
		arm.Kind = ast.ArmPanic
		arm.Value = p.parsePrimary()
	case p.atWord("throws"):
		panic(p.unsupported("'throws' in a match arm"))
	default:
		panic(p.unexpected("'then', 'gives' or 'panic'"))
	}
	return arm
}

func (p *Parser) parseArmStatement() ast.Stmt {
	if p.atWord("if") || p.atWord("while") || p.atWord("for") || p.atWord("match") {
		panic(p.errorAt(p.cur(), diag.ErrMatchBlockStatement, fmt.Sprintf("a '%s' block cannot be a match arm's action", p.cur().Text)))
	}
	return p.parseStatement()
}

func (p *Parser) parsePattern() *ast.Pattern {
	t := p.cur()
	pat := place(p, &ast.Pattern{CaseIndex: -1}, t)
	if p.at(TIdent) && !p.atWord("true") && !p.atWord("false") {
		pat.Kind = ast.PatternCase
		pat.CaseName = p.expectName("a case name")
		if p.at(TDot) {
			panic(p.errorAt(t, "E3075", "a match arm names a bare case, without its type"))
		}
		if p.accept(TLParen) {
			pat.HasBindings = true
			for !p.at(TRParen) {
				if !p.at(TIdent) || (ast.IsReserved(p.cur().Text) && p.cur().Text != "_") {
					panic(p.unexpected("a binding name"))
				}
				pat.Bindings = append(pat.Bindings, p.next().Text)
				if !p.accept(TComma) {
					break
				}
			}
			p.expect(TRParen, "')'")
		}
		return pat
	}

	pat.Value = p.parsePatternValue()
	if p.atWord("to") || p.atWord("upto") {
		pat.Kind = ast.PatternRange
		pat.Inclusive = p.next().Text == "to"
		pat.High = p.parsePatternValue()
	} else {
		pat.Kind = ast.PatternLiteral
	}
	return pat
}

func (p *Parser) parsePatternValue() ast.Expr {
	t := p.cur()
	if p.accept(TMinus) {
		v := p.expect(TInt, "an integer").IntValue()
		return place(p, &ast.IntLit{ExprBase: newExprBase(), Value: v.Neg()}, t)
	}
	if p.at(TInt) || p.at(TStr) || p.atWord("true") || p.atWord("false") {
		return p.parsePrimary()
	}
	panic(p.unexpected("a pattern"))
}

func (p *Parser) parseStandaloneExpr() ast.Expr {
	e := p.ParseExpr()
	if !p.at(TNewline) && !p.at(TEof) && !p.at(TFileEnd) {
		panic(p.unexpected("the end of the interpolation"))
	}
	return e
}

func (p *Parser) ParseExpr() ast.Expr {
	t := p.cur()
	e := p.parseOr()
	if p.atWord("if") {
		p.next()
		cond := p.parseOr()
		p.expectWord("else")
		els := p.ParseExpr()
		return place(p, &ast.CondExpr{ExprBase: newExprBase(), Cond: cond, Then: e, Else: els}, t)
	}
	if p.atWord("to") || p.atWord("upto") {
		panic(p.unsupported("a range value"))
	}
	return e
}

func (p *Parser) binaryOperator(level int) (string, bool) {
	t := p.cur()
	if t.Kind == TStr || t.Kind == TQuote || t.Kind == TInt {
		return "", false
	}
	for _, op := range binaryLevels[level] {
		if t.Text == op {
			return op, true
		}
	}
	return "", false
}

func (p *Parser) parseBinary(level int) ast.Expr {
	if level == len(binaryLevels) {
		return p.parseCast()
	}
	e := p.parseBinary(level + 1)
	for {
		if level == comparisonLevel && p.atWord("is") {
			panic(p.unsupported("'is'"))
		}
		t := p.cur()
		op, ok := p.binaryOperator(level)
		if !ok {
			return e
		}
		p.next()
		e = place(p, &ast.BinaryExpr{ExprBase: newExprBase(), Op: op, L: e, R: p.parseBinary(level + 1), OperandType: ast.VoidTy}, t)
	}
}

func (p *Parser) parseOr() ast.Expr { return p.parseBinary(0) }

func (p *Parser) parseCast() ast.Expr {
	t := p.cur()
	e := p.parseUnary()
	if p.atWord("as") {
		p.next()
		target := p.parseTypeRef()
		e = place(p, &ast.CastExpr{ExprBase: newExprBase(), E: e, Target: target}, t)
		if p.atWord("as") {
			panic(p.errorAt(p.cur(), "E2079", "a cast of a cast: cast once, or bind the first cast to a 'let'"))
		}
	}
	return e
}

func (p *Parser) parseUnary() ast.Expr {
	t := p.cur()
	if p.accept(TMinus) {
		if p.at(TMinus) {
			panic(p.errorAt(p.cur(), diag.ErrUnexpectedToken, "'--x' is not an expression; write '-(-x)'"))
		}
		if p.at(TInt) && p.peekTok().Kind != TDot {
			lit := p.next()
			return place(p, &ast.IntLit{ExprBase: newExprBase(), Value: lit.IntValue().Neg()}, t)
		}
		return place(p, &ast.UnaryExpr{ExprBase: newExprBase(), Op: "-", E: p.parsePostfix()}, t)
	}
	if p.acceptWord("not") {
		return place(p, &ast.UnaryExpr{ExprBase: newExprBase(), Op: "not", E: p.parseUnary()}, t)
	}
	if p.atWord("async") || p.atWord("await") {
		panic(p.unsupported("'" + p.cur().Text + "'"))
	}
	if (p.atWord("freeze") || p.atWord("merge") || p.atWord("swap") || p.atWord("spawn")) && p.peekTok().Kind == TIdent {
		panic(p.unsupported("'" + p.cur().Text + "'"))
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() ast.Expr {
	e := p.parsePrimary()
	for {
		t := p.cur()
		if p.accept(TDot) {
			if p.at(TInt) {
				panic(p.unsupported("tuple element access"))
			}
			if !p.at(TIdent) {
				panic(p.unexpected("a member name"))
			}
			name := p.next().Text
			e = place(p, &ast.MemberExpr{ExprBase: newExprBase(), Obj: e, Name: name, CaseIndex: -1}, t)
		} else if p.at(TLParen) {
			e = place(p, &ast.CallExpr{ExprBase: newExprBase(), Callee: e, Args: p.parseArgs(), CaseIndex: -1}, t)
		} else {
			return e
		}
	}
}

func (p *Parser) parseArgs() []*ast.Arg {
	p.expect(TLParen, "'('")
	var args []*ast.Arg
	for !p.at(TRParen) {
		t := p.cur()
		arg := place(p, &ast.Arg{}, t)
		if p.at(TIdent) && p.peekTok().Kind == TColon {
			arg.Label = p.next().Text
			p.next()
		}
		arg.Value = p.ParseExpr()
		args = append(args, arg)
		if !p.accept(TComma) {
			break
		}
	}
	p.expect(TRParen, "')'")
	return args
}

func (p *Parser) parsePrimary() ast.Expr {
	t := p.cur()

	if p.at(TInt) {
		return place(p, &ast.IntLit{ExprBase: newExprBase(), Value: p.next().IntValue()}, t)
	}

	if p.at(TStr) {
		tok := p.next()
		lit := place(p, &ast.StrLit{ExprBase: newExprBase()}, t)
		for _, part := range tok.Parts() {
			switch part := part.(type) {
			case TextPart:
				lit.Parts = append(lit.Parts, part.Text)
			case ExprPart:
				sub := LexText(p.file(), part.Source, part.Line, part.Col)
				lit.Parts = append(lit.Parts, NewParser(sub, []string{p.file()}).parseStandaloneExpr())
			default:
				panic(diag.Internal("unknown string part %T", part))
			}
		}
		return lit
	}

	if p.atWord("true") || p.atWord("false") {
		return place(p, &ast.BoolLit{ExprBase: newExprBase(), Value: p.next().Text == "true"}, t)
	}

	if p.at(TIdent) && p.peekTok().Kind == TLBrace && (p.atWord("Self") || !ast.IsReserved(p.cur().Text)) {
		s := place(p, &ast.StructLitExpr{ExprBase: newExprBase(), TypeName: p.next().Text}, t)
		p.expect(TLBrace, "'{'")
		for !p.at(TRBrace) {
			ft := p.cur()
			name := p.expectName("a field name")
			p.expect(TColon, "':'")
			value := p.ParseExpr()
			s.Fields = append(s.Fields, ast.StructField{Name: name, Value: value, Site: place(p, &ast.Pos{}, ft)})
			if !p.accept(TComma) {
				break
			}
		}
		p.expect(TRBrace, "'}'")
		return s
	}

	if p.acceptWord("self") {
		return place(p, &ast.SelfExpr{ExprBase: newExprBase()}, t)
	}

	if p.acceptWord("Self") {
		return place(p, &ast.NameExpr{ExprBase: newExprBase(), Name: "Self"}, t)
	}

	if p.atWord("panic") {
		p.next()
		p.expect(TLParen, "'('")
		msg := p.ParseExpr()
		p.expect(TRParen, "')'")
		return place(p, &ast.PanicExpr{ExprBase: newExprBase(), Message: msg}, t)
	}

	if p.atWord("match") {
		return place(p, &ast.MatchExpr{ExprBase: newExprBase(), Match: p.parseMatchCore()}, t)
	}

	if p.accept(TLParen) {
		e := p.ParseExpr()
		if p.at(TComma) {
			panic(p.unsupported("a tuple"))
		}
		p.expect(TRParen, "')'")
		return e
	}

	if p.at(TLBracket) {
		panic(p.unsupported("an array literal"))
	}
	if p.atWord("function") {
		panic(p.unsupported("a closure"))
	}
	if p.atWord("try") {
		panic(p.unsupported("'try'"))
	}

	if p.at(TIdent) && !ast.IsReserved(p.cur().Text) {
		return place(p, &ast.NameExpr{ExprBase: newExprBase(), Name: p.next().Text}, t)
	}

	panic(p.unexpected("an expression"))
}
