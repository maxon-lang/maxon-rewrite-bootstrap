package main

import (
	"fmt"
	"regexp"
	"strings"
)

// The holdings program: one value of every way the layout table (ir.holding) says a holder keeps a value, in every holder the
// debug info describes, which the debugger reads back and the dump of every target describes. A variable holds a word (a
// Count), two words (a String), the address of a box (Named, Found) or of the words of a record held in place (Point,
// Shape), and a parameter taken by mutates or sets is found at the address it holds; a field holds a word, two words, a
// box or the words of a record held in place, to two levels (Shape's edge's from); an element of an array's buffer is a
// word, two words, the address of a box, the words of a record held in place, or a byte (Color); a payload (Found's hit)
// is a box; a closure's capture is not described by the debug info, which names no capture.
const holdingsSource = `type Point
	export var x as Count
	export var y as Count

	static function make(x Count, y Count) returns Point
		return Point{x: x, y: y}
	end 'make'
end 'Point'

type Segment
	export var from as Point
	export var to as Point
	export var id as Count

	static function make(consumes from Point, consumes to Point, id Count) returns Segment
		return Segment{from: from, to: to, id: id}
	end 'make'
end 'Segment'

type Shape
	export var edge as Segment
	export var tag as Count

	static function make(consumes edge Segment, tag Count) returns Shape
		return Shape{edge: edge, tag: tag}
	end 'make'
end 'Shape'

type Named
	export var count as Count
	export var name as String

	static function make(count Count, consumes name String) returns Named
		return Named{count: count, name: name}
	end 'make'
end 'Named'

union Found
	hit(point Point)
	miss
end 'Found'

enum Color
	red
	green
	blue
end 'Color'

function note(n Count) returns Count
	if n > 0 'more'
		return note(n - 1)
	end 'more'
	return 0
end 'note'

function viewed(p Point) returns Count
	let sum = try p.x + p.y otherwise 0
	_ = note(0)
	print("{sum}\n")
	return sum
end 'viewed'

function changed(mutates p Point) returns Count
	p.x = 70
	_ = note(0)
	print("{p.x}\n")
	return p.y
end 'changed'

function grown(mutates n Count)
	n = 9
	_ = note(0)
	print("{n}\n")
end 'grown'

function labelled(mutates text String)
	text = "changed"
	_ = note(0)
	print("{text}\n")
end 'labelled'

function taken(consumes p Point) returns Count
	let sum = try p.x + p.y otherwise 0
	_ = note(0)
	print("{sum}\n")
	return sum
end 'taken'

function made(n Count) returns Point
	let r = Point.make(n, y: 22)
	_ = note(0)
	print("{r.x}\n")
	return r
end 'made'

function main() returns ExitCode
	let local = Point.make(3, y: 4)
	var moved = Point.make(5, y: 6)
	let shape = Shape.make(Segment.make(Point.make(10, y: 11), to: Point.make(12, y: 13), id: 14), tag: 15)
	let boxed = Named.make(16, name: "boxed")
	let found = Found.hit(Point.make(17, y: 18))
	var count = 1 as Count
	var text = "one"
	var points as Array with Point
	points = Array.create(0)
	points.push(Point.make(30, y: 31))
	points.push(Point.make(32, y: 33))
	var counts as Array with Count
	counts = Array.create(0)
	counts.push(40)
	counts.push(41)
	var names as Array with String
	names = Array.create(0)
	names.push("first")
	names.push("second")
	var colors as Array with Color
	colors = Array.create(0)
	colors.push(Color.red)
	colors.push(Color.green)
	colors.push(Color.blue)
	var boxes as Array with Named
	boxes = Array.create(0)
	boxes.push(Named.make(50, name: "fifty"))
	boxes.push(Named.make(51, name: "fifty-one"))
	let hit = match found 'found'
		hit(p) gives p.x
		miss gives 0
	end 'found'
	let a = viewed(local)
	let b = changed(moved)
	grown(count)
	labelled(text)
	let c = taken(Point.make(40, y: 41))
	let d = made(21)
	print("{hit} {a} {b} {c} {d.x} {local.x} {moved.x} {shape.tag} {shape.edge.to.y} {boxed.count} {count} {text} {points.count()} {counts.count()} {names.count()} {colors.count()} {boxes.count()}\n")
	return 0
end 'main'
`

// holdingsExpected is what the dump of the holdings program must show on every target: where a record is held in place the
// field, or the element of the array, is marked so, with the offsets and strides the layout table gives, and the by
// reference parameters are marked.
var holdingsExpected = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^    \[\d+\] Point  Struct  size=16 align=8  fields=2\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Segment  Struct  size=40 align=8  fields=3\r?\n        \+0x0000  from : Point \(in place\)\r?\n        \+0x0010  to : Point \(in place\)\r?\n        \+0x0020  id : Count\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Shape  Struct  size=48 align=8  fields=2\r?\n        \+0x0000  edge : Segment \(in place\)\r?\n        \+0x0028  tag : Count\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Named  Struct  size=24 align=8  fields=2\r?\n        \+0x0000  count : Count\r?\n        \+0x0008  name : String\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Array with Point  Array  size=16 align=8  fields=0\r?\n        element : Point \(in place\)\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Array with Count  Array  size=8 align=8  fields=0\r?\n        element : Count\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Array with String  Array  size=16 align=8  fields=0\r?\n        element : String\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Array with Color  Array  size=1 align=8  fields=0\r?\n        element : Color\r?$`),
	regexp.MustCompile(`(?m)^    \[\d+\] Array with Named  Array  size=8 align=8  fields=0\r?\n        element : Named\r?$`),
	regexp.MustCompile(`(?m)^        n  .*\(by reference\)  : Count\r?$`),
	regexp.MustCompile(`(?m)^        text  .*\(by reference\)  : String\r?$`),
	regexp.MustCompile(`(?m)^        p  .*\(by reference\)  : Point\r?$`),
}

// sessionStep is a command of a batch session and the event it must answer.
type sessionStep struct {
	command string
	event   *regexp.Regexp
}

// lineOf is the line of source that starts with marker, once its indentation is left out.
func lineOf(source, marker string) int {
	for i, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), marker) {
			return i + 1
		}
	}
	panic("the holdings program has no line that starts with " + marker)
}

func breakAt(line int, function string) sessionStep {
	return sessionStep{fmt.Sprintf("break %d", line), regexp.MustCompile(fmt.Sprintf(`^\{"event":"breakpoint","action":"set","function":"export::%s","file":"holdings.maxon","line":%d,`, function, line))}
}

func breakIn(function string) sessionStep {
	return sessionStep{"break " + function, regexp.MustCompile(fmt.Sprintf(`^\{"event":"breakpoint","action":"set","function":"export::%s","file":"holdings.maxon",`, function))}
}

func stopIn(command, function string) sessionStep {
	return sessionStep{command, regexp.MustCompile(fmt.Sprintf(`^\{"event":"stop","reason":"breakpoint","function":"export::%s",`, function))}
}

// valueOf is a print of a local or a path of fields and the value the debugger must read: its type, kind and display, which
// is JSON text.
func valueOf(path, typ, kind, display string) sessionStep {
	return sessionStep{"print " + path, regexp.MustCompile(fmt.Sprintf(`^\{"event":"value","value":\{"name":%s,"type":%s,"kind":%s,"display":"%s"\}\}$`,
		regexp.QuoteMeta(`"`+path+`"`), regexp.QuoteMeta(`"`+typ+`"`), regexp.QuoteMeta(`"`+kind+`"`), regexp.QuoteMeta(display)))}
}

func countOf(path, display string) sessionStep { return valueOf(path, "Count", "IntRanged", display) }

func textOf(path string, display string) sessionStep {
	return valueOf(path, "String", "String", display)
}

// holdingsSteps drive the holdings program: the stop in main before its calls reads the values it holds (every holder and
// form), and the stops in the functions read a parameter taken by value, by mutates, by sets and by consumes and a result.
func holdingsSteps() []sessionStep {
	stopAt := func(marker string) int { return lineOf(holdingsSource, marker) }
	return []sessionStep{
		breakAt(stopAt("let hit = match found"), "main"),
		breakIn("viewed"),
		breakAt(stopAt(`print("{p.x}`), "changed"),
		breakAt(stopAt(`print("{n}`), "grown"),
		breakAt(stopAt(`print("{text}`), "labelled"),
		breakIn("taken"),
		breakAt(stopAt("return r"), "made"),
		breakAt(stopAt(`print("{hit}`), "main"),
		stopIn("run", "main"),
		// A variable: a record held in place, a word, a String, a box and a union's box.
		countOf("local.y", "4"),
		countOf("count", "1"),
		textOf("text", `\"one\" (len=3)`),
		textOf("boxed.name", `\"boxed\" (len=5)`),
		// Fields: a word, a record held in place at two levels, a box's String and a union's payload.
		countOf("shape.tag", "15"),
		countOf("shape.edge.to.x", "12"),
		countOf("shape.edge.from.y", "11"),
		countOf("boxed.count", "16"),
		countOf("found.hit.y", "18"),
		// Elements: a record held in place, a word, a String, a byte, and a box.
		countOf("points.[1].x", "32"),
		countOf("points.[0].y", "31"),
		countOf("counts.[1]", "41"),
		textOf("names.[1]", `\"second\" (len=6)`),
		valueOf("colors.[2]", "Color", "Enum", "blue"),
		countOf("boxes.[1].count", "51"),
		textOf("boxes.[1].name", `\"fifty-one\" (len=9)`),
		// A parameter taken by value.
		stopIn("continue", "viewed"),
		countOf("p.x", "3"),
		countOf("p.y", "4"),
		// By mutates, in place, a word and a String by sets of the same address.
		stopIn("continue", "changed"),
		countOf("p.x", "70"),
		stopIn("continue", "grown"),
		countOf("n", "9"),
		stopIn("continue", "labelled"),
		textOf("text", `\"changed\" (len=7)`),
		// By consumes, and a result.
		stopIn("continue", "taken"),
		countOf("p.y", "41"),
		stopIn("continue", "made"),
		countOf("r.y", "22"),
		stopIn("continue", "main"),
		countOf("d.y", "22"),
		countOf("moved.x", "70"),
		countOf("count", "9"),
		textOf("text", `\"changed\" (len=7)`),
		{"continue", regexp.MustCompile(`^\{"event":"exit","code":0\}$`)},
	}
}

// batchSteps runs the batch session of the program at exe with the commands of steps, and holds its events to theirs.
func batchSteps(compiler, exe string, steps []sessionStep) error {
	commands := make([]string, len(steps))
	events := make([]*regexp.Regexp, len(steps))
	for i, s := range steps {
		commands[i], events[i] = s.command, s.event
	}
	return batchEvents(compiler, exe, "--commands="+strings.Join(commands, ";"), events)
}
