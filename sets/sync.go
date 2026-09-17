package sets

import (
	"iter"
	"sync"
)

// SyncSet serializes access to a private copy of another set's elements with a
// read-write mutex. The wrapped copy is not itself safe, so all access must go
// through the wrapper.
type SyncSet[T comparable] struct {
	inner Set[T]
	mutex sync.RWMutex
}

// NewSyncSet returns a thread-safe set holding a copy of the first non-nil
// argument's elements (via Clone, which preserves a LinkedSet's order); with no
// argument it uses a new HashSet. An argument that is already a [SyncSet] is
// returned unchanged rather than wrapped again.
//
// The source set is never retained: later changes to either set are not visible
// through the other.
func NewSyncSet[T comparable](sets ...Set[T]) *SyncSet[T] {
	var inner Set[T]
	for _, s := range sets {
		if s != nil {
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

// Add inserts x, reporting whether it was newly added.
func (s *SyncSet[T]) Add(x T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Add(x)
}

// AddAll inserts each item, reporting whether any was newly added. The whole
// batch lands under one lock, so other goroutines see it atomically.
func (s *SyncSet[T]) AddAll(items ...T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.AddAll(items...)
}

// Remove deletes x, reporting whether it was present.
func (s *SyncSet[T]) Remove(x T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Remove(x)
}

// RemoveAll deletes each item, reporting whether any was present. The whole
// batch lands under one lock.
func (s *SyncSet[T]) RemoveAll(items ...T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.RemoveAll(items...)
}

// Contains reports whether x is in the set.
func (s *SyncSet[T]) Contains(x T) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Contains(x)
}

// ContainsAll reports whether every item is in the set; true for an empty
// argument list.
func (s *SyncSet[T]) ContainsAll(items ...T) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsAll(items...)
}

// ContainsAny reports whether any item is in the set; false for an empty
// argument list.
func (s *SyncSet[T]) ContainsAny(items ...T) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsAny(items...)
}

// Retain removes every element other than x, reporting whether the set changed.
func (s *SyncSet[T]) Retain(x T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Retain(x)
}

// RetainAll keeps only the items, reporting whether the set changed. The whole
// batch lands under one lock.
func (s *SyncSet[T]) RetainAll(items ...T) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.RetainAll(items...)
}

// Size returns the number of elements.
func (s *SyncSet[T]) Size() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Size()
}

// IsEmpty reports whether the set has no elements.
func (s *SyncSet[T]) IsEmpty() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.IsEmpty()
}

// Clear removes every element.
func (s *SyncSet[T]) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.inner.Clear()
}

// Iter yields each element from a snapshot taken when iteration starts.
//
// The snapshot is taken when the range loop begins running the returned
// iterator, not when Iter is called: an element added before iteration starts
// is included, while mutations during iteration are not observed.
func (s *SyncSet[T]) Iter() iter.Seq[T] {
	return func(yield func(T) bool) {
		s.mutex.RLock()
		snapshot := s.inner.ToSlice()
		s.mutex.RUnlock()

		for _, item := range snapshot {
			if !yield(item) {
				return
			}
		}
	}
}

// ToSlice returns all elements as a snapshot; the slice does not alias the set.
func (s *SyncSet[T]) ToSlice() []T {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ToSlice()
}

// Clone returns an independent SyncSet holding a copy of the elements.
func (s *SyncSet[T]) Clone() Set[T] {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	cloned := s.inner.Clone()
	return &SyncSet[T]{
		inner: cloned,
	}
}
