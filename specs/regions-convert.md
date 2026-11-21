---
feature: regions-convert
status: experimental
keywords: [function type, interface, conformer, closure, mutable self, mutable function, may yield, conversion]
category: semantics
---

# Converting closures and conformers to function types and requirements

## Documentation

A function type and an interface requirement are read-only and never wait or yield unless declared
otherwise. A closure, function value or conformer converts to one only if each unmarked parameter (and
`self`, and each capture) is read and not written, and it neither waits nor yields. A requirement whose
conformers write the receiver is declared `mutable self`; a function type whose callee writes a parameter
declares `mutable T`; a function type whose closures write what they captured is a `mutable function(…)`,
and a value of it cannot be called through frozen or read-only data. Every fact is local to the declaration,
so separately compiled libraries stay sound.

## Tests

<!-- test: closure-writing-param-to-plain-function-type-refused -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- positions: moved -->
`advance` writes its parameter, and `Look` declares no `mutable` there, so a caller holding the argument read-only would be written through.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Look = function(Counter) returns Tally

type Counter
	var n as Tally

	static function create() returns Self
		return Self{n: 0}
	end 'create'

	function bump() returns Tally
		self.n = self.n + 1
		return self.n
	end 'bump'
end 'Counter'

function advance(counter Counter) returns Tally
	return counter.bump()
end 'advance'

function apply(look Look, counter Counter) returns Tally
	return look(counter)
end 'apply'

function main() returns ExitCode
	var counter = Counter.create()
	let n = apply(advance, counter: counter)
	print("{n}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3210: <fragment>:28:16: function value for 'Look' writes its parameter 'counter', but 'Look' does not declare it `mutable`
```

<!-- test: conformer-writing-receiver-needs-mutable-self -->
`Bag.grow` writes its receiver, and `Grows.grow` is read-only unless declared `mutable self`.

```maxon
typealias Tally = int(0 to u64.max)

interface Grows
	function grow(by Tally)
end 'Grows'

type Bag implements Grows
	var total as Tally

	static function create() returns Self
		return Self{total: 0}
	end 'create'

	function grow(by Tally)
		self.total = self.total + by
	end 'grow'

	function size() returns Tally
		return self.total
	end 'size'
end 'Bag'

function stretch(g Grows)
	g.grow(5)
end 'stretch'

function main() returns ExitCode
	var bag = Bag.create()
	stretch(bag)
	print("{bag.size()}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3210: <fragment>:15:11: 'Bag.grow' writes its receiver, but 'Grows.grow' does not declare `mutable self`
```

<!-- test: mutable-function-type-accepts-writer -->
<!-- skip: closures are not supported in minimaxon -->
<!-- skip: function typealiases are not supported in minimaxon -->
`Tallier` is a `mutable function`, so a closure that writes the record it captured converts to it, and the running total survives between the two calls.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Tallier = function(Tally) returns Tally mutates captures

type Total
	var sum as Tally

	static function create() returns Self
		return Self{sum: 0}
	end 'create'

	function bump(n Tally) returns Tally
		self.sum = self.sum + n
		return self.sum
	end 'bump'
end 'Total'

function feed(tallier Tallier) returns Tally
	let first = tallier(3)
	let second = tallier(4)
	return first + second
end 'feed'

function main() returns ExitCode
	let total = Total.create()
	// TODO: closures are not supported in minimaxon
	let result = feed(function(n) gives total.bump(n))
	print("{result}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
10
```

<!-- test: yielding-closure-to-function-type-refused -->
<!-- skip: 'async' is not supported in minimaxon; use cown/when concurrency -->
<!-- skip: 'await' is not supported in minimaxon; use cown/when concurrency -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- positions: moved -->
`slow` awaits a coroutine, and a function type never yields, so a call through `Step` inside a `modify` block could not be proven safe.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Step = function(Tally) returns Tally

function work(n Tally) returns Tally
	sleep(1)
	return n + 1
end 'work'

function slow(n Tally) returns Tally
	// TODO: 'async' is not supported in minimaxon; use cown/when concurrency
	let pending = async work(n)
	// TODO: 'await' is not supported in minimaxon; use cown/when concurrency
	return await pending
end 'slow'

function apply(step Step) returns Tally
	return step(1)
end 'apply'

function main() returns ExitCode
	let n = apply(slow)
	print("{n}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3210: <fragment>:20:16: function value for 'Step' may yield (it awaits a coroutine at line 12), but a function type never waits or yields
```

<!-- test: mutable-function-called-through-frozen -->
<!-- skip: closures are not supported in minimaxon -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- skip: parameter capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
A `mutable function` may write what it captured, so it cannot be called through frozen data.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Tallier = function(Tally) returns Tally mutates captures

type Holder
	export var tallier as Tallier

	static function create() returns Self
		// TODO: closures are not supported in minimaxon
		return Self{tallier: function(n Tally) gives n + 1}
	end 'create'
end 'Holder'

// TODO: parameter capability 'immutable' is not supported in minimaxon
function bump(holder immutable Holder) returns Tally
	return holder.tallier(1)
end 'bump'

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E3211: <fragment>:14:23: 'Tallier' mutates captures and cannot be called through immutable data
```

<!-- test: frozen-argument-to-a-mutable-function-parameter-refused -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- positions: moved -->
`Edit` declares its parameter `mutable`, so the call `edit(pinned)` with a frozen box is refused where it is written.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Edit = function(mutable Box)

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function bump(mutates b Box)
	b.n = b.n + 1
end 'bump'

function run(edit Edit)
	var pinned = freeze(Box.create(1))
	edit(pinned)
end 'run'

function main() returns ExitCode
	run(bump)
	return 0
end 'main'
```
```maxoncstderr
error E3207: <fragment>:19:6: 'Edit' takes `mutable` parameter; 'pinned' is immutable
```

<!-- test: isolated-requirement-parameter-refuses-a-mutable-argument -->
<!-- skip: parameter capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
`Sink.take` declares `items` isolated, so `spare`, a mutable array of this region, cannot be passed to it.

```maxon
typealias LineArray = Array with String

interface Sink
	// TODO: parameter capability 'isolated' is not supported in minimaxon
	function take(mutates self, items isolated LineArray)
end 'Sink'

type Counter implements Sink
	var total as Count

	static function create() returns Self
		return Self{total: 0}
	end 'create'

	// TODO: parameter capability 'isolated' is not supported in minimaxon
	function take(items isolated LineArray)
		self.total = self.total + items.count()
	end 'take'

	function size() returns Count
		return self.total
	end 'size'
end 'Counter'

function feed(sink Sink)
	var spare = LineArray.create(0)
	spare.push("a")
	sink.take(spare)
end 'feed'

function main() returns ExitCode
	var counter = Counter.create()
	feed(counter)
	print("{counter.size()}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3200: <fragment>:27:7: 'spare' is not isolated: only a clean call, a `swap` or an `isolated` local goes where an isolated value is wanted; pass a copy
```

<!-- test: isolated-function-type-parameter-refuses-a-mutable-argument -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- skip: parameter capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
`Take` declares its parameter isolated, so `spare`, a mutable array of this region, cannot be passed through it.

```maxon
typealias LineArray = Array with String

// TODO: function typealiases are not supported in minimaxon
typealias Take = function(isolated LineArray)

// TODO: parameter capability 'isolated' is not supported in minimaxon
function count(items isolated LineArray)
	print("{items.count()}\n")
end 'count'

function feed(take Take)
	var spare = LineArray.create(0)
	spare.push("a")
	take(spare)
end 'feed'

function main() returns ExitCode
	feed(count)
	return 0
end 'main'
```
```maxoncstderr
error E3200: <fragment>:12:6: 'spare' is not isolated: only a clean call, a `swap` or an `isolated` local goes where an isolated value is wanted; pass a copy
```

<!-- test: alias-capability-reaches-the-call-whichever-declaration-comes-first -->
<!-- skip: capability typealias 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
`IsoBox` is declared after `consume` and names an `isolated` box, so `consume(spare)` with a mutable local is refused as if the parameter were written `isolated Box`.

```maxon
typealias Tally = int(0 to u64.max)

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function consume(box IsoBox) returns Tally
	return box.n
end 'consume'

// TODO: capability typealias 'isolated' is not supported in minimaxon
typealias IsoBox = isolated Box

function main() returns ExitCode
	let spare = Box.create(1)
	print("{spare.n}\n")
	let n = consume(spare)
	print("{n}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3200: <fragment>:21:18: 'spare' is not isolated: only a clean call, a `swap` or an `isolated` local goes where an isolated value is wanted; pass a copy
```

<!-- test: closure-writing-a-capture-runs-when-called-directly -->
<!-- skip: closures are not supported in minimaxon -->
A closure whose body writes what it captured has an inferred `mutable function` type, so calling `bump` directly needs no cast and writes the `e` it captured.

```maxon
typealias Tally = int(0 to u64.max)

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function touch(mutates box Box) returns Tally
	box.n = box.n + 1
	return box.n
end 'touch'

function main() returns ExitCode
	let e = Box.create(0)
	// TODO: closures are not supported in minimaxon
	let bump = function() gives touch(e)
	print("{bump()}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
1
```

<!-- test: plain-closure-capturing-a-variable-the-back-edge-assigns-frozen-data-is-refused -->
<!-- skip: closures are not supported in minimaxon -->
<!-- skip: parameter capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
Each trip builds `bump` over `e`, and `e = pinned` hands the next trip frozen data, so the plain closure would write the frozen box.

```maxon
typealias Tally = int(0 to u64.max)

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function touch(mutates box Box) returns Tally
	box.n = box.n + 1
	return box.n
end 'touch'

// TODO: parameter capability 'immutable' is not supported in minimaxon
function edit(pinned immutable Box, rounds Tally) returns Tally
	var e = Box.create(0)
	var i = 0 as Tally
	var sum = 0 as Tally
	while i < rounds 'again'
		// TODO: closures are not supported in minimaxon
		let bump = function() gives touch(e)
		sum = sum + bump()
		e = pinned
		i = i + 1
	end 'again'
	return sum
end 'edit'

function main() returns ExitCode
	let pinned = freeze(Box.create(1))
	print("{edit(pinned, rounds: 2)}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3197: <fragment>:23:37: cannot write 'e' through the closure that captures it: 'e' is read-only, as it joins immutable and mutable data
```

<!-- test: mutable-function-closure-writing-frozen-data-it-captured-is-refused -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- positions: moved -->
`Bump` is a `mutable function`, but `e` holds frozen data, so the closure would write through it.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Bump = function() returns Tally mutates captures

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function touch(mutates box Box) returns Tally
	box.n = box.n + 1
	return box.n
end 'touch'

function call(bump Bump) returns Tally
	return bump()
end 'call'

function main() returns ExitCode
	var e = freeze(Box.create(1))
	print("{call(function() gives touch(e))}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3207: <fragment>:24:15: 'call' writes 'bump' (line 19: it is a function value of 'Bump', which declares `mutates captures`, called here); the closure at <fragment>:24:15 is immutable
error E3197: <fragment>:24:38: cannot write 'e' through the closure that captures it: 'e' is immutable
```

<!-- test: mutable-function-closure-capturing-a-variable-the-back-edge-assigns-frozen-data-is-refused -->
<!-- skip: closures are not supported in minimaxon -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- skip: parameter capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
The closure is a `mutable function`, but `e = pinned` hands the next trip frozen data, so at the loop header `e` joins immutable and mutable data and the closure's write through it is refused.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Bump = function() returns Tally mutates captures

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function touch(mutates box Box) returns Tally
	box.n = box.n + 1
	return box.n
end 'touch'

// TODO: parameter capability 'immutable' is not supported in minimaxon
function edit(pinned immutable Box, rounds Tally) returns Tally
	var e = Box.create(0)
	var i = 0 as Tally
	var sum = 0 as Tally
	while i < rounds 'again'
		// TODO: closures are not supported in minimaxon
		let bump = (function() gives touch(e)) as Bump
		sum = sum + bump()
		e = pinned
		i = i + 1
	end 'again'
	return sum
end 'edit'

function main() returns ExitCode
	let pinned = freeze(Box.create(1))
	print("{edit(pinned, rounds: 2)}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3197: <fragment>:24:38: cannot write 'e' through the closure that captures it: 'e' is read-only, as it joins immutable and mutable data
```

<!-- test: conformer-leaving-an-isolated-requirement-parameter-unmarked-is-refused -->
<!-- skip: parameter capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
A call through `Sink.take` hands the caller's region over, so a conformer that does not declare its parameter `isolated` would never release it.

```maxon
typealias Tally = int(0 to u64.max)

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

interface Sink
	// TODO: parameter capability 'isolated' is not supported in minimaxon
	function take(mutates self, box isolated Box)
end 'Sink'

type Counter implements Sink
	var total as Tally

	static function create() returns Self
		return Self{total: 0}
	end 'create'

	function take(box Box)
		self.total = self.total + box.n
	end 'take'

	function size() returns Tally
		return self.total
	end 'size'
end 'Counter'

function feed(sink Sink)
	sink.take(Box.create(3))
end 'feed'

function main() returns ExitCode
	var counter = Counter.create()
	feed(counter)
	print("{counter.size()}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3210: <fragment>:23:11: 'Counter.take' takes its parameter 'box' without `isolated`, but 'Sink.take' declares it `isolated`
```

<!-- test: mutable-function-alias-declared-in-an-extension-is-called-with-two-arguments -->
<!-- skip: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert -->
<!-- skip: closures are not supported in minimaxon -->
<!-- skip: extension blocks are not supported in minimaxon -->
<!-- skip: function typealiases are not supported in minimaxon -->
`Joiner` is a `mutable function` alias declared inside an extension, and `join(first, second)` calls it with two arguments through the alias's function type like any other function value.

```maxon
// TODO: extension blocks are not supported in minimaxon
extension Array
	// TODO: function typealiases are not supported in minimaxon
	typealias Joiner = function(Element, Element) returns Ordering mutates captures
	function best(join Joiner) returns Ordering
		return join(try self.get(0) otherwise panic("first"), try self.get(1) otherwise panic("second"))
	end 'best'
end 'Array'

function main() returns ExitCode
	// TODO: array/map literals are not supported in minimaxon; use Array.create(0) and push, or Map.create() and upsert
	let xs = [1 as Count, 2 as Count]
	// TODO: closures are not supported in minimaxon
	let order = xs.best(function(a, b) gives a.compare(b))
	print("{order.name}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
lessThan
```

<!-- test: function-value-storing-its-parameter-to-plain-function-type-refused -->
<!-- skip: function typealiases are not supported in minimaxon -->
<!-- positions: moved -->
`adopt` keeps `box` in the `Holder` it returns, so its result holds data reached from its read-only parameter, and `Keep` promises nothing about what a result shares with its argument.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: function typealiases are not supported in minimaxon
typealias Keep = function(Box) returns Holder

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

type Holder
	export var held as Box

	static function build(item Box) returns Self
		return Self{held: item}
	end 'build'
end 'Holder'

function adopt(box Box) returns Holder
	return Holder.build(box)
end 'adopt'

function apply(keep Keep, box Box) returns Holder
	return keep(box)
end 'apply'

function main() returns ExitCode
	var box = Box.create(1)
	let other = freeze(Box.create(2))
	let h = apply(adopt, box: box)
	print("{h.held.n} {other.n}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3210: <fragment>:32:16: function value for 'Keep' returns data reached from its read-only parameter 'box', which 'Keep' does not allow
```
