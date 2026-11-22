---
feature: unused-parameters
status: selfhosted
keywords: [parameters, warnings, errors, unused]
category: diagnostics
---

# Unused Parameter Detection

## Documentation

Maxon requires all function parameters to be used. Declaring unused parameters causes a compilation error.

### Example Error

```maxon
typealias Score = int(i64.min to i64.max)

function add(a Score, b Score) returns Score
	return a  // Error: 'b' is unused
end 'add'
```
Error message:
```
Semantic Error: The parameter 'b' is declared but its value is never used
```

### Solution

Only declare parameters you need:

```maxon
typealias Score = int(i64.min to i64.max)

function identity(a Score) returns Score
	return a  // OK: 'a' is used
end 'identity'
```

### Interface Method Exception

Methods that implement an interface are exempt from this check (see
`interface-conformance` spec). The implementer is forced to declare every
parameter the contract names, even when a particular implementation does
not need one of them. The check still applies to non-interface methods on
the same type and to local `var`/`let` bindings inside interface methods.
## Tests

<!-- test: single-unused -->
<!-- skip: language difference: style and lint rules are not checked, since the generated code does not depend on them -->
```maxon

typealias Integer = int(i64.min to i64.max)

function add(a Integer, b Integer) returns Integer
	return a
end 'add'

function main() returns ExitCode
	return add(5, b: 10)
end 'main'
```
```maxoncstderr
error E3012: specs/unused-parameters/single-unused.maxon:5:25: unused variable: 'b'
```

<!-- test: multiple-unused -->
<!-- skip: language difference: style and lint rules are not checked, since the generated code does not depend on them -->
```maxon

typealias Integer = int(i64.min to i64.max)

function test(a Integer, b Integer, c Integer) returns Integer
	return a
end 'test'

function main() returns ExitCode
	return test(1, b: 2, c: 3)
end 'main'
```
```maxoncstderr
error E3012: specs/unused-parameters/multiple-unused.maxon:5:26: unused variable: 'b'
error E3012: specs/unused-parameters/multiple-unused.maxon:5:37: unused variable: 'c'
```

<!-- test: all-used-ok -->
```maxon

typealias Integer = int(i64.min to i64.max)

function add(a Integer, b Integer) returns Integer
	return try a + b otherwise panic("out of range")
end 'add'

function main() returns ExitCode
	return try add(5, b: 10) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
15
```


<!-- test: none-unused -->
```maxon

typealias Integer = int(i64.min to i64.max)

function multiply(a Integer, b Integer) returns Integer
	return try a * b otherwise panic("out of range")
end 'multiply'

function main() returns ExitCode
	return try multiply(7, b: 6) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```


<!-- test: void-function-unused -->
⚠ **AN UNUSED NAME IS REPORTED ONLY WHEN NOTHING ELSE IS.** The unnamed second argument is `E2053`, so `main`
is abandoned, and the unused-name reports — held back while any other diagnostic stands — say nothing, not even
for `doNothing`, which is read in full. **The unused-parameter behaviour this case is named for is pinned by the
other cases in this file** — `single-unused`, `multiple-unused` and `method-on-non-conforming-type-still-errors` —
so this case pins only that the parse error is the one diagnostic.
```maxon

typealias Integer = int(i64.min to i64.max)

function doNothing(x Integer, y Integer)
	let z = 42
end 'doNothing'

function main() returns ExitCode
	doNothing(1, 2)
	return 0
end 'main'
```
```maxoncstderr
error E2053: specs/unused-parameters/void-function-unused.maxon:10:15: the second and later arguments must be named ('name: value')
```

<!-- test: method-on-non-conforming-type-still-errors -->
<!-- skip: language difference: style and lint rules are not checked, since the generated code does not depend on them -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Plain
	let value as Integer

	function helper(unused Integer) returns Integer
		return value
	end 'helper'

	static function create(value Integer) returns Self
		return Self{value: value}
	end 'create'
end 'Plain'

function main() returns ExitCode
	let p = Plain.create(1)
	return p.helper(5)
end 'main'
```
```maxoncstderr
error E3012: specs/unused-parameters/method-on-non-conforming-type-still-errors.maxon:8:18: unused variable: 'unused'
```
