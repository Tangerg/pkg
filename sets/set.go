package sets

import (
	"iter"
)

// Set is a collection with no duplicate elements: it contains no pair of
// elements e1 and e2 with e1 == e2. Implementations are [HashSet], [LinkedSet],
// and [SyncSet]; all work with any comparable element type.
//
// A set's behavior is unspecified if an element is mutated while in the set so
// that its equality changes.
type Set[T comparable] interface {
	// Size returns the number of elements.
	Size() int

	// IsEmpty reports whether the set has no elements.
	IsEmpty() bool

	// Contains reports whether x is in the set.
	Contains(x T) bool

	// ContainsAll reports whether every item is in the set; true for an empty
	// argument list.
	ContainsAll(items ...T) bool

	// ContainsAny reports whether any item is in the set; false for an empty
	// argument list.
	ContainsAny(items ...T) bool

	// Add inserts x when absent, reporting whether the set changed.
	Add(x T) bool

	// AddAll inserts every item not already present, reporting whether the set
	// changed.
	AddAll(items ...T) bool

	// Remove deletes x, reporting whether it was present.
	Remove(x T) bool

	// RemoveAll deletes every present item, reporting whether the set changed.
	RemoveAll(items ...T) bool

	// Retain keeps only x, reporting whether the set changed.
	Retain(x T) bool

	// RetainAll keeps only the items, reporting whether the set changed; an
	// empty list clears the set.
	RetainAll(items ...T) bool

	// Clear removes every element.
	Clear()

	// Iter returns an iterator over the elements. The order is
	// implementation-defined: HashSet is unordered, LinkedSet follows insertion
	// order, and SyncSet follows its backing set over a snapshot.
	Iter() iter.Seq[T]

	// ToSlice returns a slice containing all of the elements in this set. The
	// returned slice does not alias the set, so the caller may modify it freely.
	//
	// If the set guarantees an order, the elements appear in that order.
	ToSlice() []T

	// Clone returns a shallow copy: an independent set holding the same element
	// values.
	Clone() Set[T]
}
