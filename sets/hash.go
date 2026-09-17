package sets

import (
	"iter"
	"maps"
)

// HashSet is a [Set] backed by Go's built-in map. It is not safe for concurrent
// use; wrap it with [SyncSet].
type HashSet[T comparable] map[T]struct{}

// NewHashSet returns an empty HashSet, using the last positive size argument as
// the initial capacity.
func NewHashSet[T comparable](size ...int) HashSet[T] {
	c := 0
	for _, s := range size {
		if s > 0 {
			c = s
		}
	}
	return make(HashSet[T], c)
}

// Size returns the number of elements.
func (s HashSet[T]) Size() int {
	return len(s)
}

// IsEmpty reports whether the set has no elements.
func (s HashSet[T]) IsEmpty() bool {
	return s.Size() == 0
}

// Contains reports whether x is in the set.
func (s HashSet[T]) Contains(x T) bool {
	_, ok := s[x]
	return ok
}

// ContainsAll reports whether every item is in the set; true for an empty
// argument list.
func (s HashSet[T]) ContainsAll(items ...T) bool {
	for _, item := range items {
		if !s.Contains(item) {
			return false
		}
	}
	return true
}

// ContainsAny reports whether any item is in the set; false for an empty
// argument list.
func (s HashSet[T]) ContainsAny(items ...T) bool {
	for _, item := range items {
		if s.Contains(item) {
			return true
		}
	}
	return false
}

// Add inserts x when absent, reporting whether the set changed.
func (s HashSet[T]) Add(x T) bool {
	if s.Contains(x) {
		return false
	}
	s[x] = struct{}{}
	return true
}

// AddAll inserts every item not already present, reporting whether the set
// changed.
func (s HashSet[T]) AddAll(items ...T) bool {
	changed := false
	for _, item := range items {
		if s.Add(item) {
			changed = true
		}
	}
	return changed
}

// Remove deletes x, reporting whether it was present.
func (s HashSet[T]) Remove(x T) bool {
	if !s.Contains(x) {
		return false
	}
	delete(s, x)
	return true
}

// RemoveAll deletes every present item, reporting whether the set changed.
func (s HashSet[T]) RemoveAll(items ...T) bool {
	changed := false
	for _, item := range items {
		if s.Remove(item) {
			changed = true
		}
	}
	return changed
}

// Retain keeps only x, reporting whether the set changed.
func (s HashSet[T]) Retain(x T) bool {
	return s.RetainAll(x)
}

// RetainAll keeps only the items, reporting whether the set changed; an empty
// list clears the set.
func (s HashSet[T]) RetainAll(items ...T) bool {
	if len(items) == 0 {
		if s.IsEmpty() {
			return false
		}
		s.Clear()
		return true
	}

	toRetain := make(HashSet[T], len(items))
	for _, item := range items {
		toRetain[item] = struct{}{}
	}

	changed := false
	for key := range s {
		if !toRetain.Contains(key) {
			delete(s, key)
			changed = true
		}
	}

	return changed
}

// Clear removes every element.
func (s HashSet[T]) Clear() {
	clear(s)
}

// Iter yields each element in undefined order.
func (s HashSet[T]) Iter() iter.Seq[T] {
	return maps.Keys(s)
}

// ToSlice returns all elements in undefined order.
func (s HashSet[T]) ToSlice() []T {
	slice := make([]T, 0, s.Size())
	for x := range s {
		slice = append(slice, x)
	}
	return slice
}

// Clone returns an independent HashSet with the same elements.
func (s HashSet[T]) Clone() Set[T] {
	result := NewHashSet[T](s.Size())
	for x := range s {
		result.Add(x)
	}
	return result
}
