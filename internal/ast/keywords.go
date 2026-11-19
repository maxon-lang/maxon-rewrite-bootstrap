package ast

var reservedWords = map[string]bool{}

func init() {
	for _, w := range []string{
		"and", "as", "async", "await", "bool", "break", "continue", "countof", "cstring", "default", "else", "end",
		"enum", "export", "extends", "extension", "fallthrough", "false", "float", "for", "from", "function", "gives",
		"if", "ignore", "implements", "in", "int", "interface", "is", "let", "match", "mod", "not", "or", "otherwise",
		"panic", "public", "return", "returns", "self", "Self", "shl", "shr", "sizeof", "static", "then", "throw",
		"throws", "to", "true", "try", "type", "typealias", "union", "upto", "uses", "var", "where", "while", "with",
		"xor",
	} {
		reservedWords[w] = true
	}
}

func IsReserved(word string) bool { return reservedWords[word] }
