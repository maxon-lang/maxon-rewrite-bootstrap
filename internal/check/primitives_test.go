package check

import (
	"testing"

	"minimaxon/internal/diag"
)

const primitives = `type Cell
	export var n as Count
	static function make(n Count) returns Cell
		return Cell{n: n}
	end 'make'
end 'Cell'

type Entry
	export var key as Count
	export var value as String
	static function make(key Count, value sink String) returns Entry
		return Entry{key: key, value: value}
	end 'make'
end 'Entry'

type Rack
	export var left as array[Cell]
	export var right as array[Cell]
	export var node as Handle[Cell]
	static function make(node Handle[Cell]) returns Rack
		return Rack{left: array[Cell].new(), right: array[Cell].new(), node: node}
	end 'make'
end 'Rack'

interface Shape
	function area() returns Count
end 'Shape'

function touch(c inout Cell)
	c.n = c.n + 1
end 'touch'

function bump(n inout Count)
	n = n + 1
end 'bump'

function pair(a inout Cell, b inout Cell)
	a.n = b.n
end 'pair'

function grow(xs inout array[Count], n Count)
	xs.append(n)
end 'grow'

function drop(xs inout array[Cell]) returns Count
	return xs.count()
end 'drop'

function look(c Cell, n Count) returns Count
	return c.n + n
end 'look'

function take(c sink Cell) returns Count
	return c.n
end 'take'

function make() returns array[Cell]
	return array[Cell].new()
end 'make'

function total[T implements Shape](x T) returns Count
	return x.area()
end 'total'

`

func checkPrimitives(t *testing.T, decls, body string) []*diag.CompileError {
	t.Helper()
	return checkSource(t, primitives+decls+"function main() returns ExitCode\n"+body+"\treturn 0\nend 'main'\n")
}

func TestPrimitivesAccept(t *testing.T) {
	for name, body := range map[string]string{
		"a scalar element is a value":    "\tvar a = array[Count].new()\n\ta.append(1)\n\tlet n = a[0]\n\ta[0] = n + a[0]\n\tbump(a[0])\n\tprint(\"{a[0]}\")\n",
		"change through an element":      "\tvar a = array[Cell].new()\n\ta.append(Cell.make(1))\n\ta[0].n = 3\n\ttouch(a[0])\n",
		"an index that reads the array":  "\tvar a = array[Count].new()\n\ta.append(1)\n\ta.append(a[a.count() - 1] + 1)\n\ta[a.count() - 1] = 2\n",
		"elements of two arrays":         "\tvar a = array[Cell].new()\n\tvar b = array[Cell].new()\n\ta.append(Cell.make(1))\n\tb.append(Cell.make(2))\n\tpair(a[0], b: b[0])\n",
		"elements of two fields":         "\tvar p = pool[Cell].new()\n\tvar r = Rack.make(p.insert(Cell.make(1)))\n\tr.left.append(Cell.make(1))\n\tr.right.append(Cell.make(2))\n\tpair(r.left[0], b: r.right[0])\n",
		"copy an element out":            "\tvar a = array[Cell].new()\n\ta.append(Cell.make(1))\n\tlet c = a[0].copy()\n\tprint(\"{c.n}\")\n",
		"take elements out":              "\tvar a = array[Cell].new()\n\ta.append(Cell.make(1))\n\ta.append(Cell.make(2))\n\tlet c = a.remove(0)\n\tlet last = a.pop()\n\tprint(\"{c.n} {last.isSome()}\")\n\tlet d = last.unwrap()\n\tprint(\"{d.n}\")\n",
		"match what pop returns":         "\tvar a = array[Cell].new()\n\tmatch a.pop() 'm'\n\t\tsome(c) then print(\"{c.n}\")\n\t\tnone then print(\"empty\")\n\tend 'm'\n",
		"swap two elements by copying":   "\tvar a = array[Cell].new()\n\ta.append(Cell.make(1))\n\ta.append(Cell.make(2))\n\tlet t = a[0].copy()\n\ta[0] = a[1].copy()\n\ta[1] = t\n",
		"index in a loop":                "\tvar a = array[Count].filled(3, value: 0)\n\tfor i in 0 upto a.count() 'l'\n\t\ta[i] = a[i] + 1\n\tend 'l'\n",
		"an array of arrays":             "\tvar g = array[array[Count]].new()\n\tg.append(array[Count].new())\n\tg[0].append(1)\n\tg[0][0] = g[0][0] + 1\n",
		"a map of cells":                 "\tvar m = map[String, Cell].new()\n\tm.insert(\"a\", value: Cell.make(1))\n\tif m.has(\"a\") 'h'\n\t\tm[\"a\"].n = 4\n\t\ttouch(m[\"a\"])\n\tend 'h'\n\tlet gone = m.remove(\"a\")\n\tprint(\"{gone.isSome()} {m.count()} {m.isEmpty()}\")\n",
		"a pool and its handles":         "\tvar p = pool[Cell].new()\n\tlet h = p.insert(Cell.make(1))\n\tp[h].n = 2\n\ttouch(p[h])\n\tif p.contains(h) 'c'\n\t\tlet gone = p.remove(h)\n\t\tprint(\"{gone.isSome()}\")\n\tend 'c'\n",
		"a handle is a plain value":      "\tvar p = pool[Cell].new()\n\tlet h = p.insert(Cell.make(1))\n\tlet h2 = h\n\tlet h3 = h2\n\tvar hs = array[Handle[Cell]].new()\n\ths.append(h)\n\ths.append(h2)\n\tvar by = map[Handle[Cell], Count].new()\n\tby.insert(h3, value: 1)\n\tprint(\"{h == h2} {by[h]}\")\n",
		"a list":                         "\tvar l = list[Cell].new()\n\tlet a = l.pushBack(Cell.make(1))\n\tlet b = l.pushFront(Cell.make(2))\n\tl.moveToFront(a)\n\tl[b].n = 5\n\ttouch(l[a])\n\tprint(\"{l.first().isSome()} {l.next(a).isSome()} {l.prev(b).isSome()} {l.last().isSome()} {l.contains(a)}\")\n\tlet popped = l.popBack()\n\tlet gone = l.remove(a)\n\tprint(\"{popped.isSome()} {gone.isSome()} {l.count()}\")\n",
		"a handle in a record":           "\tvar p = pool[Cell].new()\n\tlet r = Rack.make(p.insert(Cell.make(1)))\n\ttouch(p[r.node])\n",
		"copy a primitive":               "\tvar a = array[Cell].new()\n\tvar p = pool[Cell].new()\n\tvar m = map[Count, Cell].new()\n\tvar l = list[Cell].new()\n\tlet a2 = a.copy()\n\tlet p2 = p.copy()\n\tlet m2 = m.copy()\n\tlet l2 = l.copy()\n\tprint(\"{a2.count()} {p2.count()} {m2.count()} {l2.count()}\")\n",
		"read an element as an argument": "\tvar a = array[Cell].new()\n\ta.append(Cell.make(1))\n\tprint(\"{look(a[0], n: a[0].n)}\")\n",
	} {
		t.Run(name, func(t *testing.T) { expectCodes(t, checkPrimitives(t, "", body)) })
	}
}

func TestPrimitivesRejects(t *testing.T) {
	for _, tc := range []struct{ name, decls, body, code string }{
		{"move an element into a let", "", "\tvar a = array[Cell].new()\n\tlet c = a[0]\n\tprint(\"{c.n}\")\n", diag.ErrMoveOutOfField},
		{"return an element of a variable", "function f() returns Cell\n\tvar a = array[Cell].new()\n\treturn a[0]\nend 'f'\n\n", "", diag.ErrMoveOutOfField},
		{"return an element of a borrowed array", "function f(xs array[Cell]) returns Cell\n\treturn xs[0]\nend 'f'\n\n", "", diag.ErrMoveOutOfBorrow},
		{"sink an element", "", "\tvar a = array[Cell].new()\n\tprint(\"{take(a[0])}\")\n", diag.ErrMoveOutOfField},
		{"assign one element from another", "", "\tvar a = array[Cell].new()\n\ta[0] = a[1]\n", diag.ErrMoveOutOfField},
		{"move an element of a temporary", "", "\tlet c = make()[0]\n\tprint(\"{c.n}\")\n", diag.ErrMoveOutOfField},
		{"move a value out of a map", "", "\tvar m = map[Count, Cell].new()\n\tlet c = m[1]\n\tprint(\"{c.n}\")\n", diag.ErrMoveOutOfField},
		{"two elements as inout arguments", "", "\tvar a = array[Cell].new()\n\tpair(a[0], b: a[1])\n", diag.ErrExclusivity},
		{"the array and one of its elements", "", "\tvar a = array[Count].new()\n\tgrow(a, n: a[0])\n", diag.ErrExclusivity},
		{"append an element of the array", "", "\tvar a = array[Count].new()\n\ta.append(a[0])\n", diag.ErrExclusivity},
		{"insert an element of the array", "", "\tvar a = array[Count].new()\n\ta.insert(a[0], at: 0)\n", diag.ErrExclusivity},
		{"remove while an element is borrowed", "", "\tvar a = array[Cell].new()\n\tprint(\"{look(a[0], n: drop(a))}\")\n", diag.ErrExclusivity},
		{"change a pool while an element is borrowed", "", "\tvar p = pool[Cell].new()\n\tlet h = p.insert(Cell.make(1))\n\tpair(p[h], b: p[h])\n", diag.ErrExclusivity},
		{"change the array in an arm that borrows its element", "", "\tvar a = array[Option[Cell]].new()\n\tmatch a[0] 'm'\n\t\tsome(c) then a.append(Option[Cell].none)\n\t\tnone then print(\"none\")\n\tend 'm'\n", diag.ErrExclusivity},
		{"assign an element of a let", "", "\tlet a = array[Count].new()\n\ta[0] = 1\n", diag.ErrAssignToImmutable},
		{"append to a let", "", "\tlet a = array[Count].new()\n\ta.append(1)\n", diag.ErrAssignToImmutable},
		{"change an element of a borrowed array", "function f(a array[Cell])\n\ttouch(a[0])\nend 'f'\n\n", "", diag.ErrAssignToImmutable},
		{"change an element of a temporary", "", "\ttouch(make()[0])\n", diag.ErrAssignToImmutable},
		{"index with a bool", "", "\tvar a = array[Count].new()\n\tprint(\"{a[true]}\")\n", diag.ErrTypeMismatch},
		{"index a pool with a number", "", "\tvar p = pool[Cell].new()\n\tprint(\"{p[0].n}\")\n", diag.ErrTypeMismatch},
		{"index a map with the wrong key", "", "\tvar m = map[String, Count].new()\n\tprint(\"{m[1]}\")\n", diag.ErrTypeMismatch},
		{"index a number", "", "\tlet n = 3 as Count\n\tprint(\"{n[0]}\")\n", diag.ErrTypeMismatch},
		{"index a handle", "", "\tvar p = pool[Cell].new()\n\tlet h = p.insert(Cell.make(1))\n\tprint(\"{h[0]}\")\n", diag.ErrTypeMismatch},
		{"index with two values", "", "\tvar a = array[Count].new()\n\tprint(\"{a[0, 1]}\")\n", diag.ErrTypeMismatch},
		{"a handle of another pool", "", "\tvar p = pool[Cell].new()\n\tvar q = pool[Entry].new()\n\tlet h = q.insert(Entry.make(1, value: \"a\"))\n\tprint(\"{p[h].n}\")\n", diag.ErrTypeMismatch},
		{"a record as a map key", "", "\tvar m = map[Cell, Count].new()\n", diag.ErrMapKey},
		{"a type parameter as a map key", "function f[K](m map[K, Count])\n\tprint(\"{m.count()}\")\nend 'f'\n\n", "", diag.ErrMapKey},
		{"an unknown method", "", "\tvar a = array[Count].new()\n\ta.push(1)\n", diag.ErrUnknownFunction},
		{"a method named without a call", "", "\tvar a = array[Count].new()\n\tprint(\"{a.count}\")\n", diag.ErrUnsupported},
		{"the wrong number of arguments", "", "\tvar a = array[Count].new()\n\ta.append()\n", diag.ErrCallArgCountMismatch},
		{"an unlabelled second argument", "", "\tvar a = array[Count].new()\n\ta.insert(1, 0)\n", diag.ErrCallArgMissingLabel},
		{"an element of the wrong type", "", "\tvar a = array[Count].new()\n\ta.append(\"x\")\n", diag.ErrTypeMismatch},
		{"an Option used after unwrap", "", "\tvar a = array[Cell].new()\n\tlet o = a.pop()\n\tlet c = o.unwrap()\n\tprint(\"{o.isSome()} {c.n}\")\n", diag.ErrUseAfterMove},
		{"a primitive given to a bounded parameter", "", "\tprint(\"{total(array[Count].new())}\")\n", diag.ErrBoundNotMet},
		{"an array without a type argument", "", "\tlet a = array.new()\n", diag.ErrCannotInfer},
		{"an array type without a type argument", "function f(a array) returns Count\n\treturn 0\nend 'f'\n\n", "", diag.ErrTypeArgumentCount},
		{"a literal of a primitive", "", "\tlet a = array{}\n", diag.ErrUnknownType},
		{"a handle has no methods", "", "\tvar p = pool[Cell].new()\n\tlet h = p.insert(Cell.make(1))\n\tprint(\"{h.count()}\")\n", diag.ErrUnknownFunction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectCodes(t, checkPrimitives(t, tc.decls, tc.body), tc.code)
		})
	}
}

// A cache that drops the entry used longest ago: a list in order of use, and a map from each key to the handle of its
// node. Neither structure points into the other; the handle is only a key.
const lruCache = `type Entry
	export var key as Count
	export var value as String
	static function make(key Count, value sink String) returns Entry
		return Entry{key: key, value: value}
	end 'make'
end 'Entry'

type Cache
	var order as list[Entry]
	var index as map[Count, Handle[Entry]]
	var capacity as Count
	static function make(capacity Count) returns Cache
		return Cache{order: list[Entry].new(), index: map[Count, Handle[Entry]].new(), capacity: capacity}
	end 'make'
	function get(inout self, key Count) returns Option[String]
		if not index.has(key) 'miss'
			return Option[String].none
		end 'miss'
		let node = index[key]
		order.moveToFront(node)
		return Option.some(order[node].value.copy())
	end 'get'
	function put(inout self, key Count, value sink String)
		if index.has(key) 'hit'
			let node = index[key]
			order[node].value = value
			order.moveToFront(node)
			return
		end 'hit'
		if index.count() == capacity 'full'
			match order.popBack() 'evict'
				some(oldest) then _ = index.remove(oldest.key)
				none then return
			end 'evict'
		end 'full'
		let node = order.pushFront(Entry.make(key, value: value))
		index.insert(key, value: node)
	end 'put'
end 'Cache'

`

// A stack of nodes that link to each other by handle, which a pool owns.
const handleStack = `type Node
	var value as Count
	var next as Option[Handle[Node]]
	static function make(value Count, next sink Option[Handle[Node]]) returns Node
		return Node{value: value, next: next}
	end 'make'
end 'Node'

type Stack
	var nodes as pool[Node]
	var top as Option[Handle[Node]]
	static function make() returns Stack
		return Stack{nodes: pool[Node].new(), top: Option[Handle[Node]].none}
	end 'make'
	function push(inout self, value Count)
		let node = Node.make(value, next: top.copy())
		top = Option.some(nodes.insert(node))
	end 'push'
	function pop(inout self) returns Option[Count]
		if not top.isSome() 'empty'
			return Option[Count].none
		end 'empty'
		let handle = top.copy().unwrap()
		let node = nodes.remove(handle).unwrap()
		top = node.next.copy()
		return Option.some(node.value)
	end 'pop'
	function peek() returns Option[Count]
		if not top.isSome() 'empty'
			return Option[Count].none
		end 'empty'
		return Option.some(nodes[top.copy().unwrap()].value)
	end 'peek'
end 'Stack'

`

// Generic code over the primitives is checked once, like any other generic code.
const genericPrimitives = `function moveFirst[T](source inout array[T], target inout array[T])
	if not source.isEmpty() 'some'
		target.append(source.remove(0))
	end 'some'
end 'moveFirst'

function store[V](m inout map[String, V], value sink V)
	m.insert("key", value: value)
end 'store'

function put[T](p inout pool[T], item sink T) returns Handle[T]
	return p.insert(item)
end 'put'

function back[T](l inout list[T], item sink T) returns Handle[T]
	let node = l.pushBack(item)
	l.moveToFront(node)
	return node
end 'back'

function swapFirst[T](a inout array[T], b inout array[T])
	let t = a[0].copy()
	a[0] = b[0].copy()
	b[0] = t
end 'swapFirst'

function find[T](p pool[T], handle Handle[T]) returns Option[T]
	if p.contains(handle) 'live'
		return Option.some(p[handle].copy())
	end 'live'
	return Option[T].none
end 'find'

function nested[T](grid inout array[array[T]], item sink T)
	grid[0].append(item)
end 'nested'

`

func TestAnLruCacheChecks(t *testing.T) {
	expectCodes(t, checkSource(t, lruCache+`function main() returns ExitCode
	var cache = Cache.make(2)
	cache.put(1, value: "one")
	cache.put(2, value: "two")
	match cache.get(1) 'm'
		some(v) then print(v)
		none then print("miss")
	end 'm'
	return 0
end 'main'
`))
}

func TestAStackOfHandlesChecks(t *testing.T) {
	expectCodes(t, checkSource(t, handleStack+`function main() returns ExitCode
	var s = Stack.make()
	s.push(1)
	s.push(2)
	match s.pop() 'm'
		some(v) then print("{v}")
		none then print("empty")
	end 'm'
	return 0
end 'main'
`))
}

func TestGenericCodeOverPrimitivesChecks(t *testing.T) {
	expectCodes(t, checkSource(t, genericPrimitives+`function main() returns ExitCode
	var a = array[Count].new()
	var b = array[Count].new()
	a.append(1)
	b.append(2)
	moveFirst(a, target: b)
	a.append(3)
	swapFirst(a, b: b)
	var m = map[String, Count].new()
	store(m, value: 1)
	var p = pool[Count].new()
	let h = put(p, item: 4)
	var l = list[Count].new()
	let n = back(l, item: 5)
	var grid = array[array[Count]].new()
	grid.append(array[Count].new())
	nested(grid, item: 6)
	print("{find(p, handle: h).isSome()} {l.contains(n)}")
	return 0
end 'main'
`))
}
