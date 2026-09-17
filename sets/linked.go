package sets

import (
	"iter"
)

type node[T comparable] struct {
	value T
	prev  *node[T]
	next  *node[T]
}

// LinkedSet is a [Set] backed by a hash map plus a doubly-linked list, so it
// keeps O(1) membership while iterating in insertion order. It is not safe for
// concurrent use; wrap it with [SyncSet].
type LinkedSet[T comparable] struct {
	nodes map[T]*node[T]
	head  *node[T]
	tail  *node[T]
}

// NewLinkedSet returns an empty insertion-ordered set, using the last positive
// size argument as the initial capacity of the internal map.
func NewLinkedSet[T comparable](size ...int) *LinkedSet[T] {
	c := 0
	for _, s := range size {
		if s > 0 {
			c = s
		}
	}
	return &LinkedSet[T]{
		nodes: make(map[T]*node[T], c),
	}
}

// Size returns the number of elements.
func (l *LinkedSet[T]) Size() int {
	return len(l.nodes)
}

// IsEmpty reports whether the set has no elements.
func (l *LinkedSet[T]) IsEmpty() bool {
	return l.Size() == 0
}

// Contains reports whether x is in the set.
func (l *LinkedSet[T]) Contains(x T) bool {
	_, exists := l.nodes[x]
	return exists
}

// ContainsAll reports whether every item is in the set; true for an empty
// argument list.
func (l *LinkedSet[T]) ContainsAll(items ...T) bool {
	for _, item := range items {
		if !l.Contains(item) {
			return false
		}
	}
	return true
}

// ContainsAny reports whether any item is in the set; false for an empty
// argument list.
func (l *LinkedSet[T]) ContainsAny(items ...T) bool {
	for _, item := range items {
		if l.Contains(item) {
			return true
		}
	}
	return false
}

// Add inserts x, appending it last, and reports whether it was newly added.
func (l *LinkedSet[T]) Add(x T) bool {
	if l.Contains(x) {
		return false
	}

	newNode := &node[T]{value: x}
	l.nodes[x] = newNode

	if l.tail == nil {
		l.head = newNode
		l.tail = newNode
	} else {
		l.tail.next = newNode
		newNode.prev = l.tail
		l.tail = newNode
	}

	return true
}

// AddAll inserts each item in order, reporting whether any was newly added.
func (l *LinkedSet[T]) AddAll(items ...T) bool {
	changed := false
	for _, item := range items {
		if l.Add(item) {
			changed = true
		}
	}
	return changed
}

func (l *LinkedSet[T]) removeNode(node *node[T]) {
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

// Remove deletes x, reporting whether it was present.
func (l *LinkedSet[T]) Remove(x T) bool {
	nodeToRemove, exists := l.nodes[x]
	if !exists {
		return false
	}

	delete(l.nodes, x)
	l.removeNode(nodeToRemove)

	return true
}

// RemoveAll deletes every present item, reporting whether the set changed.
func (l *LinkedSet[T]) RemoveAll(items ...T) bool {
	changed := false
	for _, item := range items {
		if l.Remove(item) {
			changed = true
		}
	}
	return changed
}

// Retain keeps only x, reporting whether the set changed.
func (l *LinkedSet[T]) Retain(x T) bool {
	return l.RetainAll(x)
}

// RetainAll keeps only the items, reporting whether the set changed; an empty
// list clears the set.
func (l *LinkedSet[T]) RetainAll(items ...T) bool {
	if len(items) == 0 {
		if l.IsEmpty() {
			return false
		}
		l.Clear()
		return true
	}

	toRetain := make(HashSet[T], len(items))
	for _, item := range items {
		toRetain[item] = struct{}{}
	}

	current := l.head
	changed := false

	for current != nil {
		next := current.next
		if !toRetain.Contains(current.value) {
			delete(l.nodes, current.value)
			l.removeNode(current)
			changed = true
		}
		current = next
	}

	return changed
}

// Clear removes every element.
func (l *LinkedSet[T]) Clear() {
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

// Iter yields each element in insertion order.
func (l *LinkedSet[T]) Iter() iter.Seq[T] {
	return func(yield func(T) bool) {
		current := l.head
		for current != nil {
			if !yield(current.value) {
				return
			}
			current = current.next
		}
	}
}

// ToSlice returns all elements in insertion order.
func (l *LinkedSet[T]) ToSlice() []T {
	result := make([]T, 0, l.Size())
	current := l.head
	for current != nil {
		result = append(result, current.value)
		current = current.next
	}
	return result
}

// Clone returns an independent LinkedSet with the same elements in insertion
// order.
func (l *LinkedSet[T]) Clone() Set[T] {
	cloned := NewLinkedSet[T](l.Size())
	current := l.head
	for current != nil {
		cloned.Add(current.value)
		current = current.next
	}
	return cloned
}
