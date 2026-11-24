---
feature: string-trim
status: experimental
keywords: [string, trim, whitespace, character-set]
category: types
---

# String Trim

## Documentation

The `trim`, `trimStart`, and `trimEnd` methods remove characters from the edges of a string. They accept a `CharacterSet` to specify which characters to remove.

**Signatures:**

```text
trim(CharacterSet) returns String       // both ends
trimStart(CharacterSet) returns String   // start only
trimEnd(CharacterSet) returns String     // end only
trim() returns String                        // whitespace, both ends
trimStart() returns String                   // whitespace, start only
trimEnd() returns String                     // whitespace, end only
```

**Examples:**

```text
"  hello  ".trim()                              // "hello"
"  hello  ".trimStart()                         // "hello  "
"  hello  ".trimEnd()                           // "  hello"
"123hello456".trim(CharacterSet.decimalDigits)  // "hello"
"...hello!!!".trim(CharacterSet.punctuation)    // "hello"
```

The no-argument versions are convenience methods that trim Unicode whitespace.

### CharacterSet

`CharacterSet` provides predefined character sets and supports custom sets:

```text
CharacterSet.whitespacesAndNewlines  // All Unicode whitespace including newlines
CharacterSet.whitespaces             // Spaces and tabs (no newlines)
CharacterSet.newlines                // Newline characters only
CharacterSet.decimalDigits           // 0-9
CharacterSet.letters                 // a-z, A-Z
CharacterSet.lowercaseLetters        // a-z
CharacterSet.uppercaseLetters        // A-Z
CharacterSet.alphanumerics           // a-z, A-Z, 0-9
CharacterSet.punctuation             // Punctuation characters
CharacterSet.symbols                 // Symbol characters
CharacterSet.controlCharacters       // Control characters
```

## Tests

<!-- test: trim-whitespace-both -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "  hello  "
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let ws = CharacterSet.whitespacesAndNewlines
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	let result = s.trim(ws)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-whitespace-start -->
```maxon
function main() returns ExitCode
	let s = "  hello  "
	let result = s.trimStart()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello  ]
```

<!-- test: trim-whitespace-end -->
```maxon
function main() returns ExitCode
	let s = "  hello  "
	let result = s.trimEnd()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[  hello]
```

<!-- test: trim-tabs-and-newlines -->
```maxon
function main() returns ExitCode
	let s = "\t\nhello\r\n"
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-all-whitespace -->
```maxon
function main() returns ExitCode
	let s = "   "
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[]
```

<!-- test: trim-no-whitespace -->
```maxon
function main() returns ExitCode
	let s = "hello"
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-empty-string -->
```maxon
function main() returns ExitCode
	let s = ""
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[]
```

<!-- test: trim-mixed-whitespace -->
```maxon
function main() returns ExitCode
	let s = " \t\r\n hello \t\r\n "
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-internal-preserved -->
```maxon
function main() returns ExitCode
	let s = "  hello world  "
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello world]
```

<!-- test: trim-charset-whitespace -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "  hello  "
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.whitespacesAndNewlines)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-charset-digits -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "123hello456"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.decimalDigits)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-charset-letters -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "abc123abc"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.letters)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[123]
```

<!-- test: trim-charset-digits-start -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "123hello"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trimStart(CharacterSet.decimalDigits)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-charset-digits-end -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "hello123"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trimEnd(CharacterSet.decimalDigits)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-charset-no-match -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "hello"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.decimalDigits)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-charset-all-match -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "12345"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.decimalDigits)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[]
```

<!-- test: trim-charset-empty -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = ""
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.whitespacesAndNewlines)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[]
```

<!-- test: trim-charset-punctuation -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "...hello!!!"
	// TODO: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let result = s.trim(CharacterSet.punctuation)
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello]
```

<!-- test: trim-end-emoji -->
```maxon
function main() returns ExitCode
	let s = "hello 🎉  "
	let result = s.trimEnd()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[hello 🎉]
```

<!-- test: trim-end-only-multibyte -->
```maxon
function main() returns ExitCode
	let s = "中文日本語"
	let result = s.trimEnd()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[中文日本語]
```

<!-- test: trim-end-combining-mark -->
```maxon
function main() returns ExitCode
	let s = "café  "
	let result = s.trimEnd()
	print("{result.count()}\n")
	print("{result.byteLength()}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
4
5
```

<!-- test: trim-both-multibyte -->
```maxon
function main() returns ExitCode
	let s = "  中文  "
	let result = s.trim()
	print("[{result}]")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
[中文]
```
