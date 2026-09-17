package maps

import (
	"iter"

	pkgSlices "github.com/Tangerg/pkg/slices"
)

type mapNode[K comparable, V any] struct {
	key   K
	value V
	prev  *mapNode[K, V]
	next  *mapNode[K, V]
}

// LinkedMap is a [Map] backed by a hash map plus a doubly-linked list, so it
// keeps O(1) lookups while iterating in insertion order. It is not safe for
// concurrent use; wrap it with [SyncMap].
type LinkedMap[K comparable, V any] struct {
	nodes map[K]*mapNode[K, V]
	head  *mapNode[K, V]
	tail  *mapNode[K, V]
}

// NewLinkedMap returns an empty LinkedMap with capacity for size entries.
func NewLinkedMap[K comparable, V any](size ...int) *LinkedMap[K, V] {
	c, _ := pkgSlices.First(size)
	if c <= 0 {
		c = 0
	}
	return &LinkedMap[K, V]{
		nodes: make(map[K]*mapNode[K, V], c),
	}
}

// Put associates value with key, returning the previous value and whether the
// key was already present. An existing key keeps its insertion-order position.
func (l *LinkedMap[K, V]) Put(key K, value V) (V, bool) {
	if existingNode, exists := l.nodes[key]; exists {
		oldValue := existingNode.value
		existingNode.value = value
		return oldValue, true
	}

	newNode := &mapNode[K, V]{key: key, value: value}
	l.nodes[key] = newNode

	if l.tail == nil {
		l.head = newNode
		l.tail = newNode
	} else {
		l.tail.next = newNode
		newNode.prev = l.tail
		l.tail = newNode
	}

	var zero V
	return zero, false
}

// Get returns the value for key and whether it is present.
func (l *LinkedMap[K, V]) Get(key K) (V, bool) {
	if node, exists := l.nodes[key]; exists {
		return node.value, true
	}
	var zero V
	return zero, false
}

// Remove deletes key, returning its value and whether it was present.
func (l *LinkedMap[K, V]) Remove(key K) (V, bool) {
	nodeToRemove, exists := l.nodes[key]
	if !exists {
		var zero V
		return zero, false
	}

	oldValue := nodeToRemove.value

	delete(l.nodes, key)

	l.removeNode(nodeToRemove)

	return oldValue, true
}

func (l *LinkedMap[K, V]) removeNode(node *mapNode[K, V]) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		l.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		l.tail = node.prev
	}

	// Drop the node's own links so it does not keep its neighbours alive.
	node.prev = nil
	node.next = nil
}

// ContainsKey reports whether key is present.
func (l *LinkedMap[K, V]) ContainsKey(key K) bool {
	_, exists := l.nodes[key]
	return exists
}

// ContainsValue reports whether any key maps to value, compared with
// [valuesEqual].
func (l *LinkedMap[K, V]) ContainsValue(value V) bool {
	current := l.head
	for current != nil {
		if valuesEqual(current.value, value) {
			return true
		}
		current = current.next
	}
	return false
}

// Size returns the number of entries.
func (l *LinkedMap[K, V]) Size() int {
	return len(l.nodes)
}

// IsEmpty reports whether the map has no entries.
func (l *LinkedMap[K, V]) IsEmpty() bool {
	return l.Size() == 0
}

// Clear removes every entry.
func (l *LinkedMap[K, V]) Clear() {
	current := l.head
	for current != nil {
		next := current.next
		current.prev = nil
		current.next = nil
		current = next
	}

	clear(l.nodes)
	l.head = nil
	l.tail = nil
}

// PutAll copies every mapping of other into this map. New keys are appended in
// the order encountered; existing keys keep their position.
func (l *LinkedMap[K, V]) PutAll(other Map[K, V]) {
	other.ForEach(func(k K, v V) {
		l.Put(k, v)
	})
}

// Keys returns all keys in insertion order; the slice does not alias the map.
func (l *LinkedMap[K, V]) Keys() []K {
	keys := make([]K, 0, l.Size())
	current := l.head
	for current != nil {
		keys = append(keys, current.key)
		current = current.next
	}
	return keys
}

// Values returns all values in insertion order; the slice does not alias the map.
func (l *LinkedMap[K, V]) Values() []V {
	values := make([]V, 0, l.Size())
	current := l.head
	for current != nil {
		values = append(values, current.value)
		current = current.next
	}
	return values
}

// Entries returns all key-value pairs in insertion order; the slice does not
// alias the map.
func (l *LinkedMap[K, V]) Entries() []*Entry[K, V] {
	entries := make([]*Entry[K, V], 0, l.Size())
	current := l.head
	for current != nil {
		entries = append(entries, &Entry[K, V]{
			key:   current.key,
			value: current.value,
		})
		current = current.next
	}
	return entries
}

// ForEach calls action once per entry in insertion order.
func (l *LinkedMap[K, V]) ForEach(action func(K, V)) {
	current := l.head
	for current != nil {
		action(current.key, current.value)
		current = current.next
	}
}

// GetOrDefault returns the value for key, or defaultValue when key is absent.
func (l *LinkedMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	if value, exists := l.Get(key); exists {
		return value
	}
	return defaultValue
}

// PutIfAbsent associates value with key only when key is absent (appending it
// last), returning the value now mapped to key and whether a mapping was stored.
func (l *LinkedMap[K, V]) PutIfAbsent(key K, value V) (V, bool) {
	if existingValue, exists := l.Get(key); exists {
		return existingValue, false
	}
	l.Put(key, value)
	return value, true
}

// RemoveIf deletes key only when it currently equals value, compared with
// [valuesEqual].
func (l *LinkedMap[K, V]) RemoveIf(key K, value V) bool {
	if node, exists := l.nodes[key]; exists {
		if valuesEqual(node.value, value) {
			l.Remove(key)
			return true
		}
	}
	return false
}

// Replace replaces the value for key only when key is present, keeping its
// position, and returns the previous value and whether it was replaced.
func (l *LinkedMap[K, V]) Replace(key K, value V) (V, bool) {
	if node, exists := l.nodes[key]; exists {
		oldValue := node.value
		node.value = value
		return oldValue, true
	}
	var zero V
	return zero, false
}

// ReplaceIf replaces the value for key only when it currently equals oldValue,
// compared with [valuesEqual].
func (l *LinkedMap[K, V]) ReplaceIf(key K, oldValue, newValue V) bool {
	if node, exists := l.nodes[key]; exists {
		if valuesEqual(node.value, oldValue) {
			node.value = newValue
			return true
		}
	}
	return false
}

// Compute derives a new mapping for key from its current value and existence:
// it stores the returned value when the flag is true, and removes an existing
// mapping when it is false.
func (l *LinkedMap[K, V]) Compute(key K, remappingFunc func(K, V, bool) (V, bool)) (V, bool) {
	currentValue, exists := l.Get(key)
	newValue, shouldPut := remappingFunc(key, currentValue, exists)

	if shouldPut {
		l.Put(key, newValue)
		return newValue, true
	} else if exists {
		l.Remove(key)
	}

	var zero V
	return zero, false
}

// ComputeIfAbsent returns the value for key, computing and appending it when
// key is absent.
func (l *LinkedMap[K, V]) ComputeIfAbsent(key K, mappingFunction func(K) V) V {
	if value, exists := l.Get(key); exists {
		return value
	}

	newValue := mappingFunction(key)
	l.Put(key, newValue)
	return newValue
}

// ComputeIfPresent derives a new value for key only when key is present,
// keeping its position, and returns the new value and whether it was updated.
func (l *LinkedMap[K, V]) ComputeIfPresent(key K, remappingFunc func(K, V) V) (V, bool) {
	if oldValue, exists := l.Get(key); exists {
		newValue := remappingFunc(key, oldValue)
		l.Put(key, newValue)
		return newValue, true
	}

	var zero V
	return zero, false
}

// Merge stores value under key when absent (appending it); otherwise it stores
// remappingFunc(current, value), keeping the key's position.
func (l *LinkedMap[K, V]) Merge(key K, value V, remappingFunc func(V, V) V) V {
	if oldValue, exists := l.Get(key); exists {
		newValue := remappingFunc(oldValue, value)
		l.Put(key, newValue)
		return newValue
	}

	l.Put(key, value)
	return value
}

// ReplaceAll replaces each entry's value with function(key, value), leaving the
// keys and insertion order unchanged.
func (l *LinkedMap[K, V]) ReplaceAll(function func(K, V) V) {
	current := l.head
	for current != nil {
		current.value = function(current.key, current.value)
		current = current.next
	}
}

// Iter yields each key-value pair in insertion order.
func (l *LinkedMap[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		current := l.head
		for current != nil {
			if !yield(current.key, current.value) {
				return
			}
			current = current.next
		}
	}
}

// IterKeys yields each key in insertion order.
func (l *LinkedMap[K, V]) IterKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		current := l.head
		for current != nil {
			if !yield(current.key) {
				return
			}
			current = current.next
		}
	}
}

// IterValues yields each value in insertion order.
func (l *LinkedMap[K, V]) IterValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		current := l.head
		for current != nil {
			if !yield(current.value) {
				return
			}
			current = current.next
		}
	}
}

// Clone returns an independent LinkedMap with the same entries in insertion
// order. The copy is shallow.
func (l *LinkedMap[K, V]) Clone() Map[K, V] {
	cloned := NewLinkedMap[K, V](l.Size())
	cloned.PutAll(l)
	return cloned
}

// First returns the first key-value pair in insertion order.
// Returns zero values and false if the map is empty.
func (l *LinkedMap[K, V]) First() (K, V, bool) {
	if l.head == nil {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
	return l.head.key, l.head.value, true
}

// Last returns the last key-value pair in insertion order.
// Returns zero values and false if the map is empty.
func (l *LinkedMap[K, V]) Last() (K, V, bool) {
	if l.tail == nil {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
	return l.tail.key, l.tail.value, true
}

// RemoveFirst removes and returns the first key-value pair in insertion order.
// Returns zero values and false if the map is empty.
func (l *LinkedMap[K, V]) RemoveFirst() (K, V, bool) {
	if l.head == nil {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}

	key := l.head.key
	value := l.head.value
	l.Remove(key)
	return key, value, true
}

// RemoveLast removes and returns the last key-value pair in insertion order.
// Returns zero values and false if the map is empty.
func (l *LinkedMap[K, V]) RemoveLast() (K, V, bool) {
	if l.tail == nil {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}

	key := l.tail.key
	value := l.tail.value
	l.Remove(key)
	return key, value, true
}
