package sets

import (
	"iter"
	"sync"
)

// SyncSet provides a thread-safe wrapper around a private copy of another
// set's elements. It uses a read-write mutex to allow concurrent reads while
// ensuring exclusive access for write operations; see NewSyncSet for the
// copying semantics.
//
// The implementation is designed to minimize lock contention:
//   - Read operations (Contains, Size, etc.) use read locks
//   - Write operations (Add, Remove, etc.) use write locks
//   - Iteration creates a snapshot to avoid holding locks during iteration
type SyncSet[T comparable] struct {
	inner Set[T]       // The underlying Set implementation
	mutex sync.RWMutex // Read-write mutex for thread safety
}

// NewSyncSet creates a new thread-safe set wrapper.
// If a Set is provided, its elements are copied into the wrapper (via Clone,
// which preserves the source's iteration order for a LinkedSet); otherwise a
// new HashSet is used as the backing store.
// If the provided set is already a SyncSet, it returns the same instance
// to avoid double-wrapping.
//
// The source set is never retained by the wrapper: the caller may keep using
// it freely, and changes made to it afterwards are not visible through the
// wrapper. Conversely, operations on the wrapper do not affect the source.
// Use the wrapper for all subsequent access if you want a single coherent view
// of the set.
//
// Examples:
//
//	SyncSet := NewSyncSet[int]()                    // uses a new HashSet
//	SyncSet := NewSyncSet(NewLinkedSet[string]())   // copies a LinkedSet
//	SyncSet := NewSyncSet(existingSyncSet)          // returns existingSyncSet
func NewSyncSet[T comparable](sets ...Set[T]) *SyncSet[T] {
	var inner Set[T]
	for _, s := range sets {
		if s != nil {
			// Avoid double-wrapping if the provided set is already thread-safe
			if ss, ok := s.(*SyncSet[T]); ok {
				return ss
			}
			inner = s.Clone()
			break
		}
	}

	if inner == nil {
		inner = NewHashSet[T]()
	}

	return &SyncSet[T]{
		inner: inner,
	}
}

// Add safely adds an element with exclusive access.
// Uses a write lock to ensure thread safety.
func (s *SyncSet[T]) Add(x T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Add(x)
}

// AddAll safely adds multiple elements with exclusive access.
// The entire operation is atomic - either all applicable elements
// are added or none are (in case of panic).
func (s *SyncSet[T]) AddAll(items ...T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.AddAll(items...)
}

// Remove safely removes an element with exclusive access.
// Uses a write lock to ensure thread safety.
func (s *SyncSet[T]) Remove(x T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Remove(x)
}

// RemoveAll safely removes multiple elements with exclusive access.
// The entire operation is atomic.
func (s *SyncSet[T]) RemoveAll(items ...T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.RemoveAll(items...)
}

// Contains safely checks element existence with shared access.
// Uses a read lock to allow concurrent reads while preventing
// concurrent writes.
func (s *SyncSet[T]) Contains(x T) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Contains(x)
}

// ContainsAll safely checks multiple elements with shared access.
// Uses a read lock for the entire operation.
func (s *SyncSet[T]) ContainsAll(items ...T) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsAll(items...)
}

// ContainsAny safely checks for any matching elements with shared access.
// Uses a read lock for the entire operation.
func (s *SyncSet[T]) ContainsAny(items ...T) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsAny(items...)
}

// Retain safely keeps only the specified element with exclusive access.
// Uses a write lock as this is a mutating operation.
func (s *SyncSet[T]) Retain(x T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Retain(x)
}

// RetainAll safely retains only specified elements with exclusive access.
// The entire operation is atomic.
func (s *SyncSet[T]) RetainAll(items ...T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.RetainAll(items...)
}

// Size safely returns the element count with shared access.
// Uses a read lock to ensure a consistent view.
func (s *SyncSet[T]) Size() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Size()
}

// IsEmpty safely checks if the set is empty with shared access.
// Uses a read lock to ensure a consistent view.
func (s *SyncSet[T]) IsEmpty() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.IsEmpty()
}

// Clear safely removes all elements with exclusive access.
// Uses a write lock as this is a mutating operation.
func (s *SyncSet[T]) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.inner.Clear()
}

// Iter returns a thread-safe iterator by creating a snapshot.
// This approach avoids holding locks during iteration, which could
// cause deadlocks or performance issues with long-running iterations.
//
// Iter() only returns the iterator function; the snapshot is taken when the
// iteration actually starts (when the returned function is first invoked),
// not when Iter() is called. Changes made to the set between the Iter() call
// and the start of iteration are therefore visible, while mutations that
// happen after the iteration has started are not reflected. This provides a
// consistent view for the duration of the iteration.
//
// Example:
//
//	for element := range SyncSet.Iter() {
//	    // This iteration is safe and won't block other operations
//	    fmt.Println(element)
//	}
func (s *SyncSet[T]) Iter() iter.Seq[T] {
	return func(yield func(T) bool) {
		// Create a snapshot with minimal lock time
		s.mutex.RLock()
		snapshot := s.inner.ToSlice()
		s.mutex.RUnlock()

		// Iterate over the snapshot without holding any locks
		for _, item := range snapshot {
			if !yield(item) {
				return
			}
		}
	}
}

// ToSlice safely returns a snapshot of all elements.
// Uses a read lock to ensure a consistent view.
// The returned slice is independent of the set.
func (s *SyncSet[T]) ToSlice() []T {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ToSlice()
}

// Clone safely creates an independent copy of the set.
// The clone is also a SyncSet wrapping a copy of the inner set.
// Uses a read lock to ensure a consistent snapshot for cloning.
func (s *SyncSet[T]) Clone() Set[T] {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	cloned := s.inner.Clone()
	return &SyncSet[T]{
		inner: cloned,
	}
}
