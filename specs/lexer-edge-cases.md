---
feature: lexer-edge-cases
status: stable
keywords: [lexer, cursor, eof, edge-case]
category: diagnostics
---

# Lexer Edge Cases

## Documentation

Tests for lexer behavior at source boundaries: empty source, tokens at EOF, unterminated strings and comments, and special characters.

## Tests

### Empty and minimal source

<!-- test: single-char-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

x
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/single-char-eof.maxon:6:1: Expected function declaration, got 'x'
```

<!-- test: whitespace-only -->
```maxon
   
```
```maxoncstderr
error E3001: No 'main' function found
```

### Unterminated strings

<!-- test: unterminated-squote-eof -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- positions: moved -->
```maxon
// TODO: character literals are not supported in minimaxon
let x = 'hello
```
```maxoncstderr
error E1002: specs/lexer-edge-cases/unterminated-squote-eof.maxon:2:9: Unterminated string literal
```

<!-- test: unterminated-dquote-eof -->
<!-- skip: minimaxon does not parse the converted source: unterminated string literal -->
```maxon
let x = "hello
```
```maxoncstderr
error E1002: specs/lexer-edge-cases/unterminated-dquote-eof.maxon:2:9: Unterminated string literal
```

<!-- test: string-with-newline -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- positions: moved -->
```maxon
// TODO: character literals are not supported in minimaxon
let x = 'hel

// TODO: character literals are not supported in minimaxon
lo '
```
```maxoncstderr
error E3001: No 'main' function found
```

<!-- test: string-with-newline-in-a-body -->
<!-- skip: character literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: character literals are not supported in minimaxon
	let x = 'hel
// TODO: character literals are not supported in minimaxon
lo '
	print("[{x}]\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hel
lo]
```

<!-- test: unterminated-escape-eof -->
<!-- skip: minimaxon does not parse the converted source: unknown escape sequence '\
' -->
```maxon
let x = "hello\
```
```maxoncstderr
error E1002: specs/lexer-edge-cases/unterminated-escape-eof.maxon:2:9: Unterminated string literal
```

<!-- test: empty-squote-string -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- positions: moved -->
```maxon
// TODO: character literals are not supported in minimaxon
let x = ''

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2016: specs/lexer-edge-cases/empty-squote-string.maxon:2:9: invalid character literal: a character literal must contain exactly one character
```

<!-- test: empty-dquote-string -->
```maxon
let x = ""
```
```maxoncstderr
error E3001: No 'main' function found
```

### Numbers at EOF

<!-- test: integer-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

42
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/integer-eof.maxon:6:1: Expected function declaration, got '42'
```

<!-- test: hex-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

0xFF
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/hex-eof.maxon:6:1: Expected function declaration, got '0xFF'
```

<!-- test: binary-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

0b1010
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/binary-eof.maxon:6:1: Expected function declaration, got '0b1010'
```

<!-- test: octal-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

0o77
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/octal-eof.maxon:6:1: Expected function declaration, got '0o77'
```

<!-- test: float-eof -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- positions: moved -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

// TODO: float literals are not supported in minimaxon
3.14
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/float-eof.maxon:6:1: Expected function declaration, got '3.14'
```

<!-- test: float-exponent-eof -->
<!-- positions: moved -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

1 e10
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/float-exponent-eof.maxon:6:1: Expected function declaration, got '1'
```

<!-- test: number-underscore-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

1_000
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/number-underscore-eof.maxon:6:1: Expected function declaration, got '1_000'
```

<!-- test: bare-hex-prefix-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

0x
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/bare-hex-prefix-eof.maxon:6:1: Expected function declaration, got '0x'
```

### Comments at EOF

<!-- test: line-comment-eof -->
```maxon
// comment
```
```maxoncstderr
error E3001: No 'main' function found
```

<!-- test: unterminated-block-comment -->
<!-- positions: moved -->
```maxon
// no close
//
```
```maxoncstderr
error E1007: specs/lexer-edge-cases/unterminated-block-comment.maxon:2:1: Unterminated block comment
```

<!-- test: block-comment-multiline -->
<!-- positions: moved -->
```maxon
// a
// b
```
```maxoncstderr
error E3001: No 'main' function found
```

<!-- test: empty-block-comment -->
<!-- positions: moved -->
```maxon
//
```
```maxoncstderr
error E3001: No 'main' function found
```

### Operators at EOF

<!-- test: two-char-operator-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

==
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/two-char-operator-eof.maxon:6:1: Expected function declaration, got '=='
```

<!-- test: single-equals-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

=
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/single-equals-eof.maxon:6:1: Expected function declaration, got '='
```

<!-- test: single-bang-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

!
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/single-bang-eof.maxon:6:1: Expected function declaration, got '!'
```

<!-- test: single-slash-eof -->
```maxon
function main() returns ExitCode
	return 0
end 'main'

/
```
```maxoncstderr
error E2001: specs/lexer-edge-cases/single-slash-eof.maxon:6:1: Expected function declaration, got '/'
```

### Line endings

<!-- test: crlf-handling -->
<!-- positions: moved -->
```maxon
let x = 1

let y = 2
```
```maxoncstderr
error E3001: No 'main' function found
```

<!-- test: bare-cr-handling -->
<!-- positions: moved -->
```maxon
let x = 1

let y = 2
```
```maxoncstderr
error E3001: No 'main' function found
```

### Stray characters

<!-- test: stray-question-mark-after-type -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '?' -->
<!-- positions: moved -->
```maxon
function f(a Count ?, b Count)
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-question-mark-after-type.maxon:2:19: Unexpected character '?'
```

<!-- test: stray-question-mark-after-expression -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '?' -->
```maxon
let x = 5 ?
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-question-mark-after-expression.maxon:2:11: Unexpected character '?'
```

<!-- test: stray-question-mark-before-more-source -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '?' -->
<!-- positions: moved -->
```maxon
let x = 5 ?

let y = 6
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-question-mark-before-more-source.maxon:2:11: Unexpected character '?'
```

<!-- test: stray-question-mark-in-interpolation -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '?' -->
```maxon
let s = "n{5 ?}"
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-question-mark-in-interpolation.maxon:2:14: Unexpected character '?'
```

<!-- test: stray-question-mark-in-function-body -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '?' -->
<!-- positions: moved -->
```maxon
function main() returns ExitCode
	let x = 1 ?
	return x
end 'main'
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-question-mark-in-function-body.maxon:3:11: Unexpected character '?'
```

<!-- test: stray-semicolon -->
<!-- skip: minimaxon does not parse the converted source: unexpected character ';' -->
<!-- positions: moved -->
```maxon
let x = 1 ;
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-semicolon.maxon:2:10: Unexpected character ';'
```

<!-- test: stray-pipe -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '|' -->
```maxon
let x = a | b
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-pipe.maxon:2:11: Unexpected character '|'
```

<!-- test: stray-ampersand -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '&' -->
<!-- positions: moved -->
```maxon
let x = a & & b
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-ampersand.maxon:2:11: Unexpected character '&'
```

<!-- test: stray-dollar -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '$' -->
<!-- positions: moved -->
```maxon
let x = $ y
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-dollar.maxon:2:9: Unexpected character '$'
```

<!-- test: stray-percent -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '%' -->
```maxon
let x = 7 % 2
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-percent.maxon:2:11: Unexpected character '%'
```

<!-- test: stray-tilde -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '~' -->
<!-- positions: moved -->
```maxon
let x = ~ 1
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-tilde.maxon:2:9: Unexpected character '~'
```

<!-- test: stray-caret -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '^' -->
```maxon
let x = 1 ^ 2
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-caret.maxon:2:11: Unexpected character '^'
```

<!-- test: stray-backtick -->
<!-- skip: minimaxon does not parse the converted source: unexpected character '`' -->
<!-- positions: moved -->
```maxon
let x = ` y `
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-backtick.maxon:2:9: Unexpected character '`'
```

<!-- test: stray-non-ascii-letter -->
<!-- skip: minimaxon does not parse the converted source: unexpected character 'ÔøΩ' -->
<!-- positions: moved -->
```maxon
let x = √ ©
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-non-ascii-letter.maxon:2:9: Unexpected character '√©'
```

<!-- test: stray-curly-quote -->
<!-- skip: minimaxon does not parse the converted source: unexpected character 'ÔøΩ' -->
<!-- positions: moved -->
```maxon
let x = ‚ Ä ú hello ‚ Ä ù
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-curly-quote.maxon:2:9: Unexpected character '‚Äú'
```

<!-- test: question-mark-is-text-in-strings-and-comments -->
<!-- skip: character literals are not supported in minimaxon -->
```maxon
// is this a comment?
// or a block comment?
function main() returns ExitCode
	let q = "why?"
	// TODO: character literals are not supported in minimaxon
	let c = '?'
	print("{q}{c}\n")
	return 0
end 'main'
```
```stdout
why??
```
```exitcode
0
```

<!-- test: byte-order-mark-at-start-is-accepted -->
```maxon
// --- file: main.maxon
function main() returns ExitCode
	return 0
end 'main'
```
```exitcode
0
```

<!-- test: stray-byte-order-mark-mid-file -->
<!-- skip: minimaxon does not parse the converted source: unexpected character 'ÔøΩ' -->
<!-- positions: moved -->
```maxon
function main() returns ExitCode
	Ô ª ø return 0
end 'main'
```
```maxoncstderr
error E1010: specs/lexer-edge-cases/stray-byte-order-mark-mid-file.maxon:3:2: Unexpected character 'Ôªø'
```
