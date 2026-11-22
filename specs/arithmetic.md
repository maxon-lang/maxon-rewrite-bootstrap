---
feature: arithmetic
status: stable
keywords: arithmetic, operators, math
category: operators
---
# Arithmetic Operators

## Documentation

Maxon supports basic arithmetic operators for integers:

- `+` addition
- `-` subtraction
- `*` multiplication
- `/` division
- `mod` modulo

## Tests

<!-- test: addition -->
```maxon
function main() returns ExitCode
	return 10 + 5
end 'main'
```
```exitcode
15
```

<!-- test: subtraction -->
```maxon
function main() returns ExitCode
	return 20 - 8
end 'main'
```
```exitcode
12
```

<!-- test: multiplication -->
```maxon
function main() returns ExitCode
	return 6 * 7
end 'main'
```
```exitcode
42
```

<!-- test: division -->
```maxon
function main() returns ExitCode
	return trunc(100 / 4)
end 'main'
```
```exitcode
25
```

<!-- test: modulo -->
```maxon
function main() returns ExitCode
	return 17 mod 5
end 'main'
```
```exitcode
2
```

<!-- test: complex-expression -->
```maxon
function main() returns ExitCode
	return 10 + 5 * 2
end 'main'
```
```exitcode
20
```

<!-- test: div-live-values -->
```maxon

typealias Integer = int(i64.min to i64.max)

typealias NonZero = int(1 to i64.max)

function divLive(a Integer, b NonZero, x Integer) returns Integer
	let preserved = try x + 1 otherwise panic("out of range")
	let result = try (a / (b as Integer)) otherwise panic("b is never zero")
	return trunc(try result + preserved otherwise panic("out of range"))
end 'divLive'

function main() returns ExitCode
	return try divLive(10, b: 2, x: 5) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
11
```

<!-- test: mod-live-values -->
```maxon

typealias Integer = int(i64.min to i64.max)

typealias NonZero = int(1 to i64.max)

function modLive(a Integer, b NonZero, x Integer) returns Integer
	let preserved = try x + 1 otherwise panic("out of range")
	let result = try (a mod (b as Integer)) otherwise panic("b is never zero")
	return try result + preserved otherwise panic("out of range")
end 'modLive'

function main() returns ExitCode
	return try modLive(10, b: 3, x: 5) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
7
```

<!-- test: div-loop -->
```maxon

typealias Integer = int(i64.min to i64.max)

function divLoop(n Integer) returns Integer
	var sum = 0 as Integer
	var i = 1 as Integer
	while i <= n 'loop'
		sum = try sum + trunc(try (50 / i) otherwise panic("divLoop: i was 0")) otherwise panic("out of range")
		i = try i + 1 otherwise panic("out of range")
	end 'loop'
	return sum
end 'divLoop'

function main() returns ExitCode
	return try divLoop(5) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
113
```

<!-- test: div-with-call -->
```maxon

typealias Integer = int(i64.min to i64.max)

typealias NonZero = int(1 to i64.max)

function helper(x Integer) returns Integer
	return try x * 2 otherwise panic("out of range")
end 'helper'

function divCall(a Integer, b NonZero) returns Integer
	let temp = trunc(try (a / (b as Integer)) otherwise panic("b is never zero"))
	let result = helper(temp)
	return try result + temp otherwise panic("out of range")
end 'divCall'

function main() returns ExitCode
	return try divCall(10, b: 2) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
15
```

<!-- test: multi-div -->
```maxon

typealias Integer = int(i64.min to i64.max)

typealias NonZero = int(1 to i64.max)

function multiDiv(a Integer, b NonZero, c Integer, d NonZero) returns Integer
	let r1 = try (a / (b as Integer)) otherwise panic("b is never zero")
	let r2 = try (c / (d as Integer)) otherwise panic("d is never zero")
	return trunc(try r1 + r2 otherwise panic("out of range"))
end 'multiDiv'

function main() returns ExitCode
	return try multiDiv(10, b: 2, c: 20, d: 4) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
10
```

<!-- test: register-pressure -->
```maxon

typealias Integer = int(i64.min to i64.max)

function manyVars(a Integer, b Integer, c Integer, d Integer, e Integer, f Integer) returns Integer
	let v1 = try a + 1 otherwise panic("out of range")
	let v2 = try b + 2 otherwise panic("out of range")
	let v3 = try c + 3 otherwise panic("out of range")
	let v4 = try d + 4 otherwise panic("out of range")
	let v5 = try e + 5 otherwise panic("out of range")
	let v6 = try f + 6 otherwise panic("out of range")
	let v7 = try v1 + v2 otherwise panic("out of range")
	let v8 = try v3 + v4 otherwise panic("out of range")
	let v9 = try v5 + v6 otherwise panic("out of range")
	let v10 = try v7 + v8 otherwise panic("out of range")
	let v11 = try v9 + v10 otherwise panic("out of range")
	let v12 = try v11 + v1 + v2 + v3 + v4 + v5 + v6 otherwise panic("out of range")
	return v12
end 'manyVars'

function main() returns ExitCode
	return try manyVars(1, b: 2, c: 3, d: 4, e: 5, f: 6) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
84
```
