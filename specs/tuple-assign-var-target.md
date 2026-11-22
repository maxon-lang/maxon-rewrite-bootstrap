---
feature: tuple-assign-var-target
status: stable
keywords: [tuple, assignment, destructuring, var, mutable]
category: statements
---

# Tuple Assignment — the `var` declaring target

## Documentation

`specs/tuple-assign.md` shows three shapes of target: a bare name that rebinds an existing `var`, a
`let <name>` that declares a fresh immutable binding, and `_` which discards. The compiler admits a fourth,
`var <name>`, which declares a fresh **mutable** binding at that position:

```text
(let a, var b) = pair()
b = b + 10
```

It is the per-element spelling of what `var (a, b) = t` already means for the declaring destructure, so
the assignment form admits it too. Refusing it would be an invented restriction.

A `var` target is a declaration like any other, so it carries every rule a body `var` carries — including
**E3077**, "this `var` is never reassigned, so it should be a `let`".

## Tests

⚠ **THIS FILE EXISTS BECAUSE `tuple-assign.md` PROVES NOTHING ABOUT THIS SHAPE.**
`tuple-assign.md`'s `tuple-assign-mixed-var-decl` and `tuple-assign-let-decl` are byte-identical programs
and both spell `(x, let y)`, so the `var`-qualified target is exercised by no case there.

<!-- test: var-target-declares-a-mutable-binding -->
The declared `b` is reassignable afterwards, which is the whole of what `var` adds over `let`.
```maxon
typealias Integer = int(i64.min to i64.max)

function pair(n Integer) returns (Integer, Integer)
	return (n, try n + 1 otherwise panic("out of range"))
end 'pair'

function main() returns ExitCode
	(let a, var b) = pair(5)
	b = try b + 10 otherwise panic("out of range")
	return try a + b otherwise panic("out of range")
end 'main'
```
```exitcode
21
```

<!-- test: var-target-beside-a-rebind -->
A `var` target sits beside a bare rebinding target in one statement: `x` already exists and is written,
`y` is declared here and then written again.
```maxon
typealias Integer = int(i64.min to i64.max)

function pair(n Integer) returns (Integer, Integer)
	return (try n * 2 otherwise panic("out of range"), try n * 3 otherwise panic("out of range"))
end 'pair'

function main() returns ExitCode
	var x = 0 as Integer
	(x, var y) = pair(7)
	y = try y - 1 otherwise panic("out of range")
	return try x + y otherwise panic("out of range")
end 'main'
```
```exitcode
34
```

<!-- test: var-target-never-reassigned-is-e3077 -->
<!-- skip: language difference: style and lint rules are not checked, since the generated code does not depend on them -->
A `var` target is a body `var` declaration, so the never-reassigned rule reaches it exactly as it reaches
`var y = …`. This is what says the target goes through the ordinary declaration door rather than a
private one.
```maxon
typealias Integer = int(i64.min to i64.max)

function pair(n Integer) returns (Integer, Integer)
	return (n, n + 1)
end 'pair'

function main() returns ExitCode
	(let a, var b) = pair(5)
	return a + b
end 'main'
```
```maxoncstderr
error E3077: <fragment>:9:14: variable 'b' is never reassigned; use 'let' instead of 'var'
```

<!-- test: var-target-in-a-loop-carries -->
A `var` target declared inside a loop body is fresh each trip, while a bare target rebinds the binding the
loop carries. Both spellings in one loop, so the header phi and the per-trip declaration are exercised
together.
```maxon
typealias Integer = int(i64.min to i64.max)

function step(n Integer) returns (Integer, Integer)
	return (try n + 1 otherwise panic("out of range"), try n * 2 otherwise panic("out of range"))
end 'step'

function main() returns ExitCode
	var acc = 0 as Integer
	var total = 0 as Integer
	var i = 0 as Integer
	while i < 4 'loop'
		(acc, var doubled) = step(acc)
		doubled = try doubled + 1 otherwise panic("out of range")
		total = try total + doubled otherwise panic("out of range")
		i = i + 1
	end 'loop'
	return try total as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
16
```
