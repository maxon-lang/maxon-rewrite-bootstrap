---
feature: unicode-category
status: experimental
keywords: [unicode, character-set, general-category, letters, digits, punctuation, symbols]
category: types
---

# Unicode General Category

## Documentation

CharacterSet predefined sets use Unicode General Categories for character classification rather than ASCII-only ranges. This means `CharacterSet.letters` includes all Unicode letters (Cyrillic, CJK, Arabic, etc.), `CharacterSet.decimalDigits` includes all Unicode decimal digits, and so on.

The `unicodeGeneralCategory(cp)` function returns the Unicode General Category for any codepoint, using a page-based dispatch lookup table generated from the Unicode Character Database (DerivedGeneralCategory-16.0.0.txt).

### Category Mappings

| CharacterSet accessor | Unicode Categories |
|----------------------|-------------------|
| `letters()` | Lu, Ll, Lt, Lm, Lo, Mn, Mc, Me |
| `lowercaseLetters()` | Ll |
| `uppercaseLetters()` | Lu, Lt |
| `decimalDigits()` | Nd |
| `alphanumerics()` | Lu, Ll, Lt, Lm, Lo, Mn, Mc, Me, Nd, Nl, No |
| `punctuation()` | Pc, Pd, Pe, Pf, Pi, Po, Ps |
| `symbols()` | Sc, Sk, Sm, So |
| `controlCharacters()` | Cc, Cf |
| `whitespaces()` | Zs (+ HT) |
| `whitespacesAndNewlines()` | Zs, Zl, Zp (+ HT, LF, VT, FF, CR, NEL) |
| `newlines()` | Zl, Zp (+ LF, VT, FF, CR, NEL) |

## Tests

<!-- test: cyrillic-letters -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let letters = CharacterSet.letters
	let a = '\u0410'
	let b = '\u0430'
	print("{letters.contains(a)}\n{letters.contains(b)}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
true
```

<!-- test: cjk-letters -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let letters = CharacterSet.letters
	let c = '\u4E00'
	print("{letters.contains(c)}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
```

<!-- test: arabic-digits -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let digits = CharacterSet.decimalDigits
	let d = '\u0660'
	print("{digits.contains(d)}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
```

<!-- test: unicode-punctuation -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let punct = CharacterSet.punctuation
	let a = '\u00AB'
	let b = '\u00BB'
	print("{punct.contains(a)}\n{punct.contains(b)}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
true
```

<!-- test: unicode-symbols -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let syms = CharacterSet.symbols
	let c = '\u00A9'
	print("{syms.contains(c)}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
```

<!-- test: custom-set-unchanged -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let cs = CharacterSet.from(CharSet from ['x', 'y', 'z'])
	print("{cs.contains('x')}\n{cs.contains('a')}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
false
```

<!-- test: trim-cyrillic -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
<!-- skip: a trim of a CharacterSet is not supported in minimaxon: trim, trimStart and trimEnd take no set and remove whitespace and newlines -->
```maxon
function main() returns ExitCode
	let s = "\u0410\u0411\u0412 123 \u0410\u0411\u0412"
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
[ 123 ]
```

<!-- test: unicode-whitespace -->
<!-- skip: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate -->
```maxon
function main() returns ExitCode
	// TODO: CharacterSet is not supported in minimaxon yet: its predefined sets are static lets of the library whose Set literal is built where they are read, which the reading module does not instantiate
	let ws = CharacterSet.whitespaces
	let c = '\u2003'
	print("{ws.contains(c)}")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
true
```
