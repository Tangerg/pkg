package maps

import (
	"iter"
	"reflect"
	"sync"
)

// valuesEqual reports whether a and b hold equal values.
//
// Comparison is by == when the dynamic type supports it and by
// [reflect.DeepEqual] otherwise. The split keeps [Map] usable with an
// unconstrained V: comparing two interface values that hold the same
// uncomparable type with == panics, which is exactly what sync.Map's
// CompareAndSwap and CompareAndDelete do internally.
func valuesEqual[V any](a, b V) bool {
	if isComparableValue(a) && isComparableValue(b) {
		return any(a) == any(b)
	}
	return reflect.DeepEqual(a, b)
}

// isComparableValue reports whether the dynamic type stored in v supports ==,
// i.e. whether two interface values holding it can be compared without
// panicking. A nil interface is trivially comparable.
func isComparableValue(v any) bool {
	return v == nil || reflect.TypeOf(v).Comparable()
}

// unwrap returns the value a sync.Map holds for a key.
//
// A plain v.(T) panics when T is an interface type and the stored value is a
// nil interface, which has no dynamic type to assert; the comma-ok form yields
// the zero value instead, which is exactly what was stored. It cannot mask a
// type mismatch because every entry was stored by this type as a K or a V.
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

// SyncMap serializes access to any [Map] with a read-write mutex, so a backing
// map that is not itself safe (HashMap, LinkedMap) can be shared across
// goroutines.
//
// User-supplied callbacks (ForEach, ReplaceAll, Compute, ComputeIfAbsent,
// ComputeIfPresent, Merge) are never invoked while the mutex is held, so a
// callback may call back into the same map, including through mutating
// methods, without self-deadlocking.
type SyncMap[K comparable, V any] struct {
	inner Map[K, V]
	mutex sync.RWMutex
}

// NewSyncMap returns a thread-safe wrapper around the entries of the first
// non-nil argument (copied via Clone, so a LinkedMap keeps its order); with no
// argument it uses a new HashMap. An argument that is already a [SyncMap] or
// [StdSyncMap] is returned unchanged rather than wrapped again.
//
// The source map is never retained: later changes to either are not visible
// through the other.
func NewSyncMap[K comparable, V any](maps ...Map[K, V]) Map[K, V] {
	var inner Map[K, V] = make(HashMap[K, V])

	for _, m := range maps {
		if m != nil {
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

// Put associates value with key, returning the previous value and whether the
// key was already present.
func (s *SyncMap[K, V]) Put(key K, value V) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Put(key, value)
}

// Remove deletes key, returning its value and whether it was present.
func (s *SyncMap[K, V]) Remove(key K) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Remove(key)
}

// Get returns the value for key and whether it is present.
func (s *SyncMap[K, V]) Get(key K) (V, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Get(key)
}

// ContainsKey reports whether key is present.
func (s *SyncMap[K, V]) ContainsKey(key K) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsKey(key)
}

// ContainsValue reports whether any key maps to value.
func (s *SyncMap[K, V]) ContainsValue(value V) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.ContainsValue(value)
}

// Size returns the number of entries.
func (s *SyncMap[K, V]) Size() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Size()
}

// IsEmpty reports whether the map has no entries.
func (s *SyncMap[K, V]) IsEmpty() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.IsEmpty()
}

// Clear removes every entry.
func (s *SyncMap[K, V]) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.inner.Clear()
}

// PutAll copies all mappings from other into this map.
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

// Keys returns all keys as a snapshot; the slice does not alias the map.
func (s *SyncMap[K, V]) Keys() []K {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Keys()
}

// Values returns all values as a snapshot; the slice does not alias the map.
func (s *SyncMap[K, V]) Values() []V {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Values()
}

// Entries returns all key-value pairs as a snapshot; the slice does not alias
// the map.
func (s *SyncMap[K, V]) Entries() []*Entry[K, V] {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.Entries()
}

// ForEach calls action once per entry of a snapshot taken when it is called.
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

// GetOrDefault returns the value for key, or defaultValue when key is absent.
func (s *SyncMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.inner.GetOrDefault(key, defaultValue)
}

// PutIfAbsent associates value with key only when key is absent, returning the
// value now mapped to key and whether a mapping was stored.
func (s *SyncMap[K, V]) PutIfAbsent(key K, value V) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.PutIfAbsent(key, value)
}

// RemoveIf deletes key only when it currently maps to value.
func (s *SyncMap[K, V]) RemoveIf(key K, value V) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.RemoveIf(key, value)
}

// Replace replaces the value for key only when key is present, returning the
// previous value and whether the replacement happened.
func (s *SyncMap[K, V]) Replace(key K, value V) (V, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.Replace(key, value)
}

// ReplaceIf replaces the value for key only when it currently equals oldValue.
func (s *SyncMap[K, V]) ReplaceIf(key K, oldValue, newValue V) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.inner.ReplaceIf(key, oldValue, newValue)
}

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
// Compute derives a new mapping for key from its current value and existence.
//
// remappingFunction runs with no lock held and may call back into this map; see
// the family note above for the concurrency contract. The return values match
// [HashMap.Compute]: (zero, false) when the key ends up absent.
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

// ComputeIfAbsent returns the value for key, computing and storing it when key
// is absent.
//
// mappingFunction runs with no lock held and may call back into this map. If
// another writer associates the key first, its value wins.
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

// ComputeIfPresent derives a new value for key only when key is present,
// returning the new value and whether the mapping was updated.
//
// remappingFunction runs with no lock held and may call back into this map; see
// the family note above. It is not called when the key is absent.
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

// Merge stores value under key when key is absent; otherwise it stores
// remappingFunc(current, value). It returns the value left under key.
//
// remappingFunction runs with no lock held and may call back into this map; see
// the family note above. It is not called when the key is absent.
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

// Iter yields each key-value pair from a snapshot taken when iteration starts.
//
// The snapshot is taken when the range loop begins running the returned
// iterator, not when Iter is called, so writes made during iteration — by the
// loop body or by other goroutines — are not observed by that iteration. That
// is what makes iterating while mutating safe.
//
// Example:
//
//	for k, v := range SyncMap.Iter() {
//	    fmt.Printf("%v: %v\n", k, v)
//	}
func (s *SyncMap[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		s.mutex.RLock()
		entries := s.inner.Entries()
		s.mutex.RUnlock()

		for _, entry := range entries {
			if !yield(entry.Key(), entry.Value()) {
				return
			}
		}
	}
}

// IterKeys yields each key from a snapshot; see [SyncMap.Iter] for the snapshot
// semantics.
func (s *SyncMap[K, V]) IterKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		s.mutex.RLock()
		keys := s.inner.Keys()
		s.mutex.RUnlock()

		for _, key := range keys {
			if !yield(key) {
				return
			}
		}
	}
}

// IterValues yields each value from a snapshot; see [SyncMap.Iter] for the
// snapshot semantics.
func (s *SyncMap[K, V]) IterValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		s.mutex.RLock()
		values := s.inner.Values()
		s.mutex.RUnlock()

		for _, value := range values {
			if !yield(value) {
				return
			}
		}
	}
}

// Clone returns an independent SyncMap holding a copy of the entries.
func (s *SyncMap[K, V]) Clone() Map[K, V] {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	cloned := s.inner.Clone()
	return &SyncMap[K, V]{
		inner: cloned,
	}
}

// StdSyncMap is a [Map] backed by [sync.Map], for workloads where many
// goroutines read and write mostly disjoint keys. It makes no ordering
// guarantee.
//
// V may hold any type, including slices, maps and funcs, and K may hold a nil
// interface: comparison-based operations (RemoveIf, Replace, ReplaceIf,
// Compute, ComputeIfPresent, Merge, ReplaceAll) compare through [valuesEqual]
// and unwrap through [unwrap] rather than handing values to sync.Map's
// CompareAndSwap / CompareAndDelete, which panic on uncomparable dynamic types.
//
// A comparable old value keeps sync.Map's atomic compare-and-swap. An
// uncomparable one cannot use it and instead serializes with other
// compare-based operations through mu; a concurrent Put or Remove of the same
// key can slip between that comparison and the store and be overwritten.
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

// NewStdSyncMap returns an empty [StdSyncMap].
func NewStdSyncMap[K comparable, V any]() *StdSyncMap[K, V] {
	return &StdSyncMap[K, V]{}
}

// Put associates value with key, returning the previous value and whether the
// key was already present.
func (s *StdSyncMap[K, V]) Put(key K, value V) (V, bool) {
	if oldValue, exists := s.m.Swap(key, value); exists {
		return unwrap[V](oldValue), true
	}
	var zero V
	return zero, false
}

// Get returns the value for key and whether it is present.
func (s *StdSyncMap[K, V]) Get(key K) (V, bool) {
	if value, exists := s.m.Load(key); exists {
		return unwrap[V](value), true
	}
	var zero V
	return zero, false
}

// Remove deletes key, returning its value and whether it was present.
func (s *StdSyncMap[K, V]) Remove(key K) (V, bool) {
	if value, exists := s.m.LoadAndDelete(key); exists {
		return unwrap[V](value), true
	}
	var zero V
	return zero, false
}

// ContainsKey reports whether key is present.
func (s *StdSyncMap[K, V]) ContainsKey(key K) bool {
	_, exists := s.m.Load(key)
	return exists
}

// ContainsValue reports whether any key maps to value.
func (s *StdSyncMap[K, V]) ContainsValue(value V) bool {
	found := false
	s.m.Range(func(key, val any) bool {
		if valuesEqual(unwrap[V](val), value) {
			found = true
			return false
		}
		return true
	})
	return found
}

// Size returns the number of entries. It scans the whole map, so it is O(n)
// even for a single call.
func (s *StdSyncMap[K, V]) Size() int {
	count := 0
	s.m.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// IsEmpty reports whether the map has no entries.
func (s *StdSyncMap[K, V]) IsEmpty() bool {
	isEmpty := true
	s.m.Range(func(key, value any) bool {
		isEmpty = false
		return false
	})
	return isEmpty
}

// Clear removes every entry.
func (s *StdSyncMap[K, V]) Clear() {
	s.m.Clear()
}

// PutAll copies every mapping from other into this map. It is not atomic
// across entries.
func (s *StdSyncMap[K, V]) PutAll(other Map[K, V]) {
	other.ForEach(func(k K, v V) {
		s.m.Store(k, v)
	})
}

// Keys returns all keys as a snapshot; the slice does not alias the map.
func (s *StdSyncMap[K, V]) Keys() []K {
	var keys []K
	s.m.Range(func(key, value any) bool {
		keys = append(keys, unwrap[K](key))
		return true
	})
	return keys
}

// Values returns all values as a snapshot; the slice does not alias the map.
func (s *StdSyncMap[K, V]) Values() []V {
	var values []V
	s.m.Range(func(key, value any) bool {
		values = append(values, unwrap[V](value))
		return true
	})
	return values
}

// Entries returns all entries as a snapshot; the slice does not alias the map.
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

// ForEach calls action once per entry in an unspecified order.
func (s *StdSyncMap[K, V]) ForEach(action func(K, V)) {
	s.m.Range(func(key, value any) bool {
		action(unwrap[K](key), unwrap[V](value))
		return true
	})
}

// GetOrDefault returns the value for key, or defaultValue when key is absent.
func (s *StdSyncMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	if value, exists := s.m.Load(key); exists {
		return unwrap[V](value)
	}
	return defaultValue
}

// PutIfAbsent associates value with key only when key is absent, returning the
// value now mapped to key and whether a mapping was stored.
func (s *StdSyncMap[K, V]) PutIfAbsent(key K, value V) (V, bool) {
	actual, loaded := s.m.LoadOrStore(key, value)
	if loaded {
		return unwrap[V](actual), false
	}
	return value, true
}

// RemoveIf deletes key only when it currently equals value, compared with
// [valuesEqual] so uncomparable value types are usable.
func (s *StdSyncMap[K, V]) RemoveIf(key K, value V) bool {
	return s.compareAndDelete(key, value)
}

// Replace replaces the value for key only when key is present, returning the
// previous value and whether the replacement happened.
func (s *StdSyncMap[K, V]) Replace(key K, value V) (V, bool) {
	if oldValue, exists := s.m.Load(key); exists {
		current := unwrap[V](oldValue)
		if s.compareAndSwap(key, current, value) {
			return current, true
		}
		// Lost the race: report the value that is now current.
		if currentValue, stillExists := s.m.Load(key); stillExists {
			return unwrap[V](currentValue), false
		}
	}
	var zero V
	return zero, false
}

// ReplaceIf replaces the value for key only when it currently equals oldValue,
// compared with [valuesEqual] so uncomparable value types are usable.
func (s *StdSyncMap[K, V]) ReplaceIf(key K, oldValue, newValue V) bool {
	return s.compareAndSwap(key, oldValue, newValue)
}

// Compute derives a new mapping for key from its current value and whether the
// key exists.
//
// remappingFunc runs with no lock held and, like the rest of the Compute
// family, may run more than once under contention. It returns the value to
// store and whether to store it; returning (zero, false) for an existing key
// removes it. The result matches [HashMap.Compute].
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
				if s.compareAndSwap(key, currentValue, newValue) {
					return newValue, true
				}
				continue // lost the race; recompute against the new value
			}
			if _, loaded := s.m.LoadOrStore(key, newValue); !loaded {
				return newValue, true
			}
			continue // another goroutine stored the key first
		}
		if exists {
			if s.compareAndDelete(key, currentValue) {
				var zero V
				return zero, false
			}
			continue // lost the race; recompute against the new value
		}
		var zero V
		return zero, false
	}
}

// ComputeIfAbsent returns the value for key, computing and storing it with
// mappingFunction when key is absent.
//
// mappingFunction runs with no lock held; if it races with another writer, that
// writer's value wins and the computed value is discarded.
func (s *StdSyncMap[K, V]) ComputeIfAbsent(key K, mappingFunction func(K) V) V {
	if value, exists := s.m.Load(key); exists {
		return unwrap[V](value)
	}

	newValue := mappingFunction(key)
	actual, _ := s.m.LoadOrStore(key, newValue)
	return unwrap[V](actual)
}

// ComputeIfPresent derives a new value for key only when key is present,
// returning the new value and whether the mapping was updated.
//
// remappingFunc runs with no lock held and may run more than once under
// contention; it is not called when key is absent.
func (s *StdSyncMap[K, V]) ComputeIfPresent(key K, remappingFunc func(K, V) V) (V, bool) {
	for {
		if oldValue, exists := s.m.Load(key); exists {
			current := unwrap[V](oldValue)
			newValue := remappingFunc(key, current)
			if s.compareAndSwap(key, current, newValue) {
				return newValue, true
			}
			continue // lost the race; recompute against the new value
		}
		var zero V
		return zero, false
	}
}

// Merge stores value under key when key is absent; otherwise it stores
// remappingFunc(current, value). It returns the value left under key.
//
// remappingFunc runs with no lock held and may run more than once under
// contention; it is not called when key is absent.
func (s *StdSyncMap[K, V]) Merge(key K, value V, remappingFunc func(V, V) V) V {
	for {
		if oldValue, exists := s.m.Load(key); exists {
			current := unwrap[V](oldValue)
			newValue := remappingFunc(current, value)
			if s.compareAndSwap(key, current, newValue) {
				return newValue
			}
			continue // lost the race; merge against the new value
		}
		if _, loaded := s.m.LoadOrStore(key, value); !loaded {
			return value
		}
		continue // another goroutine stored the key first
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

// Iter yields each key-value pair in an unspecified order.
//
// Unlike [SyncMap.Iter] it does not snapshot: it walks the live sync.Map, so a
// concurrent write may or may not be observed.
func (s *StdSyncMap[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		s.m.Range(func(key, value any) bool {
			return yield(unwrap[K](key), unwrap[V](value))
		})
	}
}

// IterKeys yields each key in an unspecified order; see [StdSyncMap.Iter].
func (s *StdSyncMap[K, V]) IterKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		s.m.Range(func(key, _ any) bool {
			return yield(unwrap[K](key))
		})
	}
}

// IterValues yields each value in an unspecified order; see [StdSyncMap.Iter].
func (s *StdSyncMap[K, V]) IterValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		s.m.Range(func(_, value any) bool {
			return yield(unwrap[V](value))
		})
	}
}

// Clone returns an independent StdSyncMap holding a copy of the entries. The
// copy is not an atomic snapshot: a concurrent write may make it inconsistent.
func (s *StdSyncMap[K, V]) Clone() Map[K, V] {
	cloned := NewStdSyncMap[K, V]()
	cloned.PutAll(s)
	return cloned
}
