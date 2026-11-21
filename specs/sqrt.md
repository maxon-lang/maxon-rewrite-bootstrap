---
feature: sqrt
status: stable
keywords: sqrt, square root, math
category: math-intrinsic
---
# sqrt

## Documentation

Calculate the square root of a number.

**Signature:** `sqrt(x float) float`

**Parameters:**
- `x` - The number to take the square root of (must be non-negative)

**Returns:** The square root of the input

**Example:**

```maxon
let x = 16.0
let y = sqrt(x)      // 4.0

let z = 2.0
let w = sqrt(z)      // 1.414213... (√2)
```
**Notes:**
- Input must be non-negative (undefined behavior for negative values)
- `sqrt(0.0)` returns `0.0`
- `sqrt(1.0)` returns `1.0`
- Use `trunc(sqrt(x))` to get an integer result

## Tests

<!-- test: sqrt.basic -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = sqrt(16.0)
	return trunc(x)
end 'main'
```
```exitcode
4
```

<!-- test: sqrt.precision -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = sqrt(2.0)
	// sqrt(2) * sqrt(2) should be approximately 2
	let check = x * x
	// Trunc to get integer 2
	return trunc(check)
end 'main'
```
```exitcode
2
```

<!-- test: sqrt.zero -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let result = sqrt(0.0)
	// TODO: float literals are not supported in minimaxon
	if result == 0.0 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: sqrt.rt-basic -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 16.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let x = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	return trunc(sqrt(x))
end 'main'
```
```exitcode
4
```

<!-- test: sqrt.rt-precision -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 2.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let x = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	let check = sqrt(x) * sqrt(x)
	return trunc(check)
end 'main'
```
```exitcode
2
```

<!-- test: sqrt.rt-zero -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 0.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let x = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	let result = sqrt(x)
	// TODO: float literals are not supported in minimaxon
	if result == 0.0 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```
