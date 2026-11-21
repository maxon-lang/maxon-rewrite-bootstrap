---
feature: generic-module-merge-crash
status: experimental
keywords: [generics, typealias, inner-alias, interface, merge, uses, with]
category: type-system
---

# Generic Module with Interface Conformance and Merge

## Tests

### Generic module with merge extension

<!-- test: generic-module-merge -->
<!-- skip: a block handler ('otherwise (e) 'label'') on a try expression is not supported in minimaxon; use a try block -->
<!-- skip: alias subtyping (implements on typealias) is not supported in minimaxon -->
<!-- skip: extension blocks are not supported in minimaxon -->
<!-- skip: generic interfaces (uses on interface) are not supported in minimaxon -->
<!-- skip: nested typealias declarations are not supported in minimaxon -->
```maxon
// TODO: alias subtyping (implements on typealias) is not supported in minimaxon
typealias Count = int(0 to u64.max) implements ElementIndex

typealias Integer = int(i64.min to i64.max)

// TODO: generic interfaces (uses on interface) are not supported in minimaxon
interface Mergeable uses Func
	function itemCount() returns Count
	function getItem(index Count) returns Func
	function pushItem(mutates self, mutates item Func)
	function opsCount() returns Count
	function appendOps(mutates self, source Self)
	function cloneItem(src Func) returns Func
end 'Mergeable'

// TODO: extension blocks are not supported in minimaxon
extension Mergeable
	function merge(source Self)
		self.appendOps(source)
		for i in 0 upto source.itemCount() 'eachItem'
			let src = source.getItem(i)
			let copy = self.cloneItem(src)
			self.pushItem(copy)
		end 'eachItem'
	end 'merge'
end 'Mergeable'

type Wrapper
	export var value as Integer

	static function create(value Integer) returns Self
		return Self{value: value}
	end 'create'
end 'Wrapper'

typealias WrapperArray = Array with Wrapper

type Container uses Op implements Mergeable with Wrapper
	// TODO: nested typealias declarations are not supported in minimaxon
	typealias OpArray = Array with Op
	export var ops as OpArray
	export var items as WrapperArray

	static function create() returns Self
		return Self{ops: OpArray.create(0), items: WrapperArray.create(0)}
	end 'create'

	function opsCount() returns Count
		return self.ops.count()
	end 'opsCount'

	function appendOps(source Self)
		self.ops.append(source.ops)
	end 'appendOps'

	function itemCount() returns Count
		return self.items.count()
	end 'itemCount'

	function getItem(index Count) returns Wrapper
		// TODO: a block handler ('otherwise (e) 'label'') on a try expression is not supported in minimaxon; use a try block
		return try self.items.get(index) otherwise 'unreachable'
			panic("getItem: index out of bounds")
		end 'unreachable'
	end 'getItem'

	function pushItem(mutates item Wrapper)
		self.items.push(item)
	end 'pushItem'

	function cloneItem(src Wrapper) returns Wrapper
		return Wrapper.create(src.value)
	end 'cloneItem'

	function pushOp(op Op)
		self.ops.push(op)
	end 'pushOp'
end 'Container'

typealias IntContainer = Container with Integer

typealias ExitCode = int(0 to 125)

function main() returns ExitCode
	let a = IntContainer.create()
	a.pushOp(10)
	a.pushOp(20)
	let first = Wrapper.create(99)
	a.pushItem(first)
	let b = IntContainer.create()
	b.merge(a)
	if b.opsCount() == 2 and b.itemCount() == 1 'check'
		let item = b.getItem(0)
		if item.value == 99 'valueCheck'
			return 0
		end 'valueCheck'
	end 'check'
	return 1
end 'main'
```
```exitcode
0
```
