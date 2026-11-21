---
feature: enum-allcases
status: experimental
keywords: [enum, allCases, iteration, array]
category: type-system
---

## Documentation

# Enum allCases

All enums have a static `.allCases` property that returns an `Array` of all cases in declaration order:

```text
enum Color
  red
  green
  blue
end 'Color'

for color in Color.allCases 'loop'
  print("{color.name}\n")
end 'loop'
// Prints: red green blue

let count = Color.allCases.count()  // 3
```

This works with all backing types (simple, int, float, string, char). The array always contains the cases in their declaration order.

`.allCases` is not available on enums with associated values.

## Tests

### Simple Enum

<!-- test: enum-allcases.simple -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
```maxon
enum Color
	red
	green
	blue
end 'Color'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for color in Color.allCases 'loop'
		print("{color.name}\n")
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

<!-- test: enum-allcases.count -->
```maxon
enum Color
	red
	green
	blue
end 'Color'

function main() returns ExitCode
	let count = Color.allCases.count()
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

<!-- test: enum-allcases.int-backed -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
```maxon
enum HttpStatus
	ok = 200
	notFound = 404
	serverError = 500
end 'HttpStatus'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for status in HttpStatus.allCases 'loop'
		print("{status.name}={status.rawValue}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
ok=200
notFound=404
serverError=500
```

### Float-Backed Enum

<!-- test: enum-allcases.float-backed -->
<!-- skip: float literals are not supported in minimaxon -->
<!-- skip: non-integer enum raw values are not supported in minimaxon -->
```maxon
enum Threshold
	// TODO: non-integer enum raw values are not supported in minimaxon
	// TODO: float literals are not supported in minimaxon
	low = 0.1
	// TODO: non-integer enum raw values are not supported in minimaxon
	// TODO: float literals are not supported in minimaxon
	medium = 0.5
	// TODO: non-integer enum raw values are not supported in minimaxon
	// TODO: float literals are not supported in minimaxon
	high = 0.9
end 'Threshold'

function main() returns ExitCode
	let count = Threshold.allCases.count()
	if count == 3 'check'
		return 1
	end 'check'
	return 0
end 'main'
```
```exitcode
1
```

### String-Backed Enum

<!-- test: enum-allcases.string-backed -->
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
	for ct in ContentType.allCases 'loop'
		print("{ct.rawValue}\n")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
application/json
text/html
text/plain
```

### Char-Backed Enum

<!-- test: enum-allcases.char-backed -->
<!-- skip: character literals are not supported in minimaxon -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
<!-- skip: non-integer enum raw values are not supported in minimaxon -->
```maxon
enum Grade
	// TODO: non-integer enum raw values are not supported in minimaxon
	// TODO: character literals are not supported in minimaxon
	a = 'A'
	// TODO: non-integer enum raw values are not supported in minimaxon
	// TODO: character literals are not supported in minimaxon
	b = 'B'
	// TODO: non-integer enum raw values are not supported in minimaxon
	// TODO: character literals are not supported in minimaxon
	c = 'C'
end 'Grade'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for g in Grade.allCases 'loop'
		print("{g.rawValue}")
	end 'loop'
	return 0
end 'main'
```
```exitcode
0
```
```stdout
ABC
```

### Single Case

<!-- test: enum-allcases.single-case -->
```maxon
enum Singleton
	only
end 'Singleton'

function main() returns ExitCode
	let count = Singleton.allCases.count()
	if count == 1 'check'
		return 1
	end 'check'
	return 0
end 'main'
```
```exitcode
1
```

### Error: allCases on Enum with Associated Values

<!-- test: enum-allcases.error-enum -->
<!-- skip: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array -->
<!-- positions: moved -->
```maxon
typealias Integer = int(i64.min to i64.max)

union Container
	empty
	value(n Integer)
end 'Container'

function main() returns ExitCode
	// TODO: iterator-based for loops over a string, a map, a view or an enum's cases are not supported in minimaxon; loop over an array
	for s in Container.allCases 'loop'
		print("x")
	end 'loop'
	return 0
end 'main'
```
```maxoncstderr
error E4006: specs/enum-allcases/enum-allcases.error-enum.maxon:10:11: allCases is not available on unions
```
