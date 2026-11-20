package syntax

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// initialTokenCapacity is how many tokens a scratch's buffer holds to start with, which is more than most modules need.
const initialTokenCapacity = 1 << 16

// Scratch is the working memory of reading, lexing and parsing a module that a worker reuses for each module it takes,
// so once its buffers have grown a module allocates only what it keeps: its source, its line index and its AST.
type Scratch struct {
	// file is where a file is read into before it is copied into the module's source.
	file   []byte
	tokens []Token
	lists  listStacks
	decls  listStack[ast.Decl]
}

func NewScratch() *Scratch { return &Scratch{tokens: make([]Token, 0, initialTokenCapacity)} }

// Lex lexes src into the scratch's token buffer, which holds the tokens until the next call.
func (s *Scratch) Lex(src *Source) []Token {
	s.tokens = Lex(src, s.tokens)
	return s.tokens
}

// Parse parses the tokens of src, the text of the module with handle module, and gives the module's declarations and
// the syntax errors found.
func (s *Scratch) Parse(src *Source, tokens []Token, module ast.ModuleID) (*ast.Program, []*diag.CompileError) {
	p := newParser(src, tokens, s, module)
	return p.ParseProgram(), p.Errors
}
