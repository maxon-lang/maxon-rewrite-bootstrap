---
feature: self-assignment
status: stable
keywords: [assignment, diagnostics, errors]
category: diagnostics
---

# Self-Assignment Detection

## Documentation

Maxon detects self-assignment — assigning a variable to itself — and reports it as a compile error, since it has no effect and is likely a bug. Similarly, `_ =` requires a function call on the right-hand side; using it with any other expression is an error.

### Example Errors

```maxon
function main() returns ExitCode
	var x = 42
	x = x  // Error: self-assignment has no effect
	return 0
end 'main'
```
```maxoncstderr
error E3067: specs/self-assignment/docs-example-1.maxon:4:2: self-assignment has no effect: 'x = x'
```

```maxon
function main() returns ExitCode
	_ = 0  // Error: expected a function call
	return 0
end 'main'
```
```maxoncstderr
error E3067: specs/self-assignment/docs-example-2.maxon:3:2: expected a function call
```

## Tests

<!-- test: self-assignment.basic -->
<!-- skip: language difference: E3067 refuses only a discard here, of any value that no call, send, await or panic made, a bare name too, where maxon's also refuses a self-assignment and admits a bare name, a let _ and a value it constructs (README, Where it differs from Maxon: A discard takes a value something made) -->
```maxon

function main() returns ExitCode
	var x = 10 as FullRangeInt
	x = x
	return 0
end 'main'
```
```maxoncstderr
error E3067: specs/self-assignment/self-assignment.basic.maxon:5:2: self-assignment has no effect: 'x = x'
```

<!-- test: self-assignment.different-var -->
```maxon

function main() returns ExitCode
	var x = 10 as FullRangeInt
	let y = 20
	x = y
	return x
end 'main'
```
```exitcode
20
```

<!-- test: self-assignment.expr-not-flagged -->
```maxon

function main() returns ExitCode
	var x = 10 as FullRangeInt
	x = x + 1
	return x
end 'main'
```
```exitcode
11
```

<!-- test: self-assignment.field-self-assign -->
<!-- skip: language difference: E3067 refuses only a discard here, of any value that no call, send, await or panic made, a bare name too, where maxon's also refuses a self-assignment and admits a bare name, a let _ and a value it constructs (README, Where it differs from Maxon: A discard takes a value something made) -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Point
	export var x as Integer
	export var y as Integer

	static function create(x Integer, y Integer) returns Self
		return Self{x: x, y: y}
	end 'create'
end 'Point'

function main() returns ExitCode
	var p = Point.create(1, y: 2)
	p.x = p.x
	return 0
end 'main'
```
```maxoncstderr
error E3067: specs/self-assignment/self-assignment.field-self-assign.maxon:16:2: self-assignment has no effect: 'p.x = p.x'
```

<!-- test: self-assignment.discard-literal -->
<!-- skip: language difference: E3067 refuses only a discard here, of any value that no call, send, await or panic made, a bare name too, where maxon's also refuses a self-assignment and admits a bare name, a let _ and a value it constructs (README, Where it differs from Maxon: A discard takes a value something made) -->
<!-- positions: moved -->
```maxon

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E3067: specs/self-assignment/self-assignment.discard-literal.maxon:4:2: expected a function call
```
