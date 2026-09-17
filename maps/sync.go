// Package maps /* packages
// Thread-safe Map implementations:
//
// 1. SyncMap (Recommended default):
//   - Wraps any Map implementation with RWMutex
//   - Preserves wrapped map's characteristics (order, etc.)
//   - Good balance of performance and flexibility
//   - Use: NewSyncMap() or NewSyncMap(existingMap)
//
// 2. StdSyncMap (High-concurrency reads):
//   - Based on Go's sync.Map
//   - Optimized for read-heavy workloads
//   - No order guarantees
//   - Use: NewStdSyncMap()
//
// Example:
//
//	general := NewSyncMap[string, int]()           // General purpose
//	ordered := NewSyncMap(NewLinkedMap[...])       // Ordered + thread-safe
//	readHeavy := NewStdSyncMap[string, int]()      // Read-optimized
package maps

import (
	"iter"
	"reflect"
	"sync"
)

// valuesEqual reports whether a and b hold equal values.
//
// Values whose dynamic type supports == are compared with ==, which is cheap
// and gives the same answer for the scalar types most maps use. Values whose
// dynamic type does not support == (slices, maps, funcs, or structs containing
// them) fall back to [reflect.DeepEqual].
//
// The dynamic-type check matters: comparing two interface values that hold the
// same uncomparable type with == panics with "runtime error: comparing
// uncomparable type", which is exactly what sync.Map's CompareAndSwap and
// CompareAndDelete do internally. Because [Map] leaves V unconstrained, every
// comparison-based operation has to go through this helper to stay usable with
// all the value types [HashMap] and [LinkedMap] accept.
func valuesEqual[V any](a, b V) bool {
	if isComparableValue(a) && isComparableValue(b) {
		return any(a) == any(b)
	}
	return reflect.DeepEqual(a, b)
}

// isComparableValue reports whether the dynamic type stored in v supports ==,
// i.e. whether comparing two interface values holding it is safe. Nil is
// trivially comparable: sync.Map handles the nil interface itself, while a
// direct == on it is not even reached.
func isComparableValue(v any) bool {
	return v == nil || reflect.TypeOf(v).Comparable()
}

// unwrap returns the value a sync.Map holds for a key, given the type it was
// stored with.
//
// A plain v.(T) panics when T is an interface type and the stored value is the
// nil interface: a nil interface has no dynamic type to assert. The comma-ok
// form maps that case to the zero value, which is precisely the value that was
// stored, and it cannot hide a mismatched type: every entry of the underlying
// sync.Map was stored by this type as a K or a V.
func unwrap[T any](v any) T {
	t, _ := v.(T)
	return t
}

// pendingEntry is a key paired with a value computed while no lock was held,
// used by [SyncMap.ReplaceAll] to apply its snapshot under a single write lock.
type pendingEntry[K comparable, V any] struct {
	key   K
	value V
}

// SyncMap provides a thread-safe wrapper around any Map implementation.
// It uses a read-write mutex to allow concurrent reads while ensuring
// exclusive access for write operations.
//
// The implementation is designed to minimize lock contention:
//   - Read operations (Get, ContainsKey, Size, etc.) use read locks
//   - Write operations (Put, Remove, etc.) use write locks
//   - Iteration creates a snapshot to avoid holding locks during iteration
//
// User-supplied callbacks (ForEach, ReplaceAll, Compute, ComputeIfAbsent,
// ComputeIfPresent, Merge) are never invoked while the mutex is held, so a
// callback may call back into the same map, including through mutating
// methods, without self-deadlocking.
type SyncMap[K comparable, V any] struct {
	inner Map[K, V]    // The underlying Map implementation
	mutex sync.RWMutex // Read-write mutex for thread safety
}

// NewSyncMap creates a new thread-safe map wrapper.
// If a Map is provided, its entries are copied into the wrapper (via Clone,
// which preserves the source's iteration order for a LinkedMap); otherwise a
// new HashMap is used as the backing store.
// If the provided map is already a SyncMap or a StdSyncMap, it returns that
// same instance to avoid double-wrapping.
//
// Examples:
//
//	SyncMap := NewSyncMap[string, int]()                     // uses a new HashMap
//	SyncMap := NewSyncMap(NewLinkedMap[string, int]())       // copies a LinkedMap
//	SyncMap := NewSyncMap(existingSyncMap)                   // returns existingSyncMap
//
// The source map is never retained by the wrapper: the caller may keep using it
// freely, and changes made to it afterwards are not visible through the
// wrapper. Conversely, operations on the wrapper do not affect the source.
func NewSyncMap[K comparable, V any](maps ...Map[K, V]) Map[K, V] {
	var inner Map[K, V] = make(HashMap[K, V])

	for _, m := range maps {
		if m != nil {
			// Avoid double-wrapping if the provided map is already thread-safe
			if sm, ok := m.(*SyncMap[K, V]); ok {
				return sm
			}
			if stdSm, ok := m.(*StdSyncMap[K, V]); ok {
				return stdSm
			}
			inner = m.Clone()
			break
		}
	}

	return &SyncMap[K, V]{
		inner: inner,
	}
}

// Basic Operations - Write operations use exclusive locks

// Put safely associates a value with a key with exclusive access.
// Uses a write lock to ensure thread safety.
func (s *SyncMap[K, V]) Put(key K, value V) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Put(key, value)
}

// Remove safely removes a key-value pair with exclusive access.
// Uses a write lock to ensure thread safety.
func (s *SyncMap[K, V]) Remove(key K) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Remove(key)
}

// Basic Operations - Read operations use shared locks

// Get safely retrieves a value with shared access.
// Uses a read lock to allow concurrent reads while preventing
// concurrent writes.
func (s *SyncMap[K, V]) Get(key K) (V, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Get(key)
}

// ContainsKey safely checks key existence with shared access.
// Uses a read lock to allow concurrent reads.
func (s *SyncMap[K, V]) ContainsKey(key K) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsKey(key)
}

// ContainsValue safely checks value existence with shared access.
// Uses a read lock for the entire operation.
func (s *SyncMap[K, V]) ContainsValue(value V) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsValue(value)
}

// Query Operations - Read operations use shared locks

// Size safely returns the element count with shared access.
// Uses a read lock to ensure a consistent view.
func (s *SyncMap[K, V]) Size() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Size()
}

// IsEmpty safely checks if the map is empty with shared access.
// Uses a read lock to ensure a consistent view.
func (s *SyncMap[K, V]) IsEmpty() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.IsEmpty()
}

// Bulk Operations

// Clear safely removes all elements with exclusive access.
// Uses a write lock as this is a mutating operation.
func (s *SyncMap[K, V]) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.inner.Clear()
}

// PutAll safely copies all mappings from other into this map.
//
// The source is snapshotted before this map's write lock is taken: that is
// what removes the lock-order inversion between two SyncMaps copying into each
// other, and it also lets s.PutAll(s) complete. The snapshot is then applied
// under a single write lock, so from the point of view of other writers the
// copy lands as one batch. A nil other is a no-op.
//
// Entries written to other after PutAll was called are not part of the
// snapshot and are therefore not copied.
func (s *SyncMap[K, V]) PutAll(other Map[K, V]) {
	if other == nil {
		return
	}

	// Snapshot the source before locking this map: other may be this map or
	// another SyncMap whose lock must not be held while locked here.
	entries := other.Entries()

	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, entry := range entries {
		s.inner.Put(entry.Key(), entry.Value())
	}
}

// Keys safely returns all keys as a snapshot.
// Uses a read lock to ensure a consistent view.
// The returned slice is independent of the map.
func (s *SyncMap[K, V]) Keys() []K {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Keys()
}

// Values safely returns all values as a snapshot.
// Uses a read lock to ensure a consistent view.
// The returned slice is independent of the map.
func (s *SyncMap[K, V]) Values() []V {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Values()
}

// Entries safely returns all key-value pairs as a snapshot.
// Uses a read lock to ensure a consistent view.
// The returned slice is independent of the map.
func (s *SyncMap[K, V]) Entries() []*Entry[K, V] {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Entries()
}

// ForEach safely iterates over all key-value pairs.
//
// The entries are snapshotted under a read lock and the lock is released
// before any action is called, so action runs without any lock held: it may
// call back into this map, including through mutating methods such as Put or
// Remove, without deadlocking.
//
// The iteration therefore reflects the state of the map at the moment ForEach
// was called; writes made while it runs, by action itself or by other
// goroutines, are not visited by that call.
func (s *SyncMap[K, V]) ForEach(action func(K, V)) {
	s.mutex.RLock()
	entries := s.inner.Entries()
	s.mutex.RUnlock()

	for _, entry := range entries {
		action(entry.Key(), entry.Value())
	}
}

// Default Value Related Methods

// GetOrDefault safely gets a value or returns a default with shared access.
// Uses a read lock to ensure consistent reads.
func (s *SyncMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.GetOrDefault(key, defaultValue)
}

// PutIfAbsent safely puts a value only if key is absent with exclusive access.
// Uses a write lock as this may be a mutating operation.
func (s *SyncMap[K, V]) PutIfAbsent(key K, value V) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.PutIfAbsent(key, value)
}

// Conditional Operation Methods

// RemoveIf safely removes a key-value pair conditionally with exclusive access.
// Uses a write lock as this may be a mutating operation.
func (s *SyncMap[K, V]) RemoveIf(key K, value V) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.RemoveIf(key, value)
}

// Replace safely replaces an existing value with exclusive access.
// Uses a write lock as this may be a mutating operation.
func (s *SyncMap[K, V]) Replace(key K, value V) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Replace(key, value)
}

// ReplaceIf safely replaces a value conditionally with exclusive access.
// Uses a write lock as this may be a mutating operation.
func (s *SyncMap[K, V]) ReplaceIf(key K, oldValue, newValue V) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.ReplaceIf(key, oldValue, newValue)
}

// Functional Operation Methods

// The Compute/ComputeIfAbsent/ComputeIfPresent/Merge family all follow the same
// locking contract:
//
//   - The user-supplied function is always called with no lock held, so it may
//     call back into the map, including through mutating methods.
//   - The function is invoked against a value read under a read lock, and its
//     result is applied under a write lock only if the entry is still the one
//     the function was given. When the entry changed meanwhile the operation
//     retries, which means the function may be invoked more than once under
//     concurrent writes, and a function that writes the very key it is
//     computing for never converges: keep it a pure function of its arguments.
//
// Compute attempts to compute a mapping for the specified key and its current
// mapped value (or the zero value if there is no current mapping).
//
// The remappingFunction receives the key, the current value, and whether the
// key exists; it runs without any lock held and may call back into this map.
// See the family note above for the concurrency contract. The return values
// match HashMap.Compute: (zero, false) when the key ends up absent.
func (s *SyncMap[K, V]) Compute(key K, remappingFunction func(K, V, bool) (V, bool)) (V, bool) {
	for {
		s.mutex.RLock()
		oldValue, exists := s.inner.Get(key)
		s.mutex.RUnlock()

		newValue, shouldPut := remappingFunction(key, oldValue, exists)

		s.mutex.Lock()
		currentValue, currentExists := s.inner.Get(key)
		if currentExists != exists || (exists && !valuesEqual(currentValue, oldValue)) {
			// The entry changed while the function ran: retry against the
			// state the function will be given next instead of clobbering it.
			s.mutex.Unlock()
			continue
		}

		var result V
		var applied bool
		switch {
		case shouldPut:
			s.inner.Put(key, newValue)
			result, applied = newValue, true
		case exists:
			// The remapping returned false for an existing mapping: remove it.
			s.inner.Remove(key)
		}

		s.mutex.Unlock()
		return result, applied
	}
}

// ComputeIfAbsent computes a value for the specified key if the key is not
// already associated with a value, and associates it with the computed value.
// Returns the current (existing or computed) value associated with the key.
//
// The mappingFunction runs without any lock held and may call back into this
// map. If another writer associates the key first, the value it stored wins and
// the computed value is discarded.
func (s *SyncMap[K, V]) ComputeIfAbsent(key K, mappingFunction func(K) V) V {
	s.mutex.RLock()
	value, exists := s.inner.Get(key)
	s.mutex.RUnlock()
	if exists {
		return value
	}

	newValue := mappingFunction(key)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Re-check under the write lock: never overwrite a value that appeared
	// while the mapping function was running.
	if currentValue, currentExists := s.inner.Get(key); currentExists {
		return currentValue
	}
	s.inner.Put(key, newValue)
	return newValue
}

// ComputeIfPresent computes a new mapping for the specified key if the key is
// currently mapped to a value in this map. Returns the new value and true if
// the mapping was updated, otherwise returns zero value and false.
//
// The remappingFunction runs without any lock held and may call back into this
// map. See the family note above for the concurrency contract; the function is
// not called at all when the key is absent.
func (s *SyncMap[K, V]) ComputeIfPresent(key K, remappingFunction func(K, V) V) (V, bool) {
	for {
		s.mutex.RLock()
		oldValue, exists := s.inner.Get(key)
		s.mutex.RUnlock()
		if !exists {
			var zero V
			return zero, false
		}

		newValue := remappingFunction(key, oldValue)

		s.mutex.Lock()
		currentValue, stillExists := s.inner.Get(key)
		switch {
		case stillExists && valuesEqual(currentValue, oldValue):
			s.inner.Put(key, newValue)
			s.mutex.Unlock()
			return newValue, true
		case stillExists:
			// The entry changed while the function ran: retry.
			s.mutex.Unlock()
			continue
		default:
			// The entry disappeared: nothing left to compute.
			s.mutex.Unlock()
			var zero V
			return zero, false
		}
	}
}

// Merge associates the specified value with the specified key if the key is not
// already associated with a value. If the key is already associated with a
// value, replaces the associated value with the results of the given remapping
// function. Returns the new value associated with the key.
//
// The remappingFunction runs without any lock held and may call back into this
// map. See the family note above for the concurrency contract; the function is
// not called when the key is absent.
func (s *SyncMap[K, V]) Merge(key K, value V, remappingFunction func(V, V) V) V {
	for {
		s.mutex.RLock()
		oldValue, exists := s.inner.Get(key)
		s.mutex.RUnlock()

		if !exists {
			s.mutex.Lock()
			if _, appeared := s.inner.Get(key); !appeared {
				s.inner.Put(key, value)
				s.mutex.Unlock()
				return value
			}
			// Another writer inserted the key: retry so the remapping
			// function gets a chance to merge into that value.
			s.mutex.Unlock()
			continue
		}

		newValue := remappingFunction(oldValue, value)

		s.mutex.Lock()
		currentValue, stillExists := s.inner.Get(key)
		if stillExists && valuesEqual(currentValue, oldValue) {
			s.inner.Put(key, newValue)
			s.mutex.Unlock()
			return newValue
		}
		// The entry changed or disappeared while the function ran: retry.
		s.mutex.Unlock()
	}
}

// ReplaceAll replaces each entry's value with the result of invoking the given
// function on that entry's key and value.
//
// The function is evaluated for every entry of a snapshot taken under a read
// lock, with no lock held, so it may call back into this map. The computed
// values are then applied under a single write lock, and only for keys that
// still exist: an entry removed while the function ran is not resurrected.
// Values written concurrently by other goroutines are overwritten; the last
// writer wins.
func (s *SyncMap[K, V]) ReplaceAll(function func(K, V) V) {
	s.mutex.RLock()
	entries := s.inner.Entries()
	s.mutex.RUnlock()

	pending := make([]pendingEntry[K, V], 0, len(entries))
	for _, entry := range entries {
		key, value := entry.Key(), entry.Value()
		pending = append(pending, pendingEntry[K, V]{key: key, value: function(key, value)})
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, p := range pending {
		if s.inner.ContainsKey(p.key) {
			s.inner.Put(p.key, p.value)
		}
	}
}

// Iterator Methods - Use snapshots to avoid holding locks during iteration

// Iter returns a thread-safe iterator by creating a snapshot.
// This approach avoids holding locks during iteration, which could
// cause deadlocks or performance issues with long-running iterations.
//
// The snapshot is taken when the iteration starts (when the range loop begins
// running the returned iterator), not when Iter() is called, so changes made to
// the map during iteration are not reflected in that iteration. Writes from
// other goroutines and mutations performed by the loop body are both invisible
// to it, which is what makes iterating while mutating safe.
//
// Example:
//
//	for k, v := range SyncMap.Iter() {
//	    // This iteration is safe and won't block other operations
//	    fmt.Printf("%v: %v\n", k, v)
//	}
func (s *SyncMap[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		// Create a snapshot with minimal lock time
		s.mutex.RLock()
		entries := s.inner.Entries()
		s.mutex.RUnlock()

		// Iterate over the snapshot without holding any locks
		for _, entry := range entries {
			if !yield(entry.Key(), entry.Value()) {
				return
			}
		}
	}
}

// IterKeys returns a thread-safe key iterator using snapshots.
// See Iter() for details about snapshot-based iteration.
func (s *SyncMap[K, V]) IterKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		// Create a snapshot with minimal lock time
		s.mutex.RLock()
		keys := s.inner.Keys()
		s.mutex.RUnlock()

		// Iterate over the snapshot without holding any locks
		for _, key := range keys {
			if !yield(key) {
				return
			}
		}
	}
}

// IterValues returns a thread-safe value iterator using snapshots.
// See Iter() for details about snapshot-based iteration.
func (s *SyncMap[K, V]) IterValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		// Create a snapshot with minimal lock time
		s.mutex.RLock()
		values := s.inner.Values()
		s.mutex.RUnlock()

		// Iterate over the snapshot without holding any locks
		for _, value := range values {
			if !yield(value) {
				return
			}
		}
	}
}

// Clone safely creates an independent copy of the map.
// The clone is also a SyncMap wrapping a copy of the inner map.
// Uses a read lock to ensure a consistent snapshot for cloning.
func (s *SyncMap[K, V]) Clone() Map[K, V] {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	cloned := s.inner.Clone()
	return &SyncMap[K, V]{
		inner: cloned,
	}
}

// StdSyncMap is a thread-safe Map interface implementation based on Go's
// standard library sync.Map. It provides concurrent access safety for all
// operations, making it suitable for use in multi-goroutine environments. The
// underlying sync.Map is optimized for scenarios where entries are only ever
// written once but read many times, or when multiple goroutines read, write,
// and overwrite entries for disjoint sets of keys.
//
// V may be any type, including slices, maps and funcs, and K may hold the nil
// interface: the comparison-based operations (RemoveIf, Replace, ReplaceIf,
// Compute, ComputeIfPresent, Merge, ReplaceAll) compare values through
// [valuesEqual] and unwrap stored entries through [unwrap] instead of handing
// values to sync.Map's CompareAndSwap / CompareAndDelete, which panic on
// uncomparable dynamic types.
//
// Comparable values keep sync.Map's atomic compare-and-swap, which is atomic
// with respect to every other write to the map. Uncomparable values cannot use
// it and instead serialize with each other through mu: a concurrent
// [StdSyncMap.Put] or [StdSyncMap.Remove] of the same key may land between that
// comparison and the store and be overwritten by it.
type StdSyncMap[K comparable, V any] struct {
	m sync.Map

	// mu serializes the compare-based operations for uncomparable values, which
	// cannot use sync.Map's atomic primitives without risking a panic. Read
	// paths (Get, ContainsKey, Size, ...) stay lock-free, and user callbacks
	// are never invoked while mu is held.
	mu sync.Mutex
}

// compareAndSwap stores newValue under key if the key is present and still
// holds oldValue, reporting whether it did.
//
// It is the comparison-safe replacement for sync.Map.CompareAndSwap: comparable
// values (including a nil oldValue, which sync.Map distinguishes from an absent
// key on its own) keep the atomic primitive; uncomparable ones are compared
// with [valuesEqual] under mu, where the primitive would panic.
func (s *StdSyncMap[K, V]) compareAndSwap(key K, oldValue, newValue V) bool {
	if isComparableValue(oldValue) {
		return s.m.CompareAndSwap(key, oldValue, newValue)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.m.Load(key)
	if !exists || !valuesEqual(unwrap[V](current), oldValue) {
		return false
	}
	s.m.Store(key, newValue)
	return true
}

// compareAndDelete removes key if it is present and still holds oldValue,
// reporting whether it did.
//
// It is the comparison-safe replacement for sync.Map.CompareAndDelete, split
// the same way as [StdSyncMap.compareAndSwap].
func (s *StdSyncMap[K, V]) compareAndDelete(key K, oldValue V) bool {
	if isComparableValue(oldValue) {
		return s.m.CompareAndDelete(key, oldValue)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.m.Load(key)
	if !exists || !valuesEqual(unwrap[V](current), oldValue) {
		return false
	}
	s.m.Delete(key)
	return true
}

// NewStdSyncMap creates a new thread-safe StdSyncMap instance.
func NewStdSyncMap[K comparable, V any]() *StdSyncMap[K, V] {
	return &StdSyncMap[K, V]{}
}

// Put associates the specified value with the specified key in this map.
// If the map previously contained a mapping for the key, the old value is replaced.
// This operation is thread-safe and can be called concurrently from multiple goroutines.
func (s *StdSyncMap[K, V]) Put(key K, value V) (V, bool) {
	if oldValue, exists := s.m.Swap(key, value); exists {
		return unwrap[V](oldValue), true
	}
	var zero V
	return zero, false
}

// Get returns the value to which the specified key is mapped.
// This operation is thread-safe and optimized for concurrent reads.
func (s *StdSyncMap[K, V]) Get(key K) (V, bool) {
	if value, exists := s.m.Load(key); exists {
		return unwrap[V](value), true
	}
	var zero V
	return zero, false
}

// Remove removes the mapping for a key from this map if it is present.
// This operation is thread-safe and can be called concurrently.
func (s *StdSyncMap[K, V]) Remove(key K) (V, bool) {
	if value, exists := s.m.LoadAndDelete(key); exists {
		return unwrap[V](value), true
	}
	var zero V
	return zero, false
}

// ContainsKey returns true if this map contains a mapping for the specified key.
// This operation is thread-safe.
func (s *StdSyncMap[K, V]) ContainsKey(key K) bool {
	_, exists := s.m.Load(key)
	return exists
}

// ContainsValue returns true if this map maps one or more keys to the specified value.
// This operation requires scanning all entries and uses reflection for deep equality comparison.
// Note: This operation is expensive and may impact performance in concurrent scenarios.
func (s *StdSyncMap[K, V]) ContainsValue(value V) bool {
	found := false
	s.m.Range(func(key, val any) bool {
		if valuesEqual(unwrap[V](val), value) {
			found = true
			return false // Stop iteration
		}
		return true // Continue iteration
	})
	return found
}

// Size returns the number of key-value mappings in this map.
// Note: This operation requires scanning all entries, which may be expensive.
func (s *StdSyncMap[K, V]) Size() int {
	count := 0
	s.m.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// IsEmpty returns true if this map contains no key-value mappings.
// This operation checks for the existence of at least one entry.
func (s *StdSyncMap[K, V]) IsEmpty() bool {
	isEmpty := true
	s.m.Range(func(key, value any) bool {
		isEmpty = false
		return false // Stop iteration after finding first entry
	})
	return isEmpty
}

// Clear removes all of the mappings from this map.
// This operation creates a new sync.Map instance to ensure thread safety.
func (s *StdSyncMap[K, V]) Clear() {
	s.m.Clear()
}

// PutAll copies all of the mappings from the specified map to this map.
// This operation is thread-safe but may not be atomic across all entries.
func (s *StdSyncMap[K, V]) PutAll(other Map[K, V]) {
	other.ForEach(func(k K, v V) {
		s.m.Store(k, v)
	})
}

// Keys returns a slice containing all the keys in this map.
// The returned slice is a snapshot of the current keys at the time of the call.
func (s *StdSyncMap[K, V]) Keys() []K {
	var keys []K
	s.m.Range(func(key, value any) bool {
		keys = append(keys, unwrap[K](key))
		return true
	})
	return keys
}

// Values returns a slice containing all the values in this map.
// The returned slice is a snapshot of the current values at the time of the call.
func (s *StdSyncMap[K, V]) Values() []V {
	var values []V
	s.m.Range(func(key, value any) bool {
		values = append(values, unwrap[V](value))
		return true
	})
	return values
}

// Entries returns a slice containing all the key-value pairs in this map.
// Each entry is represented as a pointer to an Entry struct.
// The returned slice is a snapshot of the current entries at the time of the call.
func (s *StdSyncMap[K, V]) Entries() []*Entry[K, V] {
	var entries []*Entry[K, V]
	s.m.Range(func(key, value any) bool {
		entries = append(entries, &Entry[K, V]{
			key:   unwrap[K](key),
			value: unwrap[V](value),
		})
		return true
	})
	return entries
}

// ForEach performs the given action for each key-value pair in this map.
// The action function is called once for each mapping in the map.
// Note: The iteration order is not guaranteed and may vary between calls.
func (s *StdSyncMap[K, V]) ForEach(action func(K, V)) {
	s.m.Range(func(key, value any) bool {
		action(unwrap[K](key), unwrap[V](value))
		return true
	})
}

// GetOrDefault returns the value to which the specified key is mapped,
// or defaultValue if this map contains no mapping for the key.
func (s *StdSyncMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	if value, exists := s.m.Load(key); exists {
		return unwrap[V](value)
	}
	return defaultValue
}

// PutIfAbsent associates the specified value with the specified key only if
// the key is not already associated with a value.
// This operation is atomic and thread-safe.
func (s *StdSyncMap[K, V]) PutIfAbsent(key K, value V) (V, bool) {
	actual, loaded := s.m.LoadOrStore(key, value)
	if loaded {
		return unwrap[V](actual), false // Key already existed
	}
	return value, true // Key was absent, value was stored
}

// RemoveIf removes the entry for the specified key only if it is currently
// mapped to the specified value, compared with [valuesEqual] so that value
// types that do not support == are usable.
func (s *StdSyncMap[K, V]) RemoveIf(key K, value V) bool {
	return s.compareAndDelete(key, value)
}

// Replace replaces the entry for the specified key only if it is currently mapped to some value.
// This operation uses compare-and-swap semantics for thread safety.
func (s *StdSyncMap[K, V]) Replace(key K, value V) (V, bool) {
	if oldValue, exists := s.m.Load(key); exists {
		current := unwrap[V](oldValue)
		if s.compareAndSwap(key, current, value) {
			return current, true
		}
		// If the compare-and-swap failed, the value was changed by another
		// goroutine. Try to get the current value.
		if currentValue, stillExists := s.m.Load(key); stillExists {
			return unwrap[V](currentValue), false
		}
	}
	var zero V
	return zero, false
}

// ReplaceIf replaces the entry for the specified key only if currently mapped
// to the specified value, compared with [valuesEqual] so that value types that
// do not support == are usable.
// This operation uses compare-and-swap semantics for thread safety.
func (s *StdSyncMap[K, V]) ReplaceIf(key K, oldValue, newValue V) bool {
	return s.compareAndSwap(key, oldValue, newValue)
}

// Compute attempts to compute a mapping for the specified key and its current mapped value.
// This operation is not atomic across the entire computation but provides consistency guarantees.
// The remappingFunc receives the key, current value, and existence flag.
func (s *StdSyncMap[K, V]) Compute(key K, remappingFunc func(K, V, bool) (V, bool)) (V, bool) {
	for {
		oldValue, exists := s.m.Load(key)
		var currentValue V
		if exists {
			currentValue = unwrap[V](oldValue)
		}

		newValue, shouldPut := remappingFunc(key, currentValue, exists)

		if shouldPut {
			if exists {
				// Try to replace the existing value
				if s.compareAndSwap(key, currentValue, newValue) {
					return newValue, true
				}
				// Value was changed by another goroutine, retry
				continue
			} else {
				// Try to store the new value
				if _, loaded := s.m.LoadOrStore(key, newValue); !loaded {
					return newValue, true
				}
				// Another goroutine stored a value, retry
				continue
			}
		} else {
			if exists {
				// Try to delete the existing value
				if s.compareAndDelete(key, currentValue) {
					var zero V
					return zero, false
				}
				// Value was changed by another goroutine, retry
				continue
			}
			// Key doesn't exist — nothing to put, return zero value.
			var zero V
			return zero, false
		}
	}
}

// ComputeIfAbsent computes a value for the specified key if the key is not already
// associated with a value, and associates it with the computed value.
// This operation is atomic and thread-safe.
func (s *StdSyncMap[K, V]) ComputeIfAbsent(key K, mappingFunction func(K) V) V {
	if value, exists := s.m.Load(key); exists {
		return unwrap[V](value)
	}

	newValue := mappingFunction(key)
	actual, _ := s.m.LoadOrStore(key, newValue)
	return unwrap[V](actual)
}

// ComputeIfPresent computes a new mapping for the specified key if the key is
// currently mapped to a value in this map.
// This operation uses compare-and-swap for thread safety.
func (s *StdSyncMap[K, V]) ComputeIfPresent(key K, remappingFunc func(K, V) V) (V, bool) {
	for {
		if oldValue, exists := s.m.Load(key); exists {
			current := unwrap[V](oldValue)
			newValue := remappingFunc(key, current)
			if s.compareAndSwap(key, current, newValue) {
				return newValue, true
			}
			// Value was changed by another goroutine, retry
			continue
		}
		// Key doesn't exist
		var zero V
		return zero, false
	}
}

// Merge associates the specified value with the specified key if the key is not
// already associated with a value, or merges the existing value with the new value
// using the provided remapping function.
// This operation handles concurrency using compare-and-swap semantics.
func (s *StdSyncMap[K, V]) Merge(key K, value V, remappingFunc func(V, V) V) V {
	for {
		if oldValue, exists := s.m.Load(key); exists {
			current := unwrap[V](oldValue)
			newValue := remappingFunc(current, value)
			if s.compareAndSwap(key, current, newValue) {
				return newValue
			}
			// Value was changed by another goroutine, retry
			continue
		} else {
			// Key doesn't exist, try to store the new value
			if _, loaded := s.m.LoadOrStore(key, value); !loaded {
				return value
			}
			// Another goroutine stored a value, retry with merge
			continue
		}
	}
}

// ReplaceAll replaces each entry's value with the result of invoking the given
// function on that entry's key and value.
//
// Every entry reported by a single snapshot of the map is visited. Entries
// removed before they are reached are skipped instead of being written back,
// and an entry whose value another goroutine changes while the function runs is
// re-read, so the function is applied again to the new value.
func (s *StdSyncMap[K, V]) ReplaceAll(function func(K, V) V) {
	for _, entry := range s.Entries() {
		key := entry.Key()
		for {
			current, exists := s.m.Load(key)
			if !exists {
				// The entry was deleted before it was reached: skip it.
				break
			}
			oldValue := unwrap[V](current)
			newValue := function(key, oldValue)
			if s.compareAndSwap(key, oldValue, newValue) {
				break
			}
			// The value changed while the function ran: retry against it.
		}
	}
}

// Iter returns an iterator that yields key-value pairs.
// Note: StdSyncMap does not guarantee any specific iteration order.
// The iteration represents a snapshot at the time Iter() is called,
// but the underlying map may be modified during iteration by other goroutines.
func (s *StdSyncMap[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		s.m.Range(func(key, value any) bool {
			return yield(unwrap[K](key), unwrap[V](value))
		})
	}
}

// IterKeys returns an iterator that yields keys only.
// Note: StdSyncMap does not guarantee any specific iteration order.
// The iteration represents a snapshot at the time IterKeys() is called.
func (s *StdSyncMap[K, V]) IterKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		s.m.Range(func(key, _ any) bool {
			return yield(unwrap[K](key))
		})
	}
}

// IterValues returns an iterator that yields values only.
// Note: StdSyncMap does not guarantee any specific iteration order.
// The iteration represents a snapshot at the time IterValues() is called.
func (s *StdSyncMap[K, V]) IterValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		s.m.Range(func(_, value any) bool {
			return yield(unwrap[V](value))
		})
	}
}

// Clone creates an independent copy of the StdSyncMap.
// The cloned map contains the same key-value pairs but is a separate instance.
// This operation is not atomic - the clone represents a snapshot of the map
// at the time Clone() is called, but concurrent modifications may result in
// an inconsistent snapshot.
func (s *StdSyncMap[K, V]) Clone() Map[K, V] {
	cloned := NewStdSyncMap[K, V]()
	cloned.PutAll(s)
	return cloned
}
