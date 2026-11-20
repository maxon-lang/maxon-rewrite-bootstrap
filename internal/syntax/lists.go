package syntax

// listStack holds the elements of the lists being parsed, those of every list still open stacked one above the next.
// A list is copied out into a slice of exactly its length once it ends, so parsing it allocates nothing but that
// slice, where appending to a slice that grows would allocate several times its final size.
type listStack[T any] struct{ items []T }

func (s *listStack[T]) push(element T) { s.items = append(s.items, element) }

// mark is where the list about to be parsed starts.
func (s *listStack[T]) mark() int { return len(s.items) }

func (s *listStack[T]) pushedSince(mark int) bool { return len(s.items) > mark }

// take copies the elements pushed since mark into a new slice, which is nil when there are none, and pops them.
func (s *listStack[T]) take(mark int) []T {
	pushed := s.items[mark:]
	if len(pushed) == 0 {
		return nil
	}
	list := make([]T, len(pushed))
	copy(list, pushed)
	s.items = s.items[:mark]
	return list
}

// reset empties the stack, after a syntax error has abandoned the lists that were open.
func (s *listStack[T]) reset() { s.items = s.items[:0] }
