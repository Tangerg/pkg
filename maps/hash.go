package maps

import (
	"iter"

	pkgSlices "github.com/Tangerg/pkg/slices"
)

// HashMap is a [Map] backed by Go's built-in map. It is not safe for
// concurrent use; wrap it with [SyncMap] to share it across goroutines.
type HashMap[K comparable, V any] map[K]V

// NewHashMap returns an empty HashMap with capacity for size entries.
func NewHashMap[K comparable, V any](size ...int) HashMap[K, V] {
	c, _ := pkgSlices.First(size)
	if c <= 0 {
		c = 0
	}
	return make(HashMap[K, V], c)
}

// Put associates value with key, returning the previous value and whether the
// key was already present.
func (h HashMap[K, V]) Put(key K, value V) (V, bool) {
	oldValue, exists := h[key]
	h[key] = value
	return oldValue, exists
}

// Get returns the value for key and whether it is present.
func (h HashMap[K, V]) Get(key K) (V, bool) {
	value, exists := h[key]
	return value, exists
}

// Remove deletes key, returning its value and whether it was present.
func (h HashMap[K, V]) Remove(key K) (V, bool) {
	value, exists := h[key]
	if exists {
		delete(h, key)
	}
	return value, exists
}

// ContainsKey reports whether key is present.
func (h HashMap[K, V]) ContainsKey(key K) bool {
	_, exists := h[key]
	return exists
}

// ContainsValue reports whether any key maps to value.
// Values are compared with [valuesEqual].
func (h HashMap[K, V]) ContainsValue(value V) bool {
	for _, v := range h {
		if valuesEqual(v, value) {
			return true
		}
	}
	return false
}

// Size returns the number of entries.
func (h HashMap[K, V]) Size() int {
	return len(h)
}

// IsEmpty reports whether the map has no entries.
func (h HashMap[K, V]) IsEmpty() bool {
	return h.Size() == 0
}

// Clear removes every entry.
func (h HashMap[K, V]) Clear() {
	clear(h)
}

// PutAll copies every mapping of other into this map.
func (h HashMap[K, V]) PutAll(other Map[K, V]) {
	other.ForEach(func(k K, v V) {
		h[k] = v
	})
}

// Keys returns all keys as a snapshot; the slice does not alias the map.
func (h HashMap[K, V]) Keys() []K {
	keys := make([]K, 0, h.Size())
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

// Values returns all values as a snapshot; the slice does not alias the map.
func (h HashMap[K, V]) Values() []V {
	values := make([]V, 0, h.Size())
	for _, v := range h {
		values = append(values, v)
	}
	return values
}

// Entries returns all key-value pairs as a snapshot; the slice does not alias
// the map.
func (h HashMap[K, V]) Entries() []*Entry[K, V] {
	entries := make([]*Entry[K, V], 0, h.Size())
	for k, v := range h {
		entries = append(entries, &Entry[K, V]{
			key:   k,
			value: v,
		})
	}
	return entries
}

// ForEach calls action once per entry in an unspecified order.
func (h HashMap[K, V]) ForEach(action func(K, V)) {
	for k, v := range h {
		action(k, v)
	}
}

// GetOrDefault returns the value for key, or defaultValue when key is absent.
func (h HashMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	if value, exists := h[key]; exists {
		return value
	}
	return defaultValue
}

// PutIfAbsent associates value with key only when key is absent, returning the
// value now mapped to key and whether a mapping was stored.
func (h HashMap[K, V]) PutIfAbsent(key K, value V) (V, bool) {
	if existingValue, exists := h[key]; exists {
		return existingValue, false
	}
	h[key] = value
	return value, true
}

// RemoveIf deletes key only when it currently equals value, compared with
// [valuesEqual].
func (h HashMap[K, V]) RemoveIf(key K, value V) bool {
	if existingValue, exists := h[key]; exists && valuesEqual(existingValue, value) {
		delete(h, key)
		return true
	}
	return false
}

// Replace replaces the value for key only when key is present, returning the
// previous value and whether the replacement happened.
func (h HashMap[K, V]) Replace(key K, value V) (V, bool) {
	if oldValue, exists := h[key]; exists {
		h[key] = value
		return oldValue, true
	}
	var zero V
	return zero, false
}

// ReplaceIf replaces the value for key only when it currently equals oldValue,
// compared with [valuesEqual].
func (h HashMap[K, V]) ReplaceIf(key K, oldValue, newValue V) bool {
	if existingValue, exists := h[key]; exists && valuesEqual(existingValue, oldValue) {
		h[key] = newValue
		return true
	}
	return false
}

// Compute derives a new mapping for key from its current value and existence:
// it stores the returned value when the flag is true, and removes an existing
// mapping when it is false.
func (h HashMap[K, V]) Compute(key K, remappingFunc func(K, V, bool) (V, bool)) (V, bool) {
	oldValue, exists := h[key]
	newValue, shouldPut := remappingFunc(key, oldValue, exists)

	if shouldPut {
		h[key] = newValue
		return newValue, true
	} else if exists {
		delete(h, key)
	}

	var zero V
	return zero, false
}

// ComputeIfAbsent returns the value for key, computing and storing it when key
// is absent.
func (h HashMap[K, V]) ComputeIfAbsent(key K, mappingFunction func(K) V) V {
	if value, exists := h[key]; exists {
		return value
	}

	newValue := mappingFunction(key)
	h[key] = newValue
	return newValue
}

// ComputeIfPresent derives a new value for key only when key is present,
// returning the new value and whether it was updated.
func (h HashMap[K, V]) ComputeIfPresent(key K, remappingFunc func(K, V) V) (V, bool) {
	if oldValue, exists := h[key]; exists {
		newValue := remappingFunc(key, oldValue)
		h[key] = newValue
		return newValue, true
	}

	var zero V
	return zero, false
}

// Merge stores value under key when absent; otherwise it stores
// remappingFunc(current, value).
func (h HashMap[K, V]) Merge(key K, value V, remappingFunc func(V, V) V) V {
	if oldValue, exists := h[key]; exists {
		newValue := remappingFunc(oldValue, value)
		h[key] = newValue
		return newValue
	}

	h[key] = value
	return value
}

// ReplaceAll replaces each entry's value with function(key, value).
func (h HashMap[K, V]) ReplaceAll(function func(K, V) V) {
	for k, v := range h {
		h[k] = function(k, v)
	}
}

// Iter yields each key-value pair in an unspecified order.
func (h HashMap[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range h {
			if !yield(k, v) {
				return
			}
		}
	}
}

// IterKeys yields each key in an unspecified order.
func (h HashMap[K, V]) IterKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range h {
			if !yield(k) {
				return
			}
		}
	}
}

// IterValues yields each value in an unspecified order.
func (h HashMap[K, V]) IterValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range h {
			if !yield(v) {
				return
			}
		}
	}
}

// Clone returns an independent HashMap with the same entries. The copy is
// shallow: pointer, slice, and map values are shared with the original.
func (h HashMap[K, V]) Clone() Map[K, V] {
	cloned := NewHashMap[K, V](h.Size())
	cloned.PutAll(h)
	return cloned
}
