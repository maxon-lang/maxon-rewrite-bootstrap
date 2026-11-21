---
feature: max
status: stable
keywords: max, maximum, math
category: math-intrinsic
---
# max

## Documentation

Returns the larger of two floating-point values.

**Signature:** `max(a float, b float) float`

**Parameters:**
- `a` - First value to compare
- `b` - Second value to compare

**Returns:** The larger of the two input values

**Example:**

```maxon
let x = max(3.0, 5.0)    // 5.0
let y = max(10.0, 2.5)   // 10.0
let z = max(-1.0, 1.0)   // 1.0
```

**Notes:**
- For integer inputs, values are automatically promoted to float
- If both values are equal, returns that value
- Works with negative numbers

## Tests

<!-- test: max.basic -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(3.0, 5.0)
	return trunc(x)
end 'main'
```
```exitcode
5
```

<!-- test: max.first-larger -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(10.0, 2.0)
	return trunc(x)
end 'main'
```
```exitcode
10
```

<!-- test: max.negative -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(-5.0, 3.0)
	return trunc(x)
end 'main'
```
```exitcode
3
```

<!-- test: max.both-negative -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(-2.0, -8.0)
	print("{trunc(x)}\n")
	return 0
end 'main'
```
```stdout
-2
```

<!-- test: max.equal-values -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(7.0, 7.0)
	return trunc(x)
end 'main'
```
```exitcode
7
```

<!-- test: max.fractional -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(3.5, 5.2)
	// max of 3.5 and 5.2 is 5.2, trunc gives 5
	return trunc(x)
end 'main'
```
```exitcode
5
```

<!-- test: max.zero -->
<!-- skip: float literals are not supported in minimaxon -->
```maxon
function main() returns ExitCode
	// TODO: float literals are not supported in minimaxon
	let x = max(0.0, -5.0)
	// TODO: float literals are not supported in minimaxon
	if x == 0.0 'check'
		return 0
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```

<!-- test: max.rt-basic -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 3.0 5.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	return trunc(max(a, b))
end 'main'
```
```exitcode
5
```

<!-- test: max.rt-first-larger -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 10.0 2.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	return trunc(max(a, b))
end 'main'
```
```exitcode
10
```

<!-- test: max.rt-negative -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: -5.0 3.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	return trunc(max(a, b))
end 'main'
```
```exitcode
3
```

<!-- test: max.rt-both-negative -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: -2.0 -8.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	return trunc(max(a, b)) + 10
end 'main'
```
```exitcode
8
```

<!-- test: max.rt-equal -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 7.0 7.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	return trunc(max(a, b))
end 'main'
```
```exitcode
7
```

<!-- test: max.rt-fractional -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 3.5 5.2 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	return trunc(max(a, b))
end 'main'
```
```exitcode
5
```

<!-- test: max.rt-zero -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- Args: 0.0 -5.0 -->
```maxon
function main() returns ExitCode
	let args = CommandLine.args()
	// TODO: float literals are not supported in minimaxon
	let a = try float.fromString(try args.get(1) otherwise "") otherwise 0.0
	// TODO: float literals are not supported in minimaxon
	let b = try float.fromString(try args.get(2) otherwise "") otherwise 0.0
	let result = max(a, b)
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
