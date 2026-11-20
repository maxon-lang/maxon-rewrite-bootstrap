package ast

const (
	// firstSlabNodes is how many nodes a type's first chunk holds: a module that uses a type once or twice should not
	// pay for a large chunk of it.
	firstSlabNodes = 4
	// maxSlabNodes bounds a chunk, which is the most of one type a parse leaves unused at its end.
	maxSlabNodes = 64
)

// slab hands out the nodes of one type from chunks, each twice the size of the one before up to maxSlabNodes. A node
// never moves, because a chunk is never grown, so a pointer to one stays valid.
type slab[T any] struct{ chunk []T }

func (s *slab[T]) next() *T {
	if len(s.chunk) == cap(s.chunk) {
		s.chunk = make([]T, 0, min(max(2*cap(s.chunk), firstSlabNodes), maxSlabNodes))
	}
	s.chunk = s.chunk[:len(s.chunk)+1]
	return &s.chunk[len(s.chunk)-1]
}
