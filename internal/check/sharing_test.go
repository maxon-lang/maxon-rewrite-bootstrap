package check

import (
	"testing"

	"minimaxon/internal/diag"
)

const sharing = `type Doc
	export var title as String
	export var n as Count
	export var words as array[Count]
	static function make(title sink String) returns Doc
		return Doc{title: title, n: 1, words: array[Count].new()}
	end 'make'
	function read() returns Count
		return n
	end 'read'
	function grow(inout self)
		n = n + 1
	end 'grow'
	function finish(sink self) returns Count
		return n
	end 'finish'
end 'Doc'

type Shelf
	export var doc as frozen[Doc]
	export var tray as cown[Doc]
	static function make(doc sink frozen[Doc], tray sink cown[Doc]) returns Shelf
		return Shelf{doc: doc, tray: tray}
	end 'make'
end 'Shelf'

function glance(d Doc) returns Count
	return d.n
end 'glance'

function bump(x inout Count)
	x = x + 1
end 'bump'

function keep(d sink Doc)
	let kept = d
end 'keep'

`

func checkSharing(t *testing.T, body string) []*diag.CompileError {
	t.Helper()
	return checkSource(t, sharing+"function main() returns ExitCode\n"+body+"\treturn 0\nend 'main'\n")
}

func TestFrozenValuesAreReadAndShared(t *testing.T) {
	expectCodes(t, checkSharing(t, `	let d = freeze(Doc.make("a"))
	let e = share(d)
	print("{d.title} {d.n} {d.words.count()} {e.read()} {glance(d)}")
	let own = thaw(d)
	var again = thaw(e)
	again.grow()
	let copied = d.copy()
	let shelf = Shelf.make(copied, tray: cown(own))
	let f = freeze(shelf)
	print("{f.doc.title} {f.doc.n}")
`))
}

func TestFrozenValuesCannotBeChanged(t *testing.T) {
	prelude := "\tlet d = freeze(Doc.make(\"a\"))\n"
	cases := map[string]struct {
		body string
		code string
	}{
		"field":    {"\td.n = 3\n", diag.ErrFrozenChange},
		"element":  {"\td.words[0] = 3\n", diag.ErrFrozenChange},
		"inout":    {"\tbump(d.n)\n", diag.ErrFrozenChange},
		"method":   {"\td.grow()\n", diag.ErrFrozenChange},
		"sink":     {"\tlet n = d.finish()\n", diag.ErrFrozenChange},
		"argument": {"\tkeep(d)\n", diag.ErrFrozenChange},
		"moved":    {"\tlet t = d.title\n", diag.ErrMoveOutOfField},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			expectCodes(t, checkSharing(t, prelude+c.body), c.code)
		})
	}
}

func TestSharingNeedsASharedValue(t *testing.T) {
	expectCodes(t, checkSharing(t, "\tlet d = Doc.make(\"a\")\n\tlet e = share(d)\n"), diag.ErrShareTarget)
	expectCodes(t, checkSharing(t, "\tlet d = Doc.make(\"a\")\n\tlet e = thaw(d)\n"), diag.ErrShareTarget)
	expectCodes(t, checkSharing(t, "\tlet n = freeze(3 as Count)\n"), diag.ErrFreezeScalar)
}

func TestACownIsSharedAndNamedByABehaviour(t *testing.T) {
	expectCodes(t, checkSharing(t, `	let c = cown(Doc.make("a"))
	let other = share(c)
	let note = Doc.make("b")
	let label = 3
	when c as x 'run'
		x.n = note.n + label
		x.grow()
	end 'run'
	when other as y 'again'
		bump(y.n)
	end 'again'
`))
}

func TestABehaviourMayScheduleAnotherButNotWithItsAccess(t *testing.T) {
	expectCodes(t, checkSharing(t, `	let c = cown(Doc.make("a"))
	let d = cown(Doc.make("b"))
	when c as x 'outer'
		when d as y 'inner'
			y.n = x.n
		end 'inner'
	end 'outer'
`), diag.ErrBehaviourBorrow)
	// What the outer body reads into a scalar of its own, the inner one captures.
	expectCodes(t, checkSharing(t, `	let c = cown(Doc.make("a"))
	let d = cown(Doc.make("b"))
	when c as x 'outer'
		let n = x.n
		when d as y 'inner'
			y.n = n
		end 'inner'
	end 'outer'
`))
}

func TestABehaviourCapturesOwnersAndCopiesScalars(t *testing.T) {
	expectCodes(t, checkSharing(t, `	let c = cown(Doc.make("a"))
	let d = cown(Doc.make("b"))
	let mine = share(d)
	when c as x 'outer'
		when mine as y 'inner'
			y.n = 1
		end 'inner'
	end 'outer'
`))
}

func TestACapturedOwnerIsMovedIntoTheBehaviour(t *testing.T) {
	expectCodes(t, checkSharing(t, `	let c = cown(Doc.make("a"))
	let note = Doc.make("b")
	when c as x 'run'
		x.n = note.n
	end 'run'
	print("{note.n}")
`), diag.ErrUseAfterMove)
}

func TestABehaviourCannotCaptureABorrow(t *testing.T) {
	source := sharing + `function watch(note Doc, c cown[Doc])
	when c as x 'run'
		x.n = note.n
	end 'run'
end 'watch'

function main() returns ExitCode
	return 0
end 'main'
`
	expectCodes(t, checkSource(t, source), diag.ErrBehaviourBorrow)
}

func TestTheValueOfACownIsOnlyForABehaviour(t *testing.T) {
	expectCodes(t, checkSharing(t, "\tlet c = cown(Doc.make(\"a\"))\n\tprint(\"{c.n}\")\n"), diag.ErrCownAccess)
	expectCodes(t, checkSharing(t, "\tlet c = cown(Doc.make(\"a\"))\n\tlet n = c.read()\n"), diag.ErrCownAccess)
}

func TestACownNamedTwiceIsRefused(t *testing.T) {
	expectCodes(t, checkSharing(t, "\tlet c = cown(Doc.make(\"a\"))\n\twhen c, c as x, y 'run'\n\t\tx.n = y.n\n\tend 'run'\n"), diag.ErrWhenSameCown)
}

func TestATypeThatReachesItselfThroughACownIsRefused(t *testing.T) {
	source := `type Node
	export var next as Option[cown[Node]]
	static function make() returns Node
		return Node{next: Option[cown[Node]].none}
	end 'make'
end 'Node'

function main() returns ExitCode
	return 0
end 'main'
`
	expectCodes(t, checkSource(t, source), diag.ErrCownCycle)
	// An owned value and a frozen value cannot form a cycle, so a type may hold either of itself.
	frozen := `type Link
	export var rest as Option[frozen[Link]]
	static function make() returns Link
		return Link{rest: Option[frozen[Link]].none}
	end 'make'
end 'Link'

function main() returns ExitCode
	return 0
end 'main'
`
	expectCodes(t, checkSource(t, frozen))
}

func TestGenericCodeSharesAndFreezesAnyType(t *testing.T) {
	source := `function wrap[T](x sink T) returns frozen[T]
	return freeze(x)
end 'wrap'

function again[T](x frozen[T]) returns frozen[T]
	return share(x)
end 'again'

function open[T](x frozen[T]) returns T
	return thaw(x)
end 'open'

function hold[T](x sink T) returns cown[T]
	return cown(x)
end 'hold'

function main() returns ExitCode
	let f = wrap("a")
	let g = again(f)
	let s = open(g)
	let c = hold(s)
	return 0
end 'main'
`
	expectCodes(t, checkSource(t, source))
}
