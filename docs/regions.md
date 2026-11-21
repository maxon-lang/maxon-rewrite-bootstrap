# Memory model: owned values, runtime primitives and behaviours

Status: steps 1 to 5 (layer 1 checks, generics, the primitives' signatures, the tracking interpreter with the
program generator, and immutable values, cowns and behaviours, see "Steps") and native freeing are implemented in
minimaxon; native code for `map`, `list`, `pool`, frozen values and behaviours, are not. It combines published designs, each where it fits best:

- **Inside a green thread: owned values, as in Hylo's mutable value semantics.** Racordon et al.,
  "Implementation Strategies for Mutable Value Semantics" (JOT 2022), the Hylo language tour
  (docs.hylo-lang.org) and specification (github.com/hylo-lang/specification).
- **Structures with internal pointers: runtime primitives**, as maxon already has (array, map, a linked list),
  plus a Pool with generational handles, after Rust's `slotmap`.
- **Between threads: Verona's behaviours.** Cheeseman et al., "When Concurrency Matters: Behaviour-Oriented
  Concurrency" (OOPSLA 2023).

Where a source leaves something open, or where following it would need a run-time check, this document says so
and gives minimaxon's choice. Changes to the borrowed designs wait until they work here.

## Goals

- **Sound.** An accepted program never has a data race, never uses freed memory, and never writes through a
  reference that promises immutability.
- **Compile time only.** No check in a compiled program is part of the safety argument.
- **No run-time overhead for data used within its own green thread.** No counting, no checks, no locks, no
  collector: every value is freed at a point the compiler knows. Costs are paid only where data crosses a thread:
  a count when shared immutable data gains an owner in another thread, and scheduling when a behaviour acquires
  cowns.
- **Little trusted code.** The runtime primitives are the only code whose safety the compiler takes on trust, and
  there are few of them: a new data structure is built from them, not added to them.
- **Follow the sources.** Each rule maps to a rule of one of the three designs, so their soundness arguments are
  ours, and a disagreement is a bug, not a new ruling.
- **Local.** Every check reads one function and the signatures it calls.

## Why the maxon design grew to fifty rulings

Maxon kept capabilities out of reference types: a value was created mutable, and isolation was recovered
afterwards from facts about its history (freshness, "clean inputs", closed types, holds through calls). Each fact
is provenance, and every construct that moves a value had to carry it: match payloads, conds over paths,
by-reference parameters, calls, loop headers. Each construct found by a red team became a ruling, and generics had
no answer to "what capability is `T`". Here, ownership and borrowing are explicit in every binding and parameter,
and the rules come from designs that have a calculus.

**The risk this design carries** is the law of exclusivity (below). Maxon struggled with exactly that shape:
mutable parameters, evaluation-order rules, a unique path passed beside its root (ruling 45.6). The difference is
that the rules are Hylo's, not grown case by case. Hylo's own laws for projections are still being refined
(hylo-lang discussion #1868), so that part is followed with care and tested hardest.

## The layers

| Layer | Model | Freed |
|---|---|---|
| Inside a green thread | Every value has one owner; other access is a scoped borrow (Hylo) | when its owner dies, known statically |
| Structures with internal pointers | Runtime primitives that own their elements; handles to elements are keys | an element when it is removed, exactly |
| Between threads | Owned values move; immutable data is shared and counted; cowns and behaviours (Verona) | shared immutable data at its last owner |

Why primitives and not Verona regions for lists and graphs: inside a region objects point to each other freely,
so the compiler cannot tell when an unlinked node becomes unreachable; it is freed only with the whole region. A
long-lived structure that adds and removes nodes (a cache, a queue, a document) would grow until it is dropped,
unless a collector or reference counting ran inside the thread, which the goals rule out. A primitive frees each
element exactly when it is removed.

## Layer 1: owned values

### Ownership

- **Every value has exactly one owner**: a scope (a local), an object (a field), or a collection (a slot).
- Binding or passing a value **moves** it, so the old name cannot be used (the existing move check, `E3102`).
  A copy is explicit: `x.clone()`.
- **A value is freed when its owner dies**: a local at the end of its scope, a field with its object, an element
  with its collection. Each point is known when compiling.
- **Collections own their elements.** Adding moves the element in; `remove` moves it out to the current scope,
  which frees it at its end unless it is moved on (see "Runtime primitives").
- **Nothing stores a reference that is not an owner.** So owned values form a tree, and a value together with
  everything it owns is isolated: nothing outside points into it. Moving it to another thread needs no further
  proof.

The cost: back-pointers, cross-links and graphs are not stored references. They are handles into a pool, keys
into a map, or indices.

### Borrows

A borrow is access to a value without owning it. Borrows are second-class: they live in parameters, locals bound
from a projection, and expressions; they are never stored in a field, never returned, and never outlive the scope
that made them. So no borrow outlives its owner, and no lifetimes appear in types.

Parameters say how they take their argument (Hylo's four conventions):

| Convention | Meaning |
|---|---|
| `let` (the default) | an immutable borrow |
| `mutates` | a mutable borrow; the callee may change the value, and the caller sees the change |
| `consumes` | ownership moves to the callee |
| `sets` | the callee initialises an uninitialised value |

A **projection** is a borrow of a part: a subscript or accessor yields an element or field as `let`, `mutates` or
`consumes`, depending on how it is used (`list[i].count = 3` projects element `i` as `mutates`). The projection lasts as
long as the expression or block that uses it.

`mutates` is Hylo's `inout`, `consumes` its `sink` and `sets` its `set`. Each is written before the parameter's name,
as a verb whose subject is the function: `push(consumes item T)` reads "push consumes item".

### The law of exclusivity

While a `mutates`, `consumes` or `sets` access to a value is active, no other access to a value that overlaps it is
active. Without it, a scoped borrow could still dangle within one call:

```
f(list, list[i])     // f mutates list and borrows one element
                      // if f removes element i, the borrow points to freed memory: refused
```

- **Paths overlap** when one is a prefix of the other: `a` and `a.f` overlap, `a.f` and `a.g` do not, and a
  collection overlaps every one of its elements (`list[i]` and `list[j]` overlap even when `i ≠ j`).
- It is checked statically, per function: the arguments of one call against each other, and each active
  projection against the accesses made while it lasts.
- Two elements of one collection cannot be mutably borrowed at once: one is swapped or removed first.

### Choices made in step 1

Where this document was open, minimaxon does the following. Hylo's rules decide anything not listed.

- **What moves.** Every value of a record, union or `String` type moves; a scalar (an integer, a bool, an enum)
  is copied. Binding, assigning, passing to `consumes`, returning, storing in a field or a union payload, and giving a
  value from a conditional or a match arm are the moving positions. A fresh value needs no move. Reading, passing
  to a `let` or `mutates` parameter, matching and interpolating only borrow. A conditional or match arm always gives
  a fresh value, so a place in a branch is moved even where the whole expression is only borrowed; write
  `.clone()` there when a borrow is meant.
- **Partial moves.** A field may be moved out of an owned value (a local, a `consumes` parameter, `consumes self`),
  at any depth through fields, as Hylo allows when the whole is consumed or reinitialised; a field of a borrowed value,
  an element and a part of a frozen value or of a temporary are never moved out, and `clone()` is the way to take
  them. A fact about a local says which of its fields are gone, so a use of the whole is refused until they are
  assigned again, and what is left is dropped where the local ends.
- **Capabilities and destructors.** A type may be unable to be copied (`is noncopyable`) or dropped (it declares a
  destructor, `function commit(destructs self)`), and a part that cannot be makes its owner unable too. A value that
  cannot be dropped must be ended explicitly: forgetting it at the end of a scope, over an assignment or in a discard
  is `E3919`, and only the type's own destructors take it apart (`E3922`). This is the compile-time form of Vale's
  linear types, and it lets a value that must be closed (a transaction, a socket) travel through the same moves as
  any other. The README's "Capabilities and destructors" has the rules.
- **Receivers.** An instance method is `function m(self…)` with no marker for a `let` receiver, `mutates self` for a
  mutable one and `consumes self` for one that consumes the value. The region keywords the grammar had (`mutable`,
  `immutable`, `isolated`, `consuming`) are gone.
- **`consumes` is owned, not mutable.** A `consumes` parameter, like a `let` local, cannot be assigned or have a field
  assigned; it is moved into a `var` to change it. This needs no copy of the argument and no slot for the
  parameter, and Hylo's rule that a `sink` can be changed adds nothing to the soundness argument.
- **`sets` takes a variable with no value.** `var x as T` declares one. Hylo also lets `set` replace a value that is
  present; here the argument must have no value, so initialisation never has to drop anything. The callee starts
  with the parameter uninitialised, may pass it on as a `sets` argument, and must have assigned it on every path
  that returns.
- **Projections.** There are no local projection bindings (`mutates r = a.f`). The borrows are parameters, call
  arguments and the payload bindings of a `match`. A payload that is not a scalar is a `let` projection of the
  subject for the arm: it cannot be assigned or moved, and the subject cannot be assigned, moved or passed
  `mutates` while the arm runs (the "active projection" rule). A scalar payload is copied into the binding.
- **Exclusivity at a call.** Every argument that names a place counts, whatever its convention or type: an
  `mutates`, `consumes` or `sets` argument may not overlap another argument or the receiver, and that includes a `let`
  scalar (`f(n, mutates n)` is refused). A computed argument (`a.f.clone()`, a call) names no place and never
  overlaps. Paths are a root followed by field steps (`paths.go`); `a[i]` is a second step kind that overlaps
  every other element of its collection (step 3).
- **Mutation needs a mutable path.** Assignment and a `mutates` argument need a `var`, a `mutates` or `sets`
  parameter, or `mutates self` as the root, and a `var` field at every step. A `let` field blocks the whole path
  below it. A `mutates` argument must name a place; a temporary cannot be mutated.
- **`clone()`.** A built-in method on every type, taking nothing. It is deep: a record or union is allocated again,
  and its fields copied in turn. A string that is not a literal is allocated again; a literal lives in the executable, is never freed, and
  is its own copy.

### Immutable values

`freeze(consumes x)` makes a value immutable. Inside a thread it is still owned once and borrowed freely, at no cost.
`share(x)` makes another owner of an immutable value and increments its count; that is only needed to give it to
another thread while keeping it, so counts change only when data crosses. The count is one per frozen
value, not per object inside it, and is atomic. Frozen values refer only to values frozen before them, so counts
form a DAG and never leak. String literals and module-level constants live in the executable image, are marked
immortal, and are never counted. Implemented in step 5 as the type `frozen with T`; see "Choices made in step 5".

## Layer 2: runtime primitives

Structures whose elements point to each other are runtime primitives. Each owns its elements, frees an element
exactly when it is removed, keeps its internal pointers private, and hands out only borrows (projections) and
handles. Their implementations are the trusted code of the system, so they are few and heavily tested.

| Primitive | Holds |
|---|---|
| `array` | elements in order, indexed |
| `map` | elements by key |
| `list` | a doubly linked list, one allocation per node (maxon's `__ManagedList`) |
| `pool` | elements addressed by generational handles |

The stdlib's `Array`, `Map` and `List` are thin wrappers over them, as in maxon.

### Handles are keys

A handle names an element without owning it, so it can be stored anywhere: in a field, in a map, in another
element. Using one is a lookup that can fail, like a map miss: if the element was removed, the lookup finds
nothing. It never reaches freed memory, because the pool checks the handle's generation against the slot's, so
a slot reused for a new element does not answer to an old handle. A lookup costs about what a bounds-checked array
access costs, and its failure is part of the operation's meaning, not a safety net.

The pool is what makes user-defined pointer structures safe without adding a primitive for each: a linked list is
a pool of nodes whose links are handles, a graph is a pool of nodes whose edges are handles, and an LRU cache is a
list plus a map from keys to node handles. Each element is freed when it is removed.

Projections into a primitive follow the law of exclusivity like any other path: an element borrowed `mutates`
overlaps its container, so the container cannot remove it while the borrow lasts.

### Choices made in step 3

Where this document was open, minimaxon does the following. The operations and their rules are in the README
("Primitives"); these are the decisions behind them.

- **The signatures are the rules.** An operation is a generic method that no source declares, with a receiver
  convention and conventions on its parameters, and a call goes through the same typing, inference, borrowing,
  moving and exclusivity code as a call of any method. Adding an element is a `consumes` parameter. Removing is a return
  value, which is owned. `mutates self` on everything that changes the structure is what makes "a container method
  overlaps any projection of its container" follow from the exclusivity rule, with no rule of its own.
- **Results that can miss throw.** An operation that can miss (`a.remove(i)`, `p.remove(h)`, `l.next(h)`,
  `m.replace(k, value: v)`) returns the element itself, owned, and throws maxon's `ArrayError` or `MapError` on a miss,
  so no value is left partly moved and nothing is allocated for the answer. A miss that is a bug and not an expected
  answer (`a[i]`, `m[k]`, `p[h]`) traps instead.
- **A subscript is a projection, an element step in the path.** `a[i]` is typed as the element and is never an owned
  value, so binding, returning or sinking it is the existing `E3901` (`E3900` under a borrowed root); no new code.
  Assigning through it needs a mutable root like a field does. Every element step overlaps every other element step
  of the same collection, which is a decision about indexes that cannot be compared before the program runs; it
  also makes `a[i]` overlap `a`. Index, key and handle are values read before the call.
- **A borrow is held until the call.** Taking the address of `a[i]` for a `mutates` argument, or loading an element
  for a `let` one, happens when the argument is evaluated, and the call runs after the arguments after it. So an
  argument that is a place stays borrowed through the rest of the call's arguments, and the target of an
  assignment through a field or an element stays borrowed while its value is computed; a nested call that changes an
  overlapping place is refused. The same holds wherever an operand is evaluated to a borrow and used after another
  operand has run: the left side of a string comparison (`a[0] == wipe(a)` loads the string, then `wipe` frees it, then
  the compare reads it), the collection of a subscript while its index is computed (`rows[0].cells[shrink(rows)]`
  loads the row's array, then `shrink` frees the row, then the element's address is taken from it), and the first
  part of an interpolated string, which is joined with the second only once the second exists. Every later part is
  joined as soon as it is evaluated and a scalar is a value when it is read, so neither is held. This is the "active
  projection against the accesses made while it lasts" check of the exclusivity section, extended from match
  bindings to arguments, assignments, operands, subscripts and string parts, and it closes the gap where
  `f(mutates a[i], g(mutates a))` took an address that `g` could invalidate. It needs no run-time check.
- **A handle is a key.** `Handle with T` is a scalar word: copied, never moved, not owning. Its generation is part of the
  word, so a stale handle never reaches a reused slot. A handle is typed by its element type and not by its pool, so
  a handle from one pool used on another pool of the same type is a lookup that fails or finds that pool's element;
  that is not a soundness issue and is not prevented. Equality of handles is allowed.
- **A list hands out handles like a pool.** A list is a doubly linked list whose nodes are named by `Handle with T`:
  `prepend` and `append` return the handle, `first`, `last`, `next` and `prev` walk, `remove` and `moveToFront`
  take one. That is the smallest API with which a cache is a list and a map from keys to handles.
- **Keys of a map** are words or text: an integer alias, a `bool`, an enum, a `String` or a `Handle`. A record has no
  equality, and a type parameter is not known to have one, so generic code over `Map with (K, V)` waits for a bound on keys
  (`E3908`).
- **No iteration beyond the index.** `for i in 0 upto a.count()` works. A `for x in a` loop would borrow the array for
  its whole body, which the exclusivity rule already covers, but it is not built, and a map has no key walk.
- **Native and interpreter.** Every operation lowers to a named runtime entry (README, "The primitives in the IR"),
  so the interpreter of step 4 implements the same entries as the backend does, and element ownership is visible in
  each call. Only the array is native, in a unit that is linked only when used. A map, a list and a pool lower too,
  and the driver refuses a program that needs one when it builds an executable, with a code of its own. Nothing is
  freed yet, so an element that is removed or dropped is leaked, as every other box is.
- **Not built.** Copying a record that contains itself through an array (`clone()` writes a copy out in place and
  would not end), a `for x in a` loop, iteration over a map, a bound on map keys, and freeing elements in native code.

### Choices made in step 4

Where this document was open, minimaxon does the following.

-  **Owner death is explicit in the IR.** Lowering emits a drop wherever a value dies: a local at the end of its block
  and on every way out of it, a temporary at the end of its statement, an overwritten value at the assignment, a
  `consumes` parameter that was not moved on, a removed element that the caller discards. A drop is deep by type, and a
  place that holds nothing drops as a no-op, so a value that was moved from is not dropped and no analysis of which
  paths moved it is needed: moving empties the place (`move`), and dropping an empty place does nothing. The
  alternative, a drop flag for each local that is maybe moved, is the same thing with the flag stored beside the value;
  an empty slot is the flag. The cost is a native frame that starts zeroed, which native freeing provides (step 6).
- **One place decides what moves.** The ownership pass already decides which expressions give their value to a new
  owner, since it must for `E3102`. It now marks them, and lowering reads the mark and nothing else, so the rule that
  refuses a use after a move and the code that empties the slot cannot disagree. A place that is marked but is not a
  whole local is an internal error in lowering, which is how a hole in "No partial moves" shows up (see below).
- **Strings are owned.** A literal lives in the executable and is immortal. An interpolation makes a new string, and
  `clone()` of a string makes a new one too, where step 1 said its copy is itself: with freeing, two owners of one string
  would free it twice.
-  **What the interpreter tracks.** The owner of each value is the place that holds it, and a place that only borrows
  holds the same pointer without owning it (a `let` or `mutates` parameter, a projection, and a `match` binding, which
  the IR marks). A value in flight, such as the result of a call, has no owner, and storing a value that has one is a
  violation: it would be freed twice. An element's address carries the version of its collection, and every insert,
  remove and move of the collection bumps it, which is the contract of the primitives ("valid until the next call that
  changes the primitive") made checkable. A handle carries a generation, and the slot freed last is the next one reused,
  so a reused slot is the common case in a test and not the rare one.
- **Exclusivity at run time.** At a call, every argument that the callee may change (an address, an argument it owns,
  a `mutates self`) must not reach any place another argument reaches, which the owner tree answers: two places overlap
  when one is, or lies inside, the other. A borrow is taken when its argument is evaluated, so from there until the call
  runs it is held, and a change to what it reaches (a store, a drop, an insert or a remove) is a violation. That is the
  rule the checker calls a held borrow, and it is checked on the borrows that name existing storage; a value that is
  still being built is not one. A read while a borrow is held is fine and is not reported.
- **Primitives in Go.** `insert` into a map that has the key keeps the entry, drops its old value and drops the key that
  came in, since the map owns one; `remove` drops the key it held. `copy` of a map, a list or a pool copies every key
  and element and keeps the handles of a list and a pool valid. A handle that names nothing traps with a message, as an
  index out of range does.
- **The test runner.** Every program in `tests/run` runs natively and under the interpreter, which must give the same
  stdout and exit code and no violation; a program that needs a map, a list or a pool has no native code and runs under
  the interpreter alone (the runner sees `E9900` and falls back, so no directive is needed).
- **Checker holes found.** Three, each with a test in `tests/errors/` that failed first.
  1. A field or an element of a temporary could be moved out of it (`let s = make().label`): the pass treated an
     expression that is not a place as fresh and moved nothing, so the temporary and the binding both owned the string
     and both would free it. Found by reading the pass while placing drops. It is `E3901` now: a part of a temporary is never moved out.
  2.  `self` of a `mutates self` method could be passed to a `mutates` parameter, which may assign the whole value and
     so replace the caller's: but `self` is the receiver, a pointer the caller still owns, not a place that can be
     replaced. Found by the generator, as an internal error in lowering ("no address for this expression"). It is
     `E2013` now; a field of `self` and a method call on it are fine.
  3.  A variable passed to a `sets` parameter counted as initialised as soon as its argument was checked, so a later
     argument of the same call could read it (`fill(s, from: s.clone())`), though the callee has not run yet. Found by
     the generator, as a read of an empty slot, a hundred thousand programs in. The variable has its value when the call
     has run, and not before: `E3902` for the read.
- **Bugs in lowering that the interpreter found**, fixed at the cause: the target of an assignment had no type, so an
  assignment to a field of a generic instance did not know to drop the old value; an assignment to an element did not
  drop the old element; a field's default was dropped as a temporary because nothing had marked it consumed.
- **What the generator reaches.** A record, a union, an array, a map, a list and a pool of any of them, with handles
  stored in fields, arrays and maps; methods whose bodies are generated like any function's, with `self` read, changed
  and consumed; the four conventions, including `sets` through a variable with no value; `match` over a union and an
  option, as a statement and as a value; conditionals that move; loops with `break` and `continue`; generic functions,
  with and without bounds, and a generic record. About two programs in five break a rule of ownership on purpose
  (moving a field, a borrow or an element; reading a variable that moved; overlapping arguments; changing a collection
  while one of its elements is borrowed; a match arm that changes its subject; a place borrowed by one operand of a
  string comparison or the first part of an interpolation, or by the base of a subscript, while a later operand, or the index, is a call that changes or
  consumes what it lies in), to see the checker refuse each, and the same expressions with a call that only reads or
  works on another variable, to see it accept them. Of the
  programs it accepts, the ones with no map, list or pool also run natively and must give the interpreter's output and
  exit code. Removing a rule from the checker (exclusivity at a call, held borrows, a use after a move, a move out of a
  borrow, a `sets` or a variable that is not initialised, the hold on the target of an assignment) makes the generator
  find a violation within a few thousand programs, which is the evidence that it reaches what it should.
- **Not covered.** The generator writes no union or enum methods, no generic interface, no `for` over anything but an
  index, and no recursion; it has not been run on more than one module. A program can only break the rules in the ways
  the generator writes it to, so a clean run says the checker holds against those and not that it holds. The
  interpreter finds a violation only on a path that runs: a program that breaks a rule on a path it does not take, or
  after a trap, is not caught by that run.

## Layer 3: between threads

A green thread runs in its own memory: the values it owns. Data reaches another thread only by:

- **Moving** an owned value: isolated by construction, so nothing else points in. A handle moved without its
  pool finds nothing on the other side, since it is only a key.
- **Sharing** an immutable value with `share` (counted).
- **Cowns and behaviours.** A **cown** (concurrent owner) owns a value, which no thread touches directly; the cown
  reference can be copied and shared freely. A **behaviour**, `when a, b as x, y … end`, names the cowns it needs
  and runs once it has exclusive access to all of them; inside, `x` and `y` are `mutates` access to their values. It
  captures only moved values, shared immutable values and cown references, returns nothing, and is not waited
  for; a result is passed on by scheduling another behaviour. A behaviour's cowns are acquired together and
  nothing blocks while holding one, so there is no deadlock. Behaviours on overlapping cowns run in the order they
  were scheduled. Implemented in step 5; see "Choices made in step 5".

### Choices made in step 5

Where this document was open, minimaxon does the following. The rules and the error codes are in the README ("Immutable
values, cowns and behaviours"); these are the decisions behind them.

-  **Immutability is a type.** `frozen with T` is a built-in generic type, and `freeze` a stdlib function that takes a
  `consumes T` and gives one. A type lets the checker tell an immutable value from its owner by what a name is declared
  as, with no facts about a value's history to carry through every construct, which is what grew maxon's rulings. The
  contents of a frozen value are one more step in a place's path, so every rule that asks whether a place can change or
  be moved (assignment, `mutates`, `consumes`, a method receiver, exclusivity, a borrow held across a call) already
  answers for it: a path that crosses a frozen step is immutable, and nothing moves out of it. Reading through a frozen
  value is implicit where it only borrows: a field, a subscript, a method that takes `let self`, a match subject, an
  interpolation, and an argument for a `let` parameter of the type it holds.
- **`clone()` is `share`, and `thaw` is the copy of the contents.** `clone()` keeps the type, as it does everywhere, and a
  frozen value is immutable, so its copy is another owner of the same value at the cost of a count. That is also what
  keeps generic code uniform: `x.clone()` of a `T` means one thing whatever `T` is instantiated with. `thaw(f)` is the
  deep copy of the contents, owned and mutable, which the spec called "`clone()` out"; it is the one construct added to
  the list in the step.
- **One count per frozen value, and no cycle.** A frozen value is made by giving up an owned tree, which is isolated
  (layer 1), so the objects inside it are the tree's, and the only counted values inside are ones that already existed.
  Nothing inside can change afterwards, so it can never be made to hold a value that is made later. The references
  between frozen values, and from a frozen value to a cown, point to older ones, which makes counts a DAG. `freeze` of a
  plain value is refused (`frozen with Count` has nothing to freeze); a generic instance may still hold a scalar, so the
  box and the entries take one.
- **A cown is a counted reference, and a type may not reach itself through one.** A cown reference is another kind of
  owner that `share` and `clone()` make, freed with the last one and with no behaviour holding the cown. The value of a
  cown is mutable and a behaviour can store a cown reference in it, so unlike a frozen value it can make a cycle that
  counting would never free. The step does not build a cycle collector, so a type that can reach itself through a cown
  is refused (`E3914`), which makes every cycle of references impossible: a cycle of values has to be a cycle of types,
  through at least one declaration, and through a cown if one of the references is. The cost is that a linked structure
  of cowns names its next link by a handle or a key. Verona leaves the cycle to a collector; if one is built, the check
  goes.
-  **A behaviour is a function that captures by use.** Its body is checked as a function of its own, whose parameters
  are the access to each cown's value (`mutates`) and then whatever it uses from the function around it, which it owns
  like a `consumes` parameter. Capture is by variable and whole: a scalar is copied, and an owned value moves in at the
  `when`, marked consumed so that the existing move check, `E3102`, covers a use after it and a use in the next
  iteration of a loop. Frozen values and cown references are owned values here, so they move too, and keeping one is
  `share` first, which the spec allows ("moved in, or shared explicitly") and extends to cown references, which it says
  are "copied/moved in". An implicit copy would be a second way to count and would hide where a count rises; the cost is
  a `let own = share(c)` before a `when` that uses a cown the enclosing code keeps. A borrow is refused (`E3909`), by
  the check that a behaviour makes at the capture and, behind it, by the rule that nothing moves out of a borrow. `self`
  and its fields are refused too, because the receiver is borrowed.
-  **The access names are `mutates` parameters.** `x` and `y` are borrows, so every rule about borrows keeps them inside
  the body: they cannot be moved, returned, stored, passed as `consumes`, or captured by a behaviour inside (`clone()` is
  the way out). They name different cowns, so exclusivity between them needs no rule of its own, except that one cown
  named twice would make two names for one value.
-  **The same cown twice.** Naming one variable twice is `E3913`. Two variables that name one cown (`let b = share(a)`)
  cannot be told apart by the checker, so a behaviour scheduled with the same cown twice traps, as an index out of range
  does: a safe failure, never a race, but the one place where the safety of a program does not rest on the checker
  alone. Verona de-duplicates the request at run time; giving the body two `mutates` names for one value would break the
  law of exclusivity, so this step does not.
- **The cowns of a `when` are names, not expressions.** In `when a, b as x, y` the word `as` after an expression is a
  cast, so the grammar takes a name for each cown (a variable or a field of `self`), and a cown that lies deeper is
  shared into a variable first. A name is only read: the behaviour takes a count of its own on each cown when it is
  scheduled and gives it back when it has run, so the cown outlives the references of the code that scheduled it.
- **Lowering.** A frozen value and a cown are boxes that more than one place owns (`rt.frozen.freeze`, `rt.frozen.get`,
  `rt.share`, `rt.cown.new`), and a `when` is one instruction whose body is a function of its own, with the cowns first and
  then the captures. Nothing of it is native: the driver refuses it with `E9900`, `freeze` alone included, because
  green threads, a scheduler and atomic counts are not built and a frozen value with no thread to cross to tests none of
  the rules.
- **A scheduler that simulates the interleaving.** Verona's queues, as written: a behaviour is queued on each cown it
  names when it is scheduled (one atomic step), and runs when it is first in all of its queues, so behaviours on a common
  cown run in the order they were scheduled and nothing waits while holding a cown. Threads are stacks of frames handed the turn
  one at a time, and the scheduler picks the next thread with a seeded generator from every thread that has started and every
  behaviour that can start, and a thread keeps the turn for a random number of instructions (up to 2, 8 or 64), so a seed
  names one interleaving at instruction granularity and the same seed always gives it again. The test runner runs a
  program that schedules behaviours under 20 seeds, and the generator under 3 or more.
- **What the interpreter detects.** Who owns each value, found by following owners up to a frame's slot, the value of a
  cown or the contents of a frozen value. A thread that touches a value of another thread, or the value of a cown while its
  own behaviour does not hold the cown, is a data race; a change to anything in a frozen value is a violation; a count
  that is wrong (a box freed while a place or a waiting behaviour holds it, or lowered below 0) is a violation, and a
  frozen value or a cown that is still live when the program has ended, which a leaked reference or a cycle leaves, is a
  leak. A value handed to a behaviour while its scheduler still owns it is a data race at the `when`. Not detected: a
  race is found only on an interleaving that the seeds happen to try, and a value that is only read by two threads is not
  a race, which is right; a read that races with a write is found when the schedule puts it between the two, which a
  few thousand programs under several seeds make likely and no number makes certain.
- **Checker holes found.** None: no program that the checker accepted broke a rule under the interpreter, in 85,000
  generated programs (below). Two bugs, neither a hole, each with a test that failed first:
  1. A method of a bound called on a frozen value (`f.area()` with `f` of `frozen with T`) reached instance resolution with
     the frozen type as the receiver, which has no such method, and failed as an internal error. Found by hand while
     writing the corpus; `Resolved` looks through a frozen receiver now, as the checker does
     (`tests/run/frozen-bound-call.maxon`, and `totalFrozen` in the generator's library).
  2. A `clone()` that no source wrote (an array filled with a value that has no native code, here a cown or a frozen
     value, and since step 4 a pool or a map) was lowered with no site to blame, so refusing it for the executable
     crashed the driver. Found by the generator's comparison with executables; lowering names the call now
     (`TestEveryRefusalNamesItsSite`).
-  **What the generator reaches.** Frozen values of records, unions, strings and arrays, held in variables (`let` and
  `var`), fields and elements; cowns of counts, records, unions, strings, arrays, frozen values and other cowns; `share`
  and `thaw`; behaviours on one or two cowns whose bodies are written like any function's, nested two deep, with
  scalars, owners, frozen values and cown references captured; reads through a frozen value in fields, subscripts,
  method calls, a bound's method, `match` and arguments for `let` parameters; and calls of generic functions with frozen
  and cown arguments. About two programs in five are careless, breaking rules throughout, and two in five of the rest
  break one rule of sharing once, which is what lets a missing rule show: a careless program is nearly always refused
  for the first of many sins. Removing each of these checks from the checker makes the generator find a violation or an
  internal error within 3,000 to 4,000 programs: a captured owner that is not moved, a captured borrow (with the
  move-out-of-a-borrow rule behind it removed too, since it refuses the same program), a change to the contents of a
  frozen value, a `mutates` or `consumes` method on one, a part taken out of one (with the field rule behind it removed
  too), `x` taken as a `consumes` parameter, and `self` named in a behaviour. Removing only the refusal of a cown's
  fields and methods finds nothing, because typing refuses them first, and the cycle rule is not reached, since the
  generator declares no type that reaches itself; both have tests of their own.
- **Results.** Three long runs with different seeds and generators of the day, every accepted program that schedules a
  behaviour under 5 or 6 schedules:

  | Programs | Accepted | Scheduling behaviours | Behaviours | Schedules each | Violations |
  |---|---|---|---|---|---|
  | 25,000 | 6,815 | 4,050 | 42,465 | 5 | 0 |
  | 30,000 | 8,293 | 4,917 | 54,919 | 6 | 0 |
  | 30,000 | 8,163 | 4,829 | 51,870 | 6 | 0 |

  Between 800 and 1,000 of the accepted programs of a run end in a trap, as they do from step 4 on, and one in the last run
  loops without end (a loop that doubles its own bound); neither is a violation. With `MINIMAXON_GEN_NATIVE=1`, 246 of
  2,500 programs (the ones with no map, list, pool, frozen value, cown or behaviour) also ran as executables with the
  interpreter's output and exit code.
- **Not covered.** A cycle of cowns, which the type rule refuses and the generator never builds, so the interpreter's leak
  report for one is tested only on IR. The generator writes no `when` in a generic function, no behaviour more than two
  deep, and only short bodies, so the interleavings are of a few instructions each. A race is found only on the
  interleavings that the seeds try. A program that breaks a rule on a path it does not take, or after a trap, is not
  caught by that run, as in step 4.

## Generics

- **Layer 1:** a generic body is checked once against its bounds (as Hylo, Swift and Rust do), never per
  instance. Ownership and conventions are the same for every type, so `T` needs no capability. Implemented; see
  "Choices made in step 2".

### Choices made in step 2

Where this document was open, minimaxon does the following.

- **What `T` is.** A value of a type parameter is owned, movable and not a scalar, whatever the parameter is
  instantiated with. Every rule of step 1 then applies to it as it applies to a record: binding, passing to `consumes`,
  returning and storing move it; a `let`, `mutates` or `sets` parameter of type `T` and a payload of one are borrowed,
  and a field of a generic value is part of its owner, so moving one out of a borrowed owner is `E3900`; exclusivity counts a
  `T` argument like any other; `clone()` is allowed. This is the strictest reading, and it is sound for every instance
  because an instance only makes a move cheaper (a scalar is copied) and never makes an access broader. The cost
  is that a generic body cannot unwrap: giving a field or a payload out is `x.clone()`, or, since step 3, taking it
  out of a primitive with `remove`, `pop` or `unwrap`.
- **What a bound is.** An interface: method signatures with their receiver conventions, naming `Self`. A type
  implements one only by declaring it (`type Square implements Shape`), and the declaration is checked once, at the type,
  for every method with the signature the interface gives. So an instance is judged by one fact, a declaration the
  type argument carries, and a bound can never fail at an instance after it passed at the declaration. Inside a
  body, a parameter meets a bound only if it is declared with it, and its methods are the bounds' methods
  (two bounds declaring one name is an error, not an overload).
- **What an instance is judged for.** Only that each type argument implements the interfaces its parameter is
  bounded by. Nothing in a generic body is looked at again per instance, and an error in a body is reported once,
  at the body, whether or not it is instantiated.
- **Interfaces are bounds, not types.** No value has an interface type, so there is no dynamic dispatch and no
  existential. A bound's method is called through the interface, so the implementing method needs no `export`. Not
  built: a generic interface, an interface that extends another, a static requirement (`Self.zero()`), a default
  method, and an implementation by a primitive or an alias, which cannot declare methods.
- **Generic declarations.** A function, a record and a union. A method takes its type's parameters and none of its
  own. `Self` in a generic type is the type at its own parameters, and a literal of it is `Self{...}`.
- **Inference.** From the argument types, by matching a parameter's type against the argument's, through the
  instances of generic types, and for what the arguments leave open, from the type the result must have where it
  goes (a return value, an argument, an assignment, a field). An integer constant or the range of an expression names
  no alias, so it infers nothing. Source writes no type arguments in an expression: where nothing fixes one, a static
  function or a case is called through a typealias of the instance (`typealias Counts = Array with Count`).
- **Lowering.** Monomorphization, because the IR keeps `scalar` and `managed` apart and copy is type-directed: each
  instance's body is the checked body lowered with the parameters bound. A phase between verify and lower finds the
  instances serially, from the calls each function records, so each is lowered once and in an order that does not
  depend on the number of jobs. An instance is emitted in the unit of the module that declares the generic.
- **Instances are shared.** A generic declaration interns its instances by type arguments behind a lock, so
  `Box with Count` is the same type in every module; an instance reads its members through the declaration. It is the
  one write the parallel phases share, and nothing reads an instance's members before the declare phase ends.
- **Limits.** Nesting of instances is capped at depth 16: a generic type that contains itself with a larger
  argument, or a generic function that calls itself with one, is an error instead of an endless expansion.

## Native freeing

### Choices made in step 6

- **The drops were already enough.** Step 4 put every owner's death in the IR and made an empty place a no-op, so the backend
  adds nothing to the IR but a copy instruction (below). A `move` and a `moveat` store 0 in the place they read, and a
  frame zeroes its managed slots when it starts (a loop's back edge needs nothing, because the `dropslot` at the end of
  the body leaves its slot empty). A register is not zeroed: lowering writes one before it drops it on every path, and the
  interpreter now reports a drop of a register that holds nothing, so a hole in that is a violation and not a crash.
- **A copy is an instruction, not code written out.** Step 3 lowered `clone()` into the IR as a loop, which cannot end for a
  type that contains itself, so it refused one (`E9000`). It is now `%r = copy %s T`: the interpreter copies by the shape of
  the value (it already could, for a map, a list and a pool), and the backend makes one copy routine for each type, as it
  does a drop routine. `E9000` for a recursive copy is gone, and a tree copies natively.
- **A routine for each type, after monomorphization.** Which words of a box are boxes depends on the type, so a drop and a
  copy are routines that the code of every module calls, generated once for the program for each concrete type that the
  program drops or copies and for each type those reach. They call each other, so a type that contains itself needs no
  special case. A box with nothing managed in it is dropped by the free itself.
- **A literal is found by its address.** The alternatives were a flag in the string's header and a distinct pointer tag;
  both cost on every read of a string and tie the design to a representation. A check of the address against the
  executable's data section costs a few instructions, only when a string is dropped, and the linker, which knows the
  section, defines its bounds. It does not depend on anything but a literal being in an image section.
- **Freeing is checked by counting.** An executable built with `--count-allocations` counts the boxes it allocates and
  frees and fails if any is live when `main` returns (exit 99). The corpus and the generator build every executable that
  way, so a drop that the interpreter's proof said was placed right, and that the backend did not turn into a free, or
  turned into a double free, shows up as a leak or a crash. A program that traps is not checked: it exits with its
  values held.
- **Memory is proved to be reused** by a run test with a loop of half a million iterations that makes about twenty boxes
  each, in a process whose memory is capped (`// memory-limit:`); the executable that never freed fails it with an
  allocation that fails.
- **Cost.** The routines add a phase after codegen, in parallel in units of 256 routines; on a generated project of 200
  modules about 4,300 routines make about 440 KB of code, and the executable is about 2.5% larger. See "Native freeing" in
  the README.

## Checking the design

- **One checker package** owns ownership, conventions, exclusivity and the generic bound checks. Nothing else
  decides one of those questions.
- **A tracking interpreter, for development only** (`self/interp`). It runs the IR and records every value's
  owner and every borrow a call is holding. It checks that nothing is used after it is freed, that everything is freed
  exactly once, that nothing is used after it moved, and that exclusivity holds. Since step 5 it also schedules
  behaviours, tracks which thread, cown or frozen value owns each value, and counts the owners of a frozen value and a
  cown. It is never part of a compiled program: if it fires on a program the compiler accepted, the checker has a hole.
  Done in steps 4 and 5.
- **Generated programs** over the constructs the rules cover: every accepted program must run clean under the
  interpreter, under several schedules when it schedules behaviours. Done in steps 4 and 5
  (`self/fuzz`, which the `fuzz` command runs).
- **The primitives get their own tests**, including stale handles after removal and slot reuse, since they are the
  trusted code.
- **Hylo's tour examples** become corpus tests for layer 1.

## Steps

minimaxon settles whether the rules are sound, which is a question for the checker, not the runtime. It
implements the rules completely and the runtime only as far as testing them needs.

1. Layer 1 checks: ownership and moves, `clone()`, the four conventions (`mutates` being Hylo's `inout`),
   second-class borrows, and the law of exclusivity over paths. **Done**, in `self/check` (see "Choices made
   in step 1").
2. Generics, checked once against their bounds. Second, because how they meet ownership is the unknown that
   hurt maxon. **Done**, in `self/check` (bounds, inference, the uniform rules over `T`) and `self/ir`
   (instances; see "Choices made in step 2").
3. The primitives' signatures (`array`, `map`, `list`, `pool`): ownership of elements, projections, handles. **Done**,
   in `self/ast` (the types), `self/check` (subscripts, element paths, the held borrows) and `self/ir`
   (lowering to runtime entries); an array also runs natively (see "Choices made in step 3").
4. The tracking interpreter and the program generator. The primitives run here, in the interpreter. **Done**, in `self/ir`
   (drops at owner death), `self/interp` (the interpreter, and the generator in its tests) and `self/driver` (the test
   runner runs every program under it); see "Choices made in step 4".
5. The rules for behaviours: what a `when` may capture, cowns, `share`. The interpreter runs behaviours with a
   simulated scheduler, in random orders, to test them. **Done**, in `self/check` (frozen contents as a path step,
   captures, the cycle rule), `self/ir` (the boxes, `when`, the behaviour bodies) and `self/interp` (the
   scheduler, owners by thread, counts); see "Choices made in step 5".
6. Native freeing: a move empties its source, a frame starts with its managed slots empty, and each drop and copy is a
   routine for its type, so a compiled program frees every value where its owner dies and a count of live boxes proves it
   (see "Choices made in step 6"). **Done**, in `self/backend` and `self/ir`.

Not built here, because they teach nothing about soundness: green threads, a scheduler, atomic counts and cowns
in generated code; native implementations of `map`, `list` and `pool` (the array is native, since corpus programs need it).
These belong to the full compiler.
