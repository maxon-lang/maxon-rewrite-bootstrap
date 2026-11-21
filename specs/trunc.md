---
feature: trunc
status: stable
keywords: trunc, truncate, rounding, math, conversion
category: math-intrinsic
---
# trunc

## Documentation

Truncate a floating-point number to an integer (toward zero).

**Signature:** `trunc(x float) int`

**Parameters:**
- `x` - The floating-point number to truncate

**Returns:** The integer part of x (fractional part removed)

**Example:**

```maxon
let x = 3.7
let y = trunc(x)     // 3

let neg = -3.7
let z = trunc(neg)   // -3 (toward zero, removes fractional part)
```
**Notes:**
- Truncates toward zero (removes decimal part)
- Different from floor/ceil for negative numbers
- `trunc(3.9)` returns `3`
- `trunc(-3.9)` returns `-3` (not `-4`)
- Equivalent to casting to int in many languages

## Tests

<!-- test: trunc.basic -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let neg = -3.7
	let y = trunc(neg)
	return y + 10
end 'main'
```
```exitcode
7
```

<!-- test: trunc.positive -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = 7.9
	let y = trunc(x)
	return y
end 'main'
```
```exitcode
7
```

<!-- test: trunc.zero -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = 0.5
	let y = trunc(x)
	return y
end 'main'
```
```exitcode
0
```

<!-- test: trunc.rt-negative -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: -3.7 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let x = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	return trunc(x) + 10
end 'main'
```
```exitcode
7
```

<!-- test: trunc.rt-positive -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 7.9 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let x = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	return trunc(x)
end 'main'
```
```exitcode
7
```

<!-- test: trunc.rt-zero -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 0.5 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let x = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	return trunc(x)
end 'main'
```
```exitcode
0
```

<!-- test: error.trunc-is-a-full-range-int -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- positions: moved -->
`trunc` produces a `FullRangeInt`, which a parameter of another alias refuses.
```maxon
typealias Score = int(0 to u64.max)

function show(v Score)
	print("{v}\n")
end 'show'

function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	show(trunc(2.5))
	return 0
end 'main'
```
```maxoncstderr
error E3005: <fragment>:9:2: argument type mismatch for 'v': expected 'Score', got 'FullRangeInt' — cast the argument with 'as Score'
```
