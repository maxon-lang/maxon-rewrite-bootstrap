package check

import (
	"slices"

	"minimaxon/internal/ast"
)

// placeDepth is how many fields deep the range analysis follows a place: 'x', 'x.f' and 'x.f.g'.
const placeDepth = 2

// place is storage that the range analysis follows: a local, or a field of one, or a field of that. Values have one owner
// and nothing else reaches them, so a place changes only where the code names it: an assignment to it or to what holds
// it, or a call that takes it to change it.
type place struct {
	root   ast.LocalID
	depth  uint8
	fields [placeDepth]*ast.FieldInfo
}

// field is the place of a field of p, if p is not too deep to follow one.
func (p place) field(f *ast.FieldInfo) (place, bool) {
	if int(p.depth) == placeDepth {
		return place{}, false
	}
	p.fields[p.depth] = f
	p.depth++
	return p, true
}

// holds is whether q is p or lies inside it.
func (p place) holds(q place) bool {
	if p.root != q.root || p.depth > q.depth {
		return false
	}
	for i := range p.depth {
		if p.fields[i] != q.fields[i] {
			return false
		}
	}
	return true
}

// overlaps is whether a change to p changes q, or the other way round.
func (p place) overlaps(q place) bool { return p.holds(q) || q.holds(p) }

// placeSpan is what is known of a field.
type placeSpan struct {
	at   place
	span span
}

// relation says that hi - lo >= gap: what 'hi >= lo' or 'hi > lo' proves, which a subtraction of the two reads.
type relation struct {
	hi, lo place
	gap    ast.I128
}

// rangeState is what the analysis knows at a point of a function: the span of each integer local by LocalID, the spans
// of the fields a condition narrowed, how places compare, or that no path reaches the point. A state that something keeps
// to come back to is kept, and is copied before it changes; the current state is changed in place otherwise, so a run of
// statements copies nothing.
type rangeState struct {
	spans     []span
	fields    []placeSpan
	relations []relation
	dead      bool
	kept      bool
}

// deadState is the state no path reaches, which holds nothing.
var deadState = &rangeState{dead: true, kept: true}

// The arena of the states allocates stateChunk states at once, and room for the spans of spanChunkStates states, or for
// minSpanChunk spans when that is more.
const (
	stateChunk      = 32
	spanChunkStates = 16
	minSpanChunk    = 256
)

// states makes the states of the functions being analysed. Every state of a function is dropped when the function has
// been walked, so the arena takes them from chunks that it reuses for the next function, and a function nested in one,
// the body of a behaviour, frees its states before the one around it does: a module allocates only for its largest
// function.
type states struct {
	structs     [][]rangeState
	spans       [][]span
	chunk, used int
	spanChunk   int
	spansUsed   int
}

// mark is where the arena is, which release goes back to.
type mark struct{ chunk, used, spanChunk, spansUsed int }

func (a *states) mark() mark { return mark{a.chunk, a.used, a.spanChunk, a.spansUsed} }

func (a *states) release(m mark) {
	a.chunk, a.used, a.spanChunk, a.spansUsed = m.chunk, m.used, m.spanChunk, m.spansUsed
}

// state is a new state, unkept and reachable, with room for n spans whose values the caller sets.
func (a *states) state(n int) *rangeState {
	if a.chunk == len(a.structs) {
		a.structs = append(a.structs, make([]rangeState, stateChunk))
	}
	s := &a.structs[a.chunk][a.used]
	if a.used++; a.used == stateChunk {
		a.chunk, a.used = a.chunk+1, 0
	}
	*s = rangeState{spans: a.take(n), fields: s.fields[:0], relations: s.relations[:0]}
	return s
}

// take is room for n spans.
func (a *states) take(n int) []span {
	for {
		if a.spanChunk == len(a.spans) {
			a.spans = append(a.spans, make([]span, max(spanChunkStates*n, minSpanChunk)))
		}
		if chunk := a.spans[a.spanChunk]; a.spansUsed+n <= len(chunk) {
			a.spansUsed += n
			return chunk[a.spansUsed-n : a.spansUsed : a.spansUsed]
		}
		a.spanChunk, a.spansUsed = a.spanChunk+1, 0
	}
}

func (a *states) clone(s *rangeState) *rangeState {
	out := a.state(len(s.spans))
	copy(out.spans, s.spans)
	out.fields = append(out.fields, s.fields...)
	out.relations = append(out.relations, s.relations...)
	out.dead = s.dead
	return out
}

// at is what is known of an integer place, if anything beyond its type.
func (s *rangeState) at(p place) (span, bool) {
	if p.depth == 0 {
		return s.spans[p.root], true
	}
	for _, f := range s.fields {
		if f.at == p {
			return f.span, true
		}
	}
	return span{}, false
}

// refine records what is known of an integer place.
func (s *rangeState) refine(p place, v span) {
	if p.depth == 0 {
		s.spans[p.root] = v
		return
	}
	for i := range s.fields {
		if s.fields[i].at == p {
			s.fields[i].span = v
			return
		}
	}
	s.fields = append(s.fields, placeSpan{at: p, span: v})
}

// relate records that hi - lo >= gap.
func (s *rangeState) relate(hi, lo place, gap ast.I128) {
	for i := range s.relations {
		if r := &s.relations[i]; r.hi == hi && r.lo == lo {
			r.gap = ast.MaxI(r.gap, gap)
			return
		}
	}
	s.relations = append(s.relations, relation{hi: hi, lo: lo, gap: gap})
}

// gap is how far hi is known to lie above lo, if anything is known of it.
func (s *rangeState) gap(hi, lo place) (ast.I128, bool) {
	for _, r := range s.relations {
		if r.hi == hi && r.lo == lo {
			return r.gap, true
		}
	}
	return ast.I128{}, false
}

// drop forgets the fields and relations that name a place gone says is gone. A local's own span stays: whoever changed it
// sets the new one.
func (s *rangeState) drop(gone func(place) bool) {
	s.fields = slices.DeleteFunc(s.fields, func(f placeSpan) bool { return gone(f.at) })
	s.relations = slices.DeleteFunc(s.relations, func(r relation) bool { return gone(r.hi) || gone(r.lo) })
}

// joined is the state where the paths of a and b meet, which may be a or b itself; nil is no path, as deadState is. It
// knows what both know.
func (st *states) joined(a, b *rangeState) *rangeState {
	switch {
	case (a == nil || a.dead) && (b == nil || b.dead):
		return deadState
	case a == nil || a.dead:
		return b
	case b == nil || b.dead || a == b:
		return a
	}
	out := st.state(len(a.spans))
	for i := range out.spans {
		out.spans[i] = a.spans[i].union(b.spans[i])
	}
	for _, f := range a.fields {
		if other, ok := b.at(f.at); ok {
			out.fields = append(out.fields, placeSpan{at: f.at, span: f.span.union(other)})
		}
	}
	for _, r := range a.relations {
		if gap, ok := b.gap(r.hi, r.lo); ok {
			out.relations = append(out.relations, relation{hi: r.hi, lo: r.lo, gap: ast.MinI(r.gap, gap)})
		}
	}
	return out
}
