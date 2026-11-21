---
feature: self-keyword
status: stable
keywords: [self, Self, method, type, instance]
category: type-system
---

# Self and self Keywords

## Documentation

### The `self` Keyword

Inside instance methods, `self` refers to the current instance:

```text
type Counter
  var count as int

  function increment()
    self.count = self.count + 1
  end 'increment'
end 'Counter'
```

Field access can omit `self.` when unambiguous:

```text
type Counter
  var count as int

  function increment()
    count = count + 1
  end 'increment'
end 'Counter'
```

### The `Self` Type

`Self` refers to the enclosing type, useful for method signatures:

```text
type Point
  var x as int
  var y as int

  function origin() returns Self
    return Point{x: 0, y: 0}
  end 'origin'
end 'Point'
```

## Tests

<!-- test: self-explicit-access -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Counter
	var count as Integer

	function increment(mutates self)
		self.count = try self.count + 1 otherwise panic("out of range")
	end 'increment'

	function get() returns Integer
		return self.count
	end 'get'

	static function create(count Integer) returns Self
		return Self{count: count}
	end 'create'
end 'Counter'

function main() returns ExitCode
	var c = Counter.create(0)
	c.increment()
	c.increment()
	return try c.get() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
2
```

<!-- test: self-implicit-access -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Counter
	var count as Integer

	function increment(mutates self)
		count = try count + 1 otherwise panic("out of range")
	end 'increment'

	function get() returns Integer
		return count
	end 'get'

	static function create(count Integer) returns Self
		return Self{count: count}
	end 'create'
end 'Counter'

function main() returns ExitCode
	var c = Counter.create(0)
	c.increment()
	c.increment()
	c.increment()
	return try c.get() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
3
```

<!-- test: self-with-params -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Accumulator
	var total as Integer

	function add(mutates self, value Integer)
		self.total = try self.total + value otherwise panic("out of range")
	end 'add'

	function getTotal() returns Integer
		return self.total
	end 'getTotal'

	static function create(total Integer) returns Self
		return Self{total: total}
	end 'create'
end 'Accumulator'

function main() returns ExitCode
	var acc = Accumulator.create(0)
	acc.add(10)
	acc.add(20)
	acc.add(12)
	return try acc.getTotal() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```

<!-- test: self-multiple-fields -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Point
	var x as Integer
	var y as Integer

	function sum() returns Integer
		return try self.x + self.y otherwise panic("out of range")
	end 'sum'

	function setX(mutates self, newX Integer)
		self.x = newX
	end 'setX'

	static function create(x Integer, y Integer) returns Self
		return Self{x: x, y: y}
	end 'create'
end 'Point'

function main() returns ExitCode
	let p = Point.create(10, y: 32)
	return try p.sum() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```

<!-- test: self-modify-and-return -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Value
	var n as Integer

	function double(mutates self)
		self.n = try self.n * 2 otherwise panic("out of range")
	end 'double'

	function get() returns Integer
		return self.n
	end 'get'

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Value'

function main() returns ExitCode
	var v = Value.create(21)
	v.double()
	return try v.get() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```

<!-- test: self-implicit-multiple-fields -->
```maxon

typealias Integer = int(i64.min to i64.max)

type Rectangle
	var width as Integer
	var height as Integer

	function area() returns Integer
		return try width * height otherwise panic("out of range")
	end 'area'

	static function create(width Integer, height Integer) returns Self
		return Self{width: width, height: height}
	end 'create'
end 'Rectangle'

function main() returns ExitCode
	let r = Rectangle.create(6, height: 7)
	return try r.area() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```
