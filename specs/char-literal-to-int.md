---
feature: char-literal-to-int
status: experimental
keywords: [character, literal, int, coercion, codepoint]
category: type-system
---

# Character Literal to Integer Coercion

## Documentation

When a character literal appears in a binary operation where the other operand is an integer type (int, byte, short), the compiler automatically converts the character literal to its Unicode codepoint value at compile time.

### Comparison

```maxon
for cp in "hello-world".codepoints() 'chars'
	if cp == '-' 'dash'
		print("found dash (cp={cp})\n")
	end 'dash'
end 'chars'
```

### Arithmetic

```maxon
let digit = 53  // codepoint for '5'
let value = digit - '0'  // '0' coerced to 48, result is 5
```

## Tests

<!-- test: char-literal-eq-codepoint -->
<!-- skip: character literals are not supported in minimaxon -->
### Compare codepoint with character literal using ==

```maxon
function main() returns ExitCode
	let cp = 45
	// TODO: character literals are not supported in minimaxon
	if cp == '-' 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: char-literal-ne-codepoint -->
<!-- skip: character literals are not supported in minimaxon -->
### Compare codepoint with character literal using !=

```maxon
function main() returns ExitCode
	let cp = 45
	// TODO: character literals are not supported in minimaxon
	if cp != '.' 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: char-literal-ge-le-codepoint -->
<!-- skip: character literals are not supported in minimaxon -->
### Compare codepoint with character literal using >= and <=

```maxon
function main() returns ExitCode
	let cp = 53
	// TODO: character literals are not supported in minimaxon
	if cp >= '0' 'ge'
		// TODO: character literals are not supported in minimaxon
		if cp <= '9' 'le'
			return 0
		end 'le'
	end 'ge'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: char-literal-arithmetic -->
<!-- skip: character literals are not supported in minimaxon -->
### Subtract character literal from codepoint

```maxon
function main() returns ExitCode
	let cp = 53
	// TODO: character literals are not supported in minimaxon
	let digit = cp - '0'
	return digit
end 'main'
```
```exitcode
5
```

<!-- test: char-literal-escape-coercion -->
<!-- skip: character literals are not supported in minimaxon -->
### Escape sequence character literal coerced to int

```maxon
function main() returns ExitCode
	let cp = 10
	// TODO: character literals are not supported in minimaxon
	if cp == '\n' 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: char-literal-lhs-coercion -->
<!-- skip: character literals are not supported in minimaxon -->
### Character literal on left-hand side of comparison

```maxon
function main() returns ExitCode
	let cp = 45
	// TODO: character literals are not supported in minimaxon
	if '-' == cp 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: char-literal-both-sides-still-character -->
<!-- skip: character literals are not supported in minimaxon -->
### Two character literals compared stay as Character

```maxon
function main() returns ExitCode
	// TODO: character literals are not supported in minimaxon
	if 'A' == 'A' 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: char-literal-codepoint-iteration -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- skip: iterator-based for loops are not supported in minimaxon; use 'for i in a to|upto b' -->
### Character literal comparison during codepoint iteration

```maxon
function main() returns ExitCode
	var count = 0 as ExitCode
	// TODO: iterator-based for loops are not supported in minimaxon; use 'for i in a to|upto b'
	for cp in "a-b-c".codepoints() 'chars'
		// TODO: character literals are not supported in minimaxon
		if cp == '-' 'dash'
			count = count + 1
		end 'dash'
	end 'chars'
	return count
end 'main'
```
```exitcode
2
```

<!-- test: char-literal-coerces-across-a-closure-body -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- skip: closures are not supported in minimaxon -->
<!-- skip: function typealiases are not supported in minimaxon -->
### A character literal parsed before a closure still converts after it

⛔⛔ **THE CLOSURE-BODY CONTEXT SWAP MUST SAVE AND RESTORE A PER-FUNCTION COLUMN.** `integerizedOperand`
rewrites a character literal's already-emitted `stringLiteral` op IN PLACE, and finds that op through
`Parser.charLiteralOps` — a per-function map keyed by `ValueId`. `parseBinary` reads the LEFT operand's
token span only once the RIGHT operand is in hand, so a closure in the right operand is parsed BETWEEN the
left literal's emit and its conversion. A swap that reset that map without restoring it gives two wrong
answers on one line:

  • `'0' + apply(function(n Integer) gives n + 1, x: 1)` finds no entry and **PANICS the compiler**.
  • `'A' + apply(function(n Integer) gives n + '0', x: 1)` is worse, because a closure's value ids are a
    FRESH SSA space that COLLIDES with the enclosing function's: the lookup HITS the closure's entry,
    rewrites the CLOSURE's op, and leaves the outer literal a `stringLiteral`, printing the record's
    `.rdata` ADDRESS added as a number where 114 is correct, silently.

The three answers below are `a=50 b=114 c=51`. The third case pins the other
direction — a literal AFTER the closure — so a fix that saved the map without restoring it would still fail.

```maxon

typealias Integer = int(i64.min to i64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Fn1 = function(Integer) returns Integer

function apply(f Fn1, x Integer) returns Integer
	return f(x)
end 'apply'

function main() returns ExitCode
	// TODO: character literals are not supported in minimaxon
	// TODO: closures are not supported in minimaxon
	let a = '0' + apply(function(n Integer) gives n + 1, x: 1)
	let p0 = 0
	let p1 = 1
	// TODO: character literals are not supported in minimaxon
	// TODO: closures are not supported in minimaxon
	let b = 'A' + apply(function(n Integer) gives n + '0', x: 1)
	// TODO: closures are not supported in minimaxon
	// TODO: character literals are not supported in minimaxon
	let c = apply(function(n Integer) gives n + 2, x: 1) + '0'
	print("a={a} b={b} c={c} pad={p0}{p1}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
a=50 b=114 c=51 pad=01
```

<!-- test: error.a-codepoint-is-a-codepoint -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- positions: moved -->
A character literal used in integer arithmetic produces a `Codepoint`, which a parameter of another alias refuses.
```maxon
typealias Score = int(0 to u64.max)

function show(v Score)
	print("{v}\n")
end 'show'

function main() returns ExitCode
	// TODO: character literals are not supported in minimaxon
	show('a' + 1)
	return 0
end 'main'
```
```maxoncstderr
error E3005: <fragment>:9:2: argument type mismatch for 'v': expected 'Score', got 'Codepoint' — cast the argument with 'as Score'
```
