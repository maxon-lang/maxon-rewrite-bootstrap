---
feature: enum-allcasenames
status: experimental
keywords: [enum, union, allCaseNames, iteration, array, string]
category: type-system
---

## Documentation

# Enum and Union allCaseNames

All enums and unions have a static `.allCaseNames` property that returns an `Array with String` containing the case names in declaration order:

```text
enum Color
  red
  green
  blue
end 'Color'

for name in Color.allCaseNames 'loop'
  print("{name}\n")
end 'loop'
// Prints: red green blue

let count = Color.allCaseNames.count()  // 3
```

This works with all backing types (simple, int, float, string, char, struct) and with unions (even those with associated values), because only the case names are returned.

For simple enums without associated values, prefer `.allCases` when you need the actual enum values; use `.allCaseNames` when you only need the names (e.g., for display or serialization).

## Tests

### Simple Enum

<!-- test: enum-allcasenames.simple -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
```maxon
enum Color
	red
	green
	blue
end 'Color'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for name in Color.allCaseNames 'loop'
		print("{name}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
red
green
blue
```

### Count

<!-- test: enum-allcasenames.count -->
```maxon
enum Color
	red
	green
	blue
end 'Color'

function main() returns ExitCode
	let count = Color.allCaseNames.count()
	if count == 3 'check'
		return 1
	end 'check'
	return 0
end 'main'
```
```exitcode
1
```

### Int-Backed Enum

<!-- test: enum-allcasenames.int-backed -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
```maxon
enum HttpStatus
	ok = 200
	notFound = 404
	serverError = 500
end 'HttpStatus'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for name in HttpStatus.allCaseNames 'loop'
		print("{name}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
ok
notFound
serverError
```

### String-Backed Enum

<!-- test: enum-allcasenames.string-backed -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
<!-- skip: non-integer enum raw values are not supported in minimaxon -->
```maxon
enum ContentType
	// TODO: non-integer enum raw values are not supported in minimaxon
	json = "application/json"
	// TODO: non-integer enum raw values are not supported in minimaxon
	html = "text/html"
	// TODO: non-integer enum raw values are not supported in minimaxon
	plain = "text/plain"
end 'ContentType'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for name in ContentType.allCaseNames 'loop'
		print("{name}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
json
html
plain
```

### Union Without Associated Values

<!-- test: enum-allcasenames.union-simple -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
```maxon
union Signal
	start
	stop
	pause
end 'Signal'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for name in Signal.allCaseNames 'loop'
		print("{name}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
start
stop
pause
```

### Union With Associated Values

<!-- test: enum-allcasenames.union-associated -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
```maxon
typealias Integer = int(i64.min to i64.max)

union Container
	empty
	value(n Integer)
	pair(a Integer, b Integer)
end 'Container'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for name in Container.allCaseNames 'loop'
		print("{name}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
empty
value
pair
```

### Single Case

<!-- test: enum-allcasenames.single-case -->
```maxon
enum Singleton
	only
end 'Singleton'

function main() returns ExitCode
	let count = Singleton.allCaseNames.count()
	if count == 1 'check'
		return 1
	end 'check'
	return 0
end 'main'
```
```exitcode
1
```

### Index Access

<!-- test: enum-allcasenames.index -->
```maxon
enum Direction
	north
	south
	east
	west
end 'Direction'

function main() returns ExitCode
	let names = Direction.allCaseNames
	let first = try names.get(0) otherwise "?"
	print("{first}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
north
```
