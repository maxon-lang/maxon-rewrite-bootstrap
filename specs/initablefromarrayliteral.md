---
feature: initablefromarrayliteral
status: stable
keywords: [array, literal, interface, InitableFromArrayLiteral, generic, type annotation]
category: type-system
---

# InitableFromArrayLiteral Interface

## Documentation

### Array Literals with Type Annotations

The stdlib `Array` type implements `InitableFromArrayLiteral`, allowing initialization from array literals:

```text
let arr = [1, 2, 3]
```

This creates an Array containing the elements 1, 2, and 3.

## Tests

<!-- test: array-from-literal -->
<!-- skip: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert -->
```maxon
function main() returns ExitCode
	// TODO: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert
	let arr = [10, 20, 30]
	return arr.count()
end 'main'
```
```exitcode
3
```

<!-- test: array-from-literal-access -->
<!-- skip: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert -->
```maxon
function main() returns ExitCode
	// TODO: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert
	let arr = [10, 20, 30]
	let val = try arr.get(1) otherwise 0
	return val
end 'main'
```
```exitcode
20
```

<!-- test: array-from-literal-first -->
<!-- skip: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert -->
```maxon
function main() returns ExitCode
	// TODO: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert
	let arr = [42, 2, 3]
	let val = try arr.first() otherwise 0
	return val
end 'main'
```
```exitcode
42
```

<!-- test: array-from-literal-last -->
<!-- skip: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert -->
```maxon
function main() returns ExitCode
	// TODO: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert
	let arr = [1, 2, 99]
	let val = try arr.last() otherwise 0
	return val
end 'main'
```
```exitcode
99
```
