---
feature: method-call-on-parameter
status: stable
keywords: method, call, parameter, same type, sibling
category: expressions
---
# Method Calls on Parameters of the Same Type

## Documentation

When writing a method, you can call other methods on parameters that have the same type as `self`.

## Example

```maxon
typealias Score = int(i64.min to i64.max)

type Counter
	var value as Score

	function getValue() returns Score
		return value
	end 'getValue'

	function addFrom(other Counter) returns Score
		// Call getValue() on 'other', not on 'self'
		return value + other.getValue()
	end 'addFrom'
end 'Counter'
```

The call `other.getValue()` correctly invokes `getValue` on the `other` parameter,
not on `self`.

## Tests

<!-- test: method-call-on-same-type-parameter -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Foo
	var x as Integer

	function bar() returns Integer
		return x
	end 'bar'

	function callBarOn(other Foo) returns Integer
		return other.bar()
	end 'callBarOn'

	static function create(x Integer) returns Self
		return Self{x: x}
	end 'create'
end 'Foo'

function main() returns ExitCode
	let f1 = Foo.create(10)
	let f2 = Foo.create(42)
	return try f1.callBarOn(f2) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```

<!-- test: method-call-chain-same-type -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Value
	var n as Integer

	function get() returns Integer
		return n
	end 'get'

	function add(other Value) returns Integer
		return try n + other.get() otherwise panic("out of range")
	end 'add'

	function multiply(other Value) returns Integer
		return try n * other.get() otherwise panic("out of range")
	end 'multiply'

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Value'

function main() returns ExitCode
	let a = Value.create(5)
	let b = Value.create(3)
	let c = Value.create(2)
	// a.add(b) = 5 + 3 = 8
	// a.multiply(c) = 5 * 2 = 10
	// total = 8 + 10 = 18
	return try a.add(b) + a.multiply(c) otherwise panic("out of range")
end 'main'
```
```exitcode
18
```

<!-- test: sibling-method-call-still-works -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Calculator
	var base as Integer

	function double() returns Integer
		return base * 2
	end 'double'

	function quadruple() returns Integer
		// Sibling call - calls self.double()
		return double() * 2
	end 'quadruple'

	static function create(base Integer) returns Self
		return Self{base: base}
	end 'create'
end 'Calculator'

function main() returns ExitCode
	let calc = Calculator.create(5)
	return calc.quadruple()
end 'main'
```
```exitcode
20
```

<!-- test: method-with-args-on-same-type-parameter -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Adder
	var value as Integer

	function addTo(n Integer) returns Integer
		return try value + n otherwise panic("out of range")
	end 'addTo'

	function delegateAdd(other Adder, n Integer) returns Integer
		return other.addTo(n)
	end 'delegateAdd'

	static function create(value Integer) returns Self
		return Self{value: value}
	end 'create'
end 'Adder'

function main() returns ExitCode
	let a = Adder.create(100)
	let b = Adder.create(50)
	return try a.delegateAdd(b, n: 7) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
57
```
