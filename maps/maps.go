package maps

import (
	"iter"
)

// Map defines the interface shared by the map implementations in this package.
type Map[K comparable, V any] interface {
	// Put associates value with key, returning the previous value and whether the
	// key was already present.
	Put(key K, value V) (V, bool)

	// Get returns the value for key and whether it is present.
	Get(key K) (V, bool)

	// Remove deletes key, returning its value and whether it was present.
	Remove(key K) (V, bool)

	// ContainsKey reports whether key is present.
	ContainsKey(key K) bool

	// ContainsValue reports whether any key maps to value.
	ContainsValue(value V) bool

	// Size returns the number of entries.
	Size() int

	// IsEmpty reports whether the map has no entries.
	IsEmpty() bool

	// Clear removes every entry.
	Clear()

	// PutAll copies every mapping of other into this map.
	PutAll(other Map[K, V])

	// Keys returns all keys as a snapshot that does not alias the map.
	Keys() []K

	// Values returns all values as a snapshot that does not alias the map.
	Values() []V

	// Entries returns all key-value pairs as a snapshot that does not alias the
	// map.
	Entries() []*Entry[K, V]

	// ForEach calls action once per entry in an implementation-defined order.
	ForEach(action func(K, V))

	// GetOrDefault returns the value for key, or defaultValue when key is absent.
	GetOrDefault(key K, defaultValue V) V

	// PutIfAbsent associates value with key only when key is absent, returning
	// the value now mapped to key and whether a mapping was stored.
	PutIfAbsent(key K, value V) (V, bool)

	// RemoveIf deletes key only when it currently equals value.
	RemoveIf(key K, value V) bool

	// Replace replaces the value for key only when key is present, returning the
	// previous value and whether the replacement happened.
	Replace(key K, value V) (V, bool)

	// ReplaceIf replaces the value for key only when it currently equals oldValue.
	ReplaceIf(key K, oldValue, newValue V) bool

	// Compute derives a new mapping for key from its current value and existence:
	// it stores the returned value when the flag is true, and removes an existing
	// mapping when it is false.
	Compute(key K, remappingFunction func(K, V, bool) (V, bool)) (V, bool)

	// ComputeIfAbsent returns the value for key, computing and storing it when the
	// key is absent.
	ComputeIfAbsent(key K, mappingFunction func(K) V) V

	// ComputeIfPresent derives a new value for key only when it is present,
	// returning the new value and whether it was updated.
	ComputeIfPresent(key K, remappingFunction func(K, V) V) (V, bool)

	// Merge stores value under key when absent; otherwise it stores
	// remappingFunction(current, value).
	Merge(key K, value V, remappingFunction func(V, V) V) V

	// ReplaceAll replaces each entry's value with function(key, value).
	ReplaceAll(function func(K, V) V)

	// Iter returns an iterator that yields key-value pairs. The order is
	// implementation-defined: HashMap is unordered while LinkedMap follows
	// insertion order.
	Iter() iter.Seq2[K, V]

	// IterKeys returns an iterator that yields keys only, in the same order as
	// Iter.
	IterKeys() iter.Seq[K]

	// IterValues returns an iterator that yields values only, in the same order
	// as Iter.
	IterValues() iter.Seq[V]

	// Clone returns an independent copy of this map with the same entries. The
	// copy is shallow: pointer, slice, and map values are shared with the
	// original.
	Clone() Map[K, V]
}

// Entry represents a key-value pair in the map. It is immutable: both
// components are fixed at construction.
type Entry[K comparable, V any] struct {
	key   K
	value V
}

// Key returns the key corresponding to this entry.
func (e *Entry[K, V]) Key() K {
	return e.key
}

// Value returns the value corresponding to this entry.
func (e *Entry[K, V]) Value() V {
	return e.value
}
