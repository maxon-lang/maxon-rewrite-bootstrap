---
feature: challenge-struct-lifetime
status: stable
keywords: struct, lifetime, return, stack, heap
category: semantics
---
# Challenge Struct Lifetime

## Documentation

## Struct Lifetime

Structs returned from functions must have their data survive beyond the function's stack frame.

## Tests

<!-- test: return-struct-with-computed-fields -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Result
	export var sum as Integer
	export var product as Integer

	static function create(sum Integer, product Integer) returns Self
		return Self{sum: sum, product: product}
	end 'create'
end 'Result'

function compute(a Integer, b Integer) returns Result
	return Result.create(try a + b otherwise panic("out of range"), product: try a * b otherwise panic("out of range"))
end 'compute'

function main() returns ExitCode
	let r = compute(3, b: 4)
	return try r.sum + r.product otherwise panic("out of range")
end 'main'
```
```exitcode
19
```

<!-- test: pass-struct-to-function-and-return -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Counter
	export var value as Integer

	static function create(value Integer) returns Self
		return Self{value: value}
	end 'create'
end 'Counter'

function increment(c Counter) returns Counter
	return Counter.create(try c.value + 1 otherwise panic("out of range"))
end 'increment'

function main() returns ExitCode
	let c1 = Counter.create(10)
	let c2 = increment(c1)
	return try c2.value as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
11
```

<!-- test: multiple-struct-returns -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Value
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Value'

function step1() returns Value
	return Value.create(1)
end 'step1'

function step2(v Value) returns Value
	return Value.create(try v.n + 10 otherwise panic("out of range"))
end 'step2'

function step3(v Value) returns Value
	return Value.create(try v.n + 100 otherwise panic("out of range"))
end 'step3'

function main() returns ExitCode
	let v1 = step1()
	let v2 = step2(v1)
	let v3 = step3(v2)
	return try v3.n as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
111
```
