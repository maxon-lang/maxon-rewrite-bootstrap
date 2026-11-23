---
feature: one-call-open-yields
status: experimental
keywords: [isolated, one-call open, window, yield, await, coroutine, E3205]
category: semantics
---

# A one-call open neither waits nor yields

## Documentation

A call that passes an `isolated` value in an unmarked position, as receiver or as argument, opens the value for that call. No sibling coroutine runs while the call is open, so a callee that awaits a coroutine, or reaches a call that waits or yields, is refused (E3205) exactly as inside a `modify` block. A callee that only reads and writes, or sleeps, is accepted.

## Tests

<!-- test: error.a-one-call-open-around-a-method-that-awaits-a-coroutine-is-refused -->
<!-- skip: language difference: the regions of maxon, its immutable, isolated and unique values and its modify and acquire blocks, are replaced by owned values, borrows and frozen values, so the rule behind the code does not exist -->
<!-- positions: moved -->
`spin` awaits its helper coroutine, which could not run while `slot` is open.

```maxon
typealias Tally = int(0 to u64.max)

type Slot
	var count as Tally

	static function create() returns Slot
		return Self{count: 0}
	end 'create'

	function spin() returns Tally
		let helper = async delayed()
		let got = await helper
		self.count = got
		return got
	end 'spin'
end 'Slot'

function delayed() returns Tally
	sleep(10)
	return 7
end 'delayed'

function runSlot(consumes slot Slot) returns Tally
	return slot.spin()
end 'runSlot'

function main() returns ExitCode
	let slot = Slot.create() as Slot
	let work = async runSlot(slot)
	let laps = await work
	print("laps={laps}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3205: <fragment>:27:14: cannot call 'spin' inside 'slot'; it may yield (it awaits a coroutine at line 13). Call it before or after the block
```

<!-- test: error.a-one-call-open-reaches-an-await-through-a-helper -->
<!-- skip: language difference: the regions of maxon, its immutable, isolated and unique values and its modify and acquire blocks, are replaced by owned values, borrows and frozen values, so the rule behind the code does not exist -->
<!-- positions: moved -->
The await is one call down: `spin` calls `relay`, which awaits.

```maxon
typealias Tally = int(0 to u64.max)

type Slot
	var count as Tally

	static function create() returns Slot
		return Self{count: 0}
	end 'create'

	function spin() returns Tally
		return relay()
	end 'spin'
end 'Slot'

function relay() returns Tally
	let helper = async delayed()
	return await helper
end 'relay'

function delayed() returns Tally
	sleep(10)
	return 7
end 'delayed'

function runSlot(consumes slot Slot) returns Tally
	return slot.spin()
end 'runSlot'

function main() returns ExitCode
	let slot = Slot.create() as Slot
	let work = async runSlot(slot)
	let laps = await work
	print("laps={laps}\n")
	return 0
end 'main'
```
```maxoncstderr
error E3205: <fragment>:29:14: cannot call 'spin' inside 'slot'; it may yield (it calls 'relay', which awaits a coroutine at line 19). Call it before or after the block
```

<!-- test: a-one-call-open-around-a-method-that-only-sleeps-runs -->
<!-- skip: language difference: a let, a let field, a let parameter and a match binding are read-only places, where maxon's let only forbids rebinding the name -->
Sleeping blocks the strand for the call and switches to no sibling, so it is not a yield.

```maxon
typealias Tally = int(0 to u64.max)

type Slot
	var count as Tally

	static function create() returns Slot
		return Self{count: 0}
	end 'create'

	function quiet() returns Tally
		sleep(5)
		self.count = self.count + 3
		return self.count
	end 'quiet'
end 'Slot'

function runSlot(consumes slot Slot) returns Tally
	return slot.quiet()
end 'runSlot'

function main() returns ExitCode
	let slot = Slot.create() as Slot
	let work = async runSlot(slot)
	let laps = await work
	print("laps={laps}\n")
	return 0
end 'main'
```
```stdout
laps=3
```
```exitcode
0
```
