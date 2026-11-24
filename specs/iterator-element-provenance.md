---
feature: iterator-element-provenance
status: experimental
keywords: [let, read-only, iterator, cursor, withIterator, createIterator, current, element, for]
category: type-system
---

# An iterator or cursor over a read-only source hands out read-only elements

## Documentation

A `let` is a name that is never reassigned, so the records a `let` collection holds stay writable. An element an
iterator or a cursor yields is read out of the collection it was made from, and it is the collection's own record:

- a `for` over `withIterator()`, or over any cursor made from a `let`, a `let` parameter or a `let` field, yields
  the collection's records, exactly as `for element in collection` does, and a write through one is legal;
- `current()` and `peek()` of a cursor made by `createIterator()` or `cursor()` yield the collection's records too:
  writing through one, passing one to a function that writes it, and binding one to a `var` that is then written
  are legal;
- the cursor itself moves: `advance()`, `retreat()` and `seek()` change it, so a cursor is a `var`;
- the same holds over a `var` collection, through a `for` and through a cursor alike.

A `let` that was made from a `var` (`let frozen = items`, which moves `items`) is a `let` like any other: it is
never reassigned, and what it holds can be written.

## Tests

<!-- test: a-for-with-iterator-over-a-let-array-writes-its-elements -->
`frozen` is the `let` that took `items` over. The element `c` is a record `frozen` holds, and the write reaches it.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function main() returns ExitCode
	var items = Plain.create(0)
	items.push(Counter.create(4))
	var frozen = items
	for (_, c) in frozen.enumerated() 'each'
		c.n = 99
	end 'each'
	let first = try frozen.get(0) otherwise return 1
	return try first.n as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
99
```

<!-- test: a-for-with-iterator-over-a-let-built-by-a-call-writes-its-elements -->
The same with a `let` bound to a call result instead of one moved out of a `var`.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function build() returns Plain
	var items = Plain.create(0)
	items.push(Counter.create(4))
	return items
end 'build'

function main() returns ExitCode
	var frozen = build()
	for (_, c) in frozen.enumerated() 'each'
		c.n = 99
	end 'each'
	let first = try frozen.get(0) otherwise return 1
	return try first.n as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
99
```

<!-- test: a-for-with-iterator-over-a-let-parameters-field-writes-through-the-call -->
`bumpAll` writes the elements of the array its parameter holds. Handing it a `let` is accepted: the `let` is never reassigned.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

type Bag
	export var items as Plain

	static function create(items Plain) returns Self
		return Self{items: items.clone()}
	end 'create'
end 'Bag'

function bumpAll(mutates bag Bag)
	for (_, c) in bag.items.enumerated() 'each'
		c.n = 99
	end 'each'
end 'bumpAll'

function main() returns ExitCode
	var items = Plain.create(0)
	items.push(Counter.create(4))
	var bag = Bag.create(items)
	bumpAll(bag)
	let first = try bag.items.get(0) otherwise return 1
	return try first.n as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
99
```

<!-- test: the-current-element-of-a-cursor-over-a-let-array-can-be-bound-to-a-var -->
<!-- skip: language difference: a let, a let field, a let parameter and a match binding are read-only places, where maxon's let only forbids rebinding the name -->
A cursor made from the `let` `frozen` hands out `frozen`'s elements, so `var c` writes one.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function main() returns ExitCode
	let items = Plain.create(0)
	items.push(Counter.create(4))
	let frozen = items
	let it = try frozen.createIterator() otherwise return 2
	let c = it.current()
	c.n = 99
	let first = try frozen.get(0) otherwise return 1
	return first.n as ExitCode
end 'main'
```
```exitcode
99
```

<!-- test: the-current-element-of-a-cursor-over-a-let-array-is-written-by-a-callee -->
<!-- skip: language difference: a let, a let field, a let parameter and a match binding are read-only places, where maxon's let only forbids rebinding the name -->
The element is passed straight to `bump`, which writes it.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function bump(c Counter)
	c.n = 99
end 'bump'

function main() returns ExitCode
	let items = Plain.create(0)
	items.push(Counter.create(4))
	let frozen = items
	let it = try frozen.createIterator() otherwise return 2
	bump(it.current())
	let first = try frozen.get(0) otherwise return 1
	return first.n as ExitCode
end 'main'
```
```exitcode
99
```

<!-- test: an-element-read-out-of-a-let-moved-from-a-var-can-be-bound-to-a-var -->
<!-- skip: language difference: a value has one owner, so binding, storing or passing it moves it, where maxon shares it by a reference count -->
`get` on the `let` that took `items` over hands out an element of `frozen`. No cursor is involved.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function main() returns ExitCode
	let items = Plain.create(0)
	items.push(Counter.create(4))
	let frozen = items
	let c = try frozen.get(0) otherwise return 2
	c.n = 99
	let first = try frozen.get(0) otherwise return 1
	return first.n as ExitCode
end 'main'
```
```exitcode
99
```

<!-- test: a-var-array-hands-out-writable-elements-through-a-for-and-a-cursor -->
<!-- skip: language difference: a let, a let field, a let parameter and a match binding are read-only places, where maxon's let only forbids rebinding the name -->
Over a `var` array the same two routes write: the `for` variable and the cursor's element both are the array's own
records.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function main() returns ExitCode
	let items = Plain.create(0)
	items.push(Counter.create(4))
	items.push(Counter.create(5))
	for (_, c) in items.enumerated() 'each'
		c.n = c.n + 10
	end 'each'
	let it = try items.createIterator() otherwise return 2
	let c = it.current()
	c.n = c.n + 100
	try it.advance() otherwise return 3
	let d = it.current()
	d.n = d.n + 1000
	let first = try items.get(0) otherwise return 1
	let second = try items.get(1) otherwise return 1
	return (first.n + second.n - 1000 - 100) as ExitCode
end 'main'
```
```exitcode
29
```

<!-- test: a-let-array-is-read-and-a-var-cursor-over-it-moves -->
A `var` cursor over a `let` array advances, and every element it yields is read: bound to a `let`, summed, passed to
a function that only reads. Nothing writes an element, so the program is accepted.

```maxon
typealias Integer = int(i64.min to i64.max)

type Counter
	export var n as Integer

	static function create(n Integer) returns Self
		return Self{n: n}
	end 'create'
end 'Counter'

typealias Plain = Array with Counter

function read(c Counter) returns Integer
	return c.n
end 'read'

function main() returns ExitCode
	var items = Plain.create(0)
	items.push(Counter.create(4))
	items.push(Counter.create(5))
	let frozen = items
	var total = 0 as Integer
	for (_, c) in frozen.enumerated() 'each'
		total = try total + read(c) otherwise panic("out of range")
	end 'each'
	var it = try frozen.createIterator() otherwise return 2
	let head = it.current().clone()
	total = try total + head.n otherwise panic("out of range")
	try it.advance() otherwise return 3
	total = try total + read(it.current()) otherwise panic("out of range")
	return try total as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
18
```
