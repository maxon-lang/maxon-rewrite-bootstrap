---
feature: regions-syntax
status: experimental
keywords: [isolated, immutable, mutable, self, capability, region, contextual words]
category: semantics
---

# Capability words and the `self` parameter

## Documentation

A capability is a prefix word on a type, read like an adjective: `isolated`, `immutable` or `mutable`. The
words are contextual: they are recognised only where the grammar expects a type, so a local named `mutable`
or a method named `merge` keeps working. A method may declare its receiver as its first parameter with a
capability, `function reported(immutable self)`, for the cases inference cannot express; `self` is never
written at the call.

A capability word written directly on a value type (a number, `bool`, a payload-free union or enum,
`String`), a service handle or a promise is refused, because such a type has no region of its own to
qualify. A union with a payload has one and takes `isolated` and `immutable`. `mutable` on a parameter
or function-type parameter of a value type is the one exception: it makes the parameter by reference.

## Tests

<!-- test: capability-word-on-a-value-type-refused -->
<!-- skip: parameter capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
`immutable` qualifies a region, and a `Tally` is a number that belongs to none, so the word is refused.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: parameter capability 'immutable' is not supported in minimaxon
function echo(n immutable Tally) returns Tally
	return n
end 'echo'

function main() returns ExitCode
	print("{echo(5)}\n")
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:4:17: Expected 'a type' but got 'immutable'
```

<!-- test: capability-word-on-a-payload-free-union-refused -->
<!-- skip: parameter capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
A union none of whose cases carries a payload is a plain tag value that belongs to no region, so `isolated` is refused as on an enum.

```maxon
union Mode
	idle
	busy
end 'Mode'

// TODO: parameter capability 'isolated' is not supported in minimaxon
function echo(m isolated Mode) returns Mode
	return m
end 'echo'

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:7:17: Expected 'a type' but got 'isolated'
```

<!-- test: capability-word-on-a-payload-union-parameter-and-result-runs -->
<!-- skip: parameter capability 'isolated' is not supported in minimaxon -->
<!-- skip: return type capability 'isolated' is not supported in minimaxon -->
A union with a payload holds a record, so it is a region and takes `isolated` on a parameter and on a result.

```maxon
typealias Count = int(0 to 1000)

type Box
	export var n as Count

	static function create(n Count) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

union Slot
	filled(box Box)
	vacant
end 'Slot'

// TODO: return type capability 'isolated' is not supported in minimaxon
function fill(n Count) returns isolated Slot
	return Slot.filled(Box.create(n))
end 'fill'

// TODO: parameter capability 'isolated' is not supported in minimaxon
function read(slot isolated Slot) returns Count
	return match slot 'which'
		filled(box) gives box.n
		vacant gives 0
	end 'which'
end 'read'

function main() returns ExitCode
	let slot = fill(7)
	return read(slot) as ExitCode
end 'main'
```
```exitcode
7
```

<!-- test: self-parameter-not-first-refused -->
<!-- positions: moved -->
A receiver declared as a parameter is the first parameter; written after another it is a parse error.

```maxon
typealias Tally = int(0 to u64.max)

type Scale
	var reading as Tally

	static function create(reading Tally) returns Self
		return Self{reading: reading}
	end 'create'

	function plus(by Tally, immutable self) returns Tally
		return self.reading + by
	end 'plus'
end 'Scale'

function main() returns ExitCode
	let scale = freeze(Scale.create(4))
	print("{scale.plus(3)}\n")
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:11:36: Expected 'a type' but got 'self'
```

<!-- test: immutable-self-method-runs-on-frozen -->
<!-- skip: receiver capability 'immutable self' is not supported in minimaxon -->
A method declared `immutable self` runs on a frozen value, and reads the receiver's field.

```maxon
typealias Tally = int(0 to u64.max)

type Scale
	var reading as Tally

	static function create(reading Tally) returns Self
		return Self{reading: reading}
	end 'create'

	// TODO: receiver capability 'immutable self' is not supported in minimaxon
	function reported(immutable self) returns Tally
		return self.reading
	end 'reported'
end 'Scale'

function main() returns ExitCode
	let frozen = freeze(Scale.create(42))
	print("{frozen.reported()}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
42
```

<!-- test: region-operator-word-before-a-spaced-parenthesis-is-the-operator -->
`freeze (…)` with a space is the operator over a parenthesized operand, while `swap(…)` with the parenthesis adjacent stays a call to the user's own `swap`.

```maxon
typealias Tally = int(0 to u64.max)

type Box
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Box'

function swap(left Tally, right Tally) returns Tally
	return try left * 10 + right otherwise panic("out of range")
end 'swap'

function main() returns ExitCode
	let pinned = freeze(Box.create(7))
	let swapped = swap(1, right: 2)
	print("{pinned.n} {swapped}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
7 12
```

<!-- test: a-mutates-captures-clause-converts-a-capture-writing-closure -->
<!-- skip: 'mutates' is a keyword in minimaxon and cannot be used as a name; rename it -->
<!-- skip: a function type that 'mutates captures' is not supported in minimaxon -->
`Tallier` ends in `mutates captures`, so a closure that writes the record it captured converts to it, and the running total survives between the two calls. A local named `mutates` or `captures` keeps its meaning.

```maxon
typealias Tally = int(0 to u64.max)

// TODO: a function type that 'mutates captures' is not supported in minimaxon
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
	// TODO: 'mutates' is a keyword in minimaxon and cannot be used as a name; rename it
	let mutates = feed(function(n) gives total.bump(n))
	let captures = mutates + 1
	print("{mutates} {captures}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
10 11
```

<!-- test: capability-word-on-a-value-type-field-refused -->
<!-- skip: region capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
A field of a value type names no region, so a capability word before its type is not a type.

```maxon
typealias Tally = int(0 to 9)

type Gauge
	// TODO: region capability 'immutable' is not supported in minimaxon
	var reading as immutable Tally

	static function create() returns Self
		return Self{reading: 1}
	end 'create'
end 'Gauge'

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:5:17: Expected 'a type' but got 'immutable'
```

<!-- test: capability-word-on-a-function-type-parameter-refused -->
<!-- skip: parameter capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
A function type's parameter of a value type takes `mutable` and no other capability word.

```maxon
typealias Tally = int(0 to 9)

// TODO: parameter capability 'immutable' is not supported in minimaxon
typealias Visit = function(immutable Tally)

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:3:30: Expected 'a type' but got 'immutable'
```

<!-- test: capability-word-on-a-value-type-result-refused -->
<!-- skip: return type capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
A result of a value type has no region either.

```maxon
typealias Tally = int(0 to 9)

// TODO: return type capability 'immutable' is not supported in minimaxon
function one() returns immutable Tally
	return 1
end 'one'

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:4:24: Expected 'a type' but got 'immutable'
```

<!-- test: capability-word-on-a-map-key-argument-refused -->
<!-- skip: type argument capability 'immutable' is not supported in minimaxon -->
<!-- positions: moved -->
A `Map`'s key is stored by value, so no capability word qualifies it.

```maxon
typealias Tally = int(0 to 9)

type Box
	export var n as Tally

	static function create() returns Self
		return Self{n: 1}
	end 'create'
end 'Box'

// TODO: type argument capability 'immutable' is not supported in minimaxon
typealias Table = Map with immutable Tally, Box

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:12:28: Expected 'a type' but got 'immutable'
```

<!-- test: isolated-type-argument-without-the-bound-refused -->
<!-- skip: type argument capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
A generic type that does not bound its parameter by `isolated` takes no `isolated` argument.

```maxon
typealias Tally = int(0 to 9)

type Box
	export var n as Tally

	static function create() returns Self
		return Self{n: 1}
	end 'create'
end 'Box'

type Wrapper uses Element
	var item as Element
end 'Wrapper'

// TODO: type argument capability 'isolated' is not supported in minimaxon
typealias Wrapped = Wrapper with isolated Box

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:16:34: Expected 'a type' but got 'isolated'
```

<!-- test: receiver-capability-on-a-static-function-refused -->
<!-- skip: receiver capability 'immutable self' is not supported in minimaxon -->
<!-- positions: moved -->
A static function has no receiver to qualify.

```maxon
typealias Tally = int(0 to 9)

type Scale
	var reading as Tally

	// TODO: receiver capability 'immutable self' is not supported in minimaxon
	static function create(immutable self) returns Self
		return Self{reading: 1}
	end 'create'
end 'Scale'

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:7:25: Expected 'a type' but got 'immutable'
```

<!-- test: receiver-capability-on-a-free-function-refused -->
<!-- positions: moved -->
A free function has no receiver to qualify.

```maxon
typealias Tally = int(0 to 9)

function echo(mutates self) returns Tally
	return 1
end 'echo'

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:4:15: Expected 'a type' but got 'mutable'
```

<!-- test: a-capability-word-after-as-other-than-isolated-refused -->
`as` takes `isolated` before a record type and no other capability word.

```maxon
typealias Tally = int(0 to 9)

type Box
	export var n as Tally

	static function create() returns Self
		return Self{n: 1}
	end 'create'
end 'Box'

function main() returns ExitCode
	let a = Box.create()
	let b = a as mutable Box
	print("{b.n}\n")
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:14:15: Expected 'a type' but got 'mutable'
```

<!-- test: as-isolated-on-a-value-type-refused -->
<!-- skip: region capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
`as isolated` qualifies a record, and a `Tally` is a number.

```maxon
typealias Tally = int(0 to 9)

function main() returns ExitCode
	// TODO: region capability 'isolated' is not supported in minimaxon
	let n = 5 as isolated Tally
	print("{n}\n")
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:5:15: Expected 'a type' but got 'isolated'
```

<!-- test: as-isolated-outside-an-initializer-refused -->
<!-- skip: region capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
`as isolated` ends a `let` or `var` initializer; as a call argument it qualifies nothing.

```maxon
typealias Tally = int(0 to 9)

type Box
	export var n as Tally

	static function create() returns Self
		return Self{n: 1}
	end 'create'
end 'Box'

function eat(b Box)
	print("{b.n}\n")
end 'eat'

function main() returns ExitCode
	let a = Box.create()
	// TODO: region capability 'isolated' is not supported in minimaxon
	eat(a as isolated Box)
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:18:2: Expected newline but got 'eat'
```

<!-- test: as-isolated-inside-a-call-argument-of-an-initializer-refused -->
<!-- skip: parameter capability 'isolated' is not supported in minimaxon -->
<!-- skip: region capability 'isolated' is not supported in minimaxon -->
<!-- positions: moved -->
`as isolated` ends a `let` or `var` initializer; inside the call argument of one, with a `let` around it, it is refused as it is in a call statement.

```maxon
typealias Tally = int(0 to 9)

type Payload
	export var n as Tally

	static function create(n Tally) returns Self
		return Self{n: n}
	end 'create'
end 'Payload'

// TODO: parameter capability 'isolated' is not supported in minimaxon
function take(p isolated Payload) returns Tally
	return p.n
end 'take'

function main() returns ExitCode
	// TODO: region capability 'isolated' is not supported in minimaxon
	let k = take(Payload.create(1) as isolated Payload)
	print("{k}\n")
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:17:52: Expected newline but got ')'
```

<!-- test: mutable-tuple-item-refused -->
<!-- skip: tuple typealiases are not supported in minimaxon -->
<!-- positions: moved -->
A tuple item is `immutable` or unmarked.

```maxon
typealias Tally = int(0 to 9)

type Box
	export var n as Tally

	static function create() returns Self
		return Self{n: 1}
	end 'create'
end 'Box'

// TODO: tuple typealiases are not supported in minimaxon
typealias Pair = (mutable Box, Tally)

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:12:19: Expected 'a type' but got 'mutable'
```

<!-- test: isolated-alias-tuple-item-refused -->
<!-- skip: capability typealias 'isolated' is not supported in minimaxon -->
<!-- skip: tuple typealiases are not supported in minimaxon -->
<!-- positions: moved -->
An alias of an `isolated` record cannot be a tuple item either.

```maxon
typealias Tally = int(0 to 9)

type Box
	export var n as Tally

	static function create() returns Self
		return Self{n: 1}
	end 'create'
end 'Box'

// TODO: capability typealias 'isolated' is not supported in minimaxon
typealias Own = isolated Box

// TODO: tuple typealiases are not supported in minimaxon
typealias Pair = (Own, Tally)

function main() returns ExitCode
	return 0
end 'main'
```
```maxoncstderr
error E2010: <fragment>:13:19: Expected 'a type' but got 'Own'
```

<!-- test: capability-words-as-names-and-operands-compile-and-run -->
`isolated`, `immutable` and `mutable` qualify a type only where a type is expected, so a function named `isolated` is called with or without a space before its parenthesis, a parameter named `mutable` is read, and a local named `immutable` is the operand of `not`.

```maxon
typealias Tally = int(0 to u64.max)

function isolated(x Tally) returns Tally
	return try x + 1 otherwise panic("out of range")
end 'isolated'

function viaCall(x Tally) returns Tally
	return isolated(x)
end 'viaCall'

function viaOperand(x Tally) returns Tally
	return isolated(x)
end 'viaOperand'

function bump(mutable Tally) returns Tally
	return try mutable + 1 otherwise panic("out of range")
end 'bump'

function negated(flag bool) returns bool
	let immutable = flag
	return not immutable
end 'negated'

function mutable(x Tally) returns Tally
	return try x + 2 otherwise panic("out of range")
end 'mutable'

function immutable(x Tally) returns bool
	return x == 4
end 'immutable'

function viaMutable(x Tally) returns Tally
	return mutable(x)
end 'viaMutable'

function viaMutableOperand(x Tally) returns Tally
	return mutable(x)
end 'viaMutableOperand'

function viaNot(x Tally) returns bool
	return not immutable(x)
end 'viaNot'

function main() returns ExitCode
	let immutable = 1
	let readonly = 2
	print("{viaCall(5)} {viaOperand(5)} {bump(7)} {negated(true)} {immutable} {readonly} {viaMutable(5)} {viaMutableOperand(5)} {viaNot(4)}\n")
	return 0
end 'main'
```
```exitcode
0
```
```stdout
6 6 8 false 1 2 7 7 false
```
