---
feature: self-field-method-chain
status: experimental
keywords: [self, field, method, chain, nested]
category: type-system
---

# Self Field Method Chain

## Documentation

Methods on struct types can call methods on their own fields using `self.field.method()` syntax. This supports arbitrary chain depth like `self.field.subfield.method()`.

## Tests

<!-- test: self-field-push -->
Call a method on a struct field via self.
```maxon
typealias Integer = int(i64.min to i64.max)

typealias IntArray = Array with Integer

type Container
	export var items as IntArray

	function addItem(mutates self, value Integer)
		self.items.push(value)
	end 'addItem'

	function getCount() returns Integer
		return try self.items.count() as Integer otherwise panic("out of range")
	end 'getCount'

	static function create(items IntArray) returns Self
		return Self{items: items.clone()}
	end 'create'
end 'Container'

function main() returns ExitCode
	var c = Container.create(IntArray.create(0))
	c.addItem(10)
	c.addItem(20)
	return try c.getCount() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
2
```

<!-- test: self-field-get -->
Call a throwing method on a struct field via self.
```maxon
typealias Integer = int(i64.min to i64.max)

typealias IntArray = Array with Integer

type NumberList
	export var data as IntArray

	function add(mutates self, value Integer)
		self.data.push(value)
	end 'add'

	function first() returns Integer
		return try self.data.get(0) otherwise -1
	end 'first'

	static function create(data IntArray) returns Self
		return Self{data: data.clone()}
	end 'create'
end 'NumberList'

function main() returns ExitCode
	var list = NumberList.create(IntArray.create(0))
	list.add(42)
	return try list.first() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
42
```

<!-- test: self-nested-field-method -->
Call a method on a nested struct field via self.
```maxon
typealias Integer = int(i64.min to i64.max)

typealias IntArray = Array with Integer

type Inner
	export var values as IntArray

	function total() returns Integer
		return try self.values.count() as Integer otherwise panic("out of range")
	end 'total'

	static function create(values IntArray) returns Self
		return Self{values: values.clone()}
	end 'create'
end 'Inner'

type Outer
	export var inner as Inner

	function getInnerCount() returns Integer
		return try self.inner.values.count() as Integer otherwise panic("out of range")
	end 'getInnerCount'

	static function create(inner Inner) returns Self
		return Self{inner: inner.clone()}
	end 'create'
end 'Outer'

function main() returns ExitCode
	let inner = Inner.create([1, 2, 3])
	let outer = Outer.create(inner)
	return try outer.getInnerCount() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
3
```

<!-- test: self-field-method-assignment -->
Mutate a struct field's sub-object via self chain.
```maxon
typealias Integer = int(i64.min to i64.max)

typealias IntArray = Array with Integer

type Accumulator
	export var items as IntArray

	function addAndCount(mutates self, value Integer) returns Integer
		self.items.push(value)
		return try self.items.count() as Integer otherwise panic("out of range")
	end 'addAndCount'

	static function create(items IntArray) returns Self
		return Self{items: items.clone()}
	end 'create'
end 'Accumulator'

function main() returns ExitCode
	var acc = Accumulator.create(IntArray.create(0))
	_ = acc.addAndCount(1)
	_ = acc.addAndCount(2)
	return try acc.addAndCount(3) as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
3
```
