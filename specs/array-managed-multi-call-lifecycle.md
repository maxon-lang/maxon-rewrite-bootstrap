---
feature: array-managed-multi-call-lifecycle
status: stable
keywords: [array, struct, enum, managed, refcount, push, reassign, lifecycle]
category: memory
---
# Array of Managed Structs: Multi-Call Lifecycle

## Documentation

When an array of managed structs is populated across multiple function calls, and then the field is reassigned, the refcount cleanup must handle all elements correctly. This tests the interaction between:

- Push with potential buffer realloc/grow
- Function-scope cleanup of temporary variables
- Array field reassignment triggering old array destructor
- `~ManagedElements` iterating freed backing buffer

## Tests

<!-- test: managed-array-push-across-calls -->
### Push managed elements across separate function calls, then reassign
Simulates the pattern from the query engine: recordDependency pushes elements
between clearDepsFor calls.
```maxon
union QueryKey
	sourceFile(path String)
	tokens(path String)
	allModule
	codeResult
end 'QueryKey'

type Dependency
	export var dependent as QueryKey
	export var dependency as QueryKey

	static function create(dependent QueryKey, dependency QueryKey) returns Self
		return Self{dependent: dependent.clone(), dependency: dependency.clone()}
	end 'create'
end 'Dependency'

typealias DependencyArray = Array with Dependency

type Database
	export var dependencies as DependencyArray

	static function create(dependencies DependencyArray) returns Self
		return Self{dependencies: dependencies.clone()}
	end 'create'
end 'Database'

function addDep(mutates db Database, dep Dependency)
	db.dependencies.push(dep.clone())
end 'addDep'

function clearAllModule(mutates db Database)
	var newDeps = DependencyArray.create(0)
	for dep in db.dependencies 'scan'
		match dep.dependent 'check'
			sourceFile then newDeps.push(dep.clone())
			tokens then newDeps.push(dep.clone())
			allModule then continue
			codeResult then newDeps.push(dep.clone())
		end 'check'
	end 'scan'
	db.dependencies = newDeps
end 'clearAllModule'

function main() returns ExitCode
	var db = Database.create(DependencyArray.create(0))
	// First cycle: add a dep, then clear
	addDep(db, dep: Dependency.create(QueryKey.sourceFile("a.maxon long enough for heap"), dependency: QueryKey.tokens("a.maxon long enough for heap")))
	clearAllModule(db)
	// Second cycle: add another dep (now array has 2 elements total)
	addDep(db, dep: Dependency.create(QueryKey.allModule, dependency: QueryKey.sourceFile("b.maxon long enough for heap")))
	clearAllModule(db)
	// Third cycle: add more deps, then clear for allModule
	addDep(db, dep: Dependency.create(QueryKey.allModule, dependency: QueryKey.sourceFile("c.maxon long enough for heap")))
	clearAllModule(db)
	return try db.dependencies.count() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
1
```

<!-- test: managed-array-grow-then-reassign -->
### Array grows past initial capacity, then field is reassigned
Push enough managed elements to trigger multiple buffer reallocations,
then reassign the array field so the old array's destructor must clean up all elements.
```maxon
typealias Integer = int(i64.min to i64.max)

union Tag
	label(name String)
	index(n Integer)
end 'Tag'

type Entry
	export var tag as Tag

	static function create(tag Tag) returns Self
		return Self{tag: tag.clone()}
	end 'create'
end 'Entry'

typealias EntryArray = Array with Entry

type Store
	export var entries as EntryArray

	static function create(entries EntryArray) returns Self
		return Self{entries: entries.clone()}
	end 'create'
end 'Store'

function addEntry(mutates store Store, entry Entry)
	store.entries.push(entry.clone())
end 'addEntry'

function replaceAll(mutates store Store)
	store.entries = EntryArray.create(0)
end 'replaceAll'

function main() returns ExitCode
	var store = Store.create(EntryArray.create(0))
	// Push enough to trigger multiple reallocations (initial cap is usually 4)
	addEntry(store, entry: Entry.create(Tag.label("entry zero long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry one long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry two long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry three long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry four long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry five long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry six long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry seven long enough for heap allocation")))
	addEntry(store, entry: Entry.create(Tag.label("entry eight long enough for heap allocation")))
	replaceAll(store)
	return try store.entries.count() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
0
```

<!-- test: managed-array-interleaved-push-clear -->
### Interleaved push and clear cycles on same array field
Multiple rounds of push-then-reassign, each round the old array is freed.
```maxon
union Key
	file(path String)
	module
end 'Key'

type Dep
	export var source as Key
	export var target as Key

	static function create(source Key, target Key) returns Self
		return Self{source: source.clone(), target: target.clone()}
	end 'create'
end 'Dep'

typealias DepArray = Array with Dep

type State
	export var deps as DepArray

	static function create(deps DepArray) returns Self
		return Self{deps: deps.clone()}
	end 'create'
end 'State'

function record(mutates state State, dep Dep)
	state.deps.push(dep.clone())
end 'record'

function clearFor(mutates state State)
	var kept = DepArray.create(0)
	for dep in state.deps 'scan'
		match dep.source 'check'
			module then continue
			file then kept.push(dep.clone())
		end 'check'
	end 'scan'
	state.deps = kept
end 'clearFor'

function main() returns ExitCode
	var state = State.create(DepArray.create(0))
	record(state, dep: Dep.create(Key.file("x.maxon long enough for heap"), target: Key.module))
	clearFor(state)
	record(state, dep: Dep.create(Key.module, target: Key.file("y.maxon long enough for heap")))
	clearFor(state)
	record(state, dep: Dep.create(Key.module, target: Key.file("z.maxon long enough for heap")))
	clearFor(state)
	return try state.deps.count() as ExitCode otherwise panic("out of range")
end 'main'
```
```exitcode
1
```
