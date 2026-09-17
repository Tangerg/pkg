package maps

import (
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// SyncMap Constructor Tests
// =============================================================================

func TestSyncMap_NewSyncMap(t *testing.T) {
	t.Run("create with default HashMap", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		if m == nil {
			t.Fatal("NewSyncMap returned nil")
		}
		syncMap, ok := m.(*SyncMap[string, int])
		if !ok {
			t.Fatal("NewSyncMap did not return *SyncMap")
		}
		if syncMap.inner == nil {
			t.Error("inner map is nil")
		}
	})

	t.Run("wrap existing HashMap", func(t *testing.T) {
		hm := NewHashMap[string, int]()
		hm.Put("test", 123)

		m := NewSyncMap(hm)
		syncMap := m.(*SyncMap[string, int])

		if val, ok := syncMap.Get("test"); !ok || val != 123 {
			t.Errorf("Get() = (%v, %v), want (123, true)", val, ok)
		}
	})

	t.Run("wrap existing LinkedMap preserves order", func(t *testing.T) {
		lm := NewLinkedMap[string, int]()
		lm.Put("a", 1)
		lm.Put("b", 2)
		lm.Put("c", 3)

		m := NewSyncMap(lm)
		keys := m.Keys()

		expected := []string{"a", "b", "c"}
		for i, key := range keys {
			if key != expected[i] {
				t.Errorf("Keys()[%d] = %v, want %v", i, key, expected[i])
			}
		}
	})

	t.Run("avoid double wrapping SyncMap", func(t *testing.T) {
		m1 := NewSyncMap[string, int]()
		m2 := NewSyncMap(m1)

		if m1 != m2 {
			t.Error("NewSyncMap should return same instance when wrapping SyncMap")
		}
	})

	t.Run("avoid double wrapping StdSyncMap", func(t *testing.T) {
		m1 := NewStdSyncMap[string, int]()
		m2 := NewSyncMap[string, int](m1)

		if m1 != m2 {
			t.Error("NewSyncMap should return same instance when wrapping StdSyncMap")
		}
	})

	t.Run("ignore nil maps", func(t *testing.T) {
		m := NewSyncMap[string, int](nil)
		if m == nil {
			t.Fatal("NewSyncMap returned nil")
		}
		if !m.IsEmpty() {
			t.Error("map should be empty")
		}
	})
}

// =============================================================================
// SyncMap Basic Thread Safety Tests
// =============================================================================

func TestSyncMap_ConcurrentPut(t *testing.T) {
	m := NewSyncMap[int, int]()
	const numGoroutines = 100
	const numOpsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				key := id*numOpsPerGoroutine + j
				m.Put(key, key*10)
			}
		}(i)
	}

	wg.Wait()

	expectedSize := numGoroutines * numOpsPerGoroutine
	if m.Size() != expectedSize {
		t.Errorf("Size() = %v, want %v", m.Size(), expectedSize)
	}
}

func TestSyncMap_ConcurrentGet(t *testing.T) {
	m := NewSyncMap[int, int]()

	// Populate map
	for i := 0; i < 1000; i++ {
		m.Put(i, i*10)
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				val, ok := m.Get(j)
				if !ok {
					errors <- fmt.Errorf("key %d not found", j)
					return
				}
				if val != j*10 {
					errors <- fmt.Errorf("Get(%d) = %v, want %v", j, val, j*10)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestSyncMap_ConcurrentRemove(t *testing.T) {
	m := NewSyncMap[int, int]()

	// Populate map
	const numKeys = 1000
	for i := 0; i < numKeys; i++ {
		m.Put(i, i)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	var removedCount int32

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := id; j < numKeys; j += numGoroutines {
				if _, removed := m.Remove(j); removed {
					atomic.AddInt32(&removedCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	if m.Size() != 0 {
		t.Errorf("Size() = %v, want 0", m.Size())
	}
	if int(removedCount) != numKeys {
		t.Errorf("removed count = %v, want %v", removedCount, numKeys)
	}
}

func TestSyncMap_ConcurrentReadWrite(t *testing.T) {
	m := NewSyncMap[int, int]()

	// Populate initial data
	for i := 0; i < 100; i++ {
		m.Put(i, i)
	}

	const duration = 100 * time.Millisecond
	done := make(chan bool)
	errors := make(chan error, 10)

	// Writers
	for i := 0; i < 5; i++ {
		go func(id int) {
			timer := time.After(duration)
			for {
				select {
				case <-timer:
					done <- true
					return
				default:
					key := rand.Intn(100)
					m.Put(key, id*1000+key)
				}
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		go func() {
			timer := time.After(duration)
			for {
				select {
				case <-timer:
					done <- true
					return
				default:
					key := rand.Intn(100)
					if _, ok := m.Get(key); !ok {
						errors <- fmt.Errorf("key %d should exist", key)
						return
					}
				}
			}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// =============================================================================
// SyncMap Operation Tests
// =============================================================================

func TestSyncMap_BasicOperations(t *testing.T) {
	m := NewSyncMap[string, int]()

	t.Run("Put and Get", func(t *testing.T) {
		m.Put("key1", 100)
		if val, ok := m.Get("key1"); !ok || val != 100 {
			t.Errorf("Get() = (%v, %v), want (100, true)", val, ok)
		}
	})

	t.Run("ContainsKey", func(t *testing.T) {
		if !m.ContainsKey("key1") {
			t.Error("ContainsKey() = false, want true")
		}
		if m.ContainsKey("missing") {
			t.Error("ContainsKey() = true, want false")
		}
	})

	t.Run("Size and IsEmpty", func(t *testing.T) {
		if m.Size() != 1 {
			t.Errorf("Size() = %v, want 1", m.Size())
		}
		if m.IsEmpty() {
			t.Error("IsEmpty() = true, want false")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		val, removed := m.Remove("key1")
		if !removed || val != 100 {
			t.Errorf("Remove() = (%v, %v), want (100, true)", val, removed)
		}
		if !m.IsEmpty() {
			t.Error("map should be empty after removing last element")
		}
	})
}

func TestSyncMap_BulkOperations(t *testing.T) {
	t.Run("PutAll", func(t *testing.T) {
		m1 := NewSyncMap[string, int]()
		m1.Put("a", 1)
		m1.Put("b", 2)

		m2 := NewSyncMap[string, int]()
		m2.PutAll(m1)

		if m2.Size() != 2 {
			t.Errorf("Size() = %v, want 2", m2.Size())
		}
		if val, _ := m2.Get("a"); val != 1 {
			t.Errorf("Get(a) = %v, want 1", val)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)
		m.Clear()

		if !m.IsEmpty() {
			t.Error("map should be empty after Clear()")
		}
	})

	t.Run("Keys, Values, Entries", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)

		keys := m.Keys()
		if len(keys) != 2 {
			t.Errorf("len(Keys()) = %v, want 2", len(keys))
		}

		values := m.Values()
		if len(values) != 2 {
			t.Errorf("len(Values()) = %v, want 2", len(values))
		}

		entries := m.Entries()
		if len(entries) != 2 {
			t.Errorf("len(Entries()) = %v, want 2", len(entries))
		}
	})
}

func TestSyncMap_ConditionalOperations(t *testing.T) {
	t.Run("PutIfAbsent", func(t *testing.T) {
		m := NewSyncMap[string, int]()

		val, inserted := m.PutIfAbsent("key", 100)
		if !inserted || val != 100 {
			t.Errorf("PutIfAbsent() = (%v, %v), want (100, true)", val, inserted)
		}

		val, inserted = m.PutIfAbsent("key", 200)
		if inserted || val != 100 {
			t.Errorf("PutIfAbsent() = (%v, %v), want (100, false)", val, inserted)
		}
	})

	t.Run("RemoveIf", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		m.Put("key", 100)

		if !m.RemoveIf("key", 100) {
			t.Error("RemoveIf() = false, want true")
		}
		if m.RemoveIf("key", 100) {
			t.Error("RemoveIf() = true, want false after removal")
		}
	})

	t.Run("Replace", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		m.Put("key", 100)

		oldVal, replaced := m.Replace("key", 200)
		if !replaced || oldVal != 100 {
			t.Errorf("Replace() = (%v, %v), want (100, true)", oldVal, replaced)
		}

		_, replaced = m.Replace("missing", 300)
		if replaced {
			t.Error("Replace() on missing key should return false")
		}
	})

	t.Run("ReplaceIf", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		m.Put("key", 100)

		if !m.ReplaceIf("key", 100, 200) {
			t.Error("ReplaceIf() = false, want true")
		}
		if m.ReplaceIf("key", 100, 300) {
			t.Error("ReplaceIf() with wrong old value should return false")
		}
	})
}

func TestSyncMap_ComputeOperations(t *testing.T) {
	t.Run("Compute", func(t *testing.T) {
		m := NewSyncMap[string, int]()

		val, ok := m.Compute("key", func(k string, v int, exists bool) (int, bool) {
			if !exists {
				return 100, true
			}
			return v + 1, true
		})

		if !ok || val != 100 {
			t.Errorf("Compute() = (%v, %v), want (100, true)", val, ok)
		}

		val, ok = m.Compute("key", func(k string, v int, exists bool) (int, bool) {
			return v + 1, true
		})

		if !ok || val != 101 {
			t.Errorf("Compute() = (%v, %v), want (101, true)", val, ok)
		}
	})

	t.Run("ComputeIfAbsent", func(t *testing.T) {
		m := NewSyncMap[string, int]()

		val := m.ComputeIfAbsent("key", func(k string) int {
			return 100
		})

		if val != 100 {
			t.Errorf("ComputeIfAbsent() = %v, want 100", val)
		}

		callCount := 0
		val = m.ComputeIfAbsent("key", func(k string) int {
			callCount++
			return 200
		})

		if val != 100 || callCount != 0 {
			t.Error("ComputeIfAbsent should not call function for existing key")
		}
	})

	t.Run("ComputeIfPresent", func(t *testing.T) {
		m := NewSyncMap[string, int]()
		m.Put("key", 100)

		val, ok := m.ComputeIfPresent("key", func(k string, v int) int {
			return v * 2
		})

		if !ok || val != 200 {
			t.Errorf("ComputeIfPresent() = (%v, %v), want (200, true)", val, ok)
		}

		_, ok = m.ComputeIfPresent("missing", func(k string, v int) int {
			return 999
		})

		if ok {
			t.Error("ComputeIfPresent on missing key should return false")
		}
	})

	t.Run("Merge", func(t *testing.T) {
		m := NewSyncMap[string, int]()

		val := m.Merge("key", 100, func(old, new int) int {
			return old + new
		})

		if val != 100 {
			t.Errorf("Merge() = %v, want 100", val)
		}

		val = m.Merge("key", 50, func(old, new int) int {
			return old + new
		})

		if val != 150 {
			t.Errorf("Merge() = %v, want 150", val)
		}
	})
}

func TestSyncMap_IteratorSafety(t *testing.T) {
	m := NewSyncMap[int, int]()
	for i := 0; i < 100; i++ {
		m.Put(i, i*10)
	}

	t.Run("Iter creates snapshot", func(t *testing.T) {
		count := 0
		for k, v := range m.Iter() {
			count++
			if k*10 != v {
				t.Errorf("Iter() yielded (%v, %v), expected value = key*10", k, v)
			}
			// Modify map during iteration should not affect iteration
			if count == 50 {
				m.Put(1000, 10000)
			}
		}

		if count != 100 {
			t.Errorf("iterated %v items, want 100", count)
		}
	})

	t.Run("concurrent iteration", func(t *testing.T) {
		var wg sync.WaitGroup
		const numIterators = 10

		wg.Add(numIterators)
		for i := 0; i < numIterators; i++ {
			go func() {
				defer wg.Done()
				count := 0
				for range m.Iter() {
					count++
				}
				// Count may vary due to concurrent modifications
			}()
		}

		wg.Wait()
	})
}

func TestSyncMap_Clone(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Put("a", 1)
	m.Put("b", 2)

	cloned := m.Clone()

	t.Run("clone has same content", func(t *testing.T) {
		if cloned.Size() != 2 {
			t.Errorf("cloned Size() = %v, want 2", cloned.Size())
		}
		if val, _ := cloned.Get("a"); val != 1 {
			t.Errorf("cloned Get(a) = %v, want 1", val)
		}
	})

	t.Run("clone is independent", func(t *testing.T) {
		cloned.Put("c", 3)
		m.Put("d", 4)

		if m.ContainsKey("c") {
			t.Error("original should not contain key from clone")
		}
		if cloned.ContainsKey("d") {
			t.Error("clone should not contain key from original")
		}
	})

	t.Run("clone is also thread-safe", func(t *testing.T) {
		_, ok := cloned.(*SyncMap[string, int])
		if !ok {
			t.Error("clone should be *SyncMap")
		}
	})
}

// =============================================================================
// StdSyncMap Tests
// =============================================================================

func TestStdSyncMap_NewStdSyncMap(t *testing.T) {
	m := NewStdSyncMap[string, int]()
	if m == nil {
		t.Fatal("NewStdSyncMap returned nil")
	}
	if !m.IsEmpty() {
		t.Error("new map should be empty")
	}
}

func TestStdSyncMap_BasicOperations(t *testing.T) {
	m := NewStdSyncMap[string, int]()

	t.Run("Put and Get", func(t *testing.T) {
		oldVal, existed := m.Put("key1", 100)
		if existed {
			t.Error("Put() on new key should return existed=false")
		}
		if oldVal != 0 {
			t.Errorf("Put() old value = %v, want 0", oldVal)
		}

		val, ok := m.Get("key1")
		if !ok || val != 100 {
			t.Errorf("Get() = (%v, %v), want (100, true)", val, ok)
		}

		oldVal, existed = m.Put("key1", 200)
		if !existed || oldVal != 100 {
			t.Errorf("Put() on existing key = (%v, %v), want (100, true)", oldVal, existed)
		}
	})

	t.Run("Remove", func(t *testing.T) {
		m.Put("key2", 300)
		val, removed := m.Remove("key2")
		if !removed || val != 300 {
			t.Errorf("Remove() = (%v, %v), want (300, true)", val, removed)
		}

		_, removed = m.Remove("key2")
		if removed {
			t.Error("Remove() on non-existent key should return false")
		}
	})

	t.Run("ContainsKey", func(t *testing.T) {
		m.Put("key3", 400)
		if !m.ContainsKey("key3") {
			t.Error("ContainsKey() = false, want true")
		}
		if m.ContainsKey("missing") {
			t.Error("ContainsKey() = true, want false")
		}
	})
}

func TestStdSyncMap_ConcurrentOperations(t *testing.T) {
	m := NewStdSyncMap[int, int]()

	t.Run("concurrent Put", func(t *testing.T) {
		const numGoroutines = 50
		const numOpsPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < numOpsPerGoroutine; j++ {
					key := id*numOpsPerGoroutine + j
					m.Put(key, key*10)
				}
			}(i)
		}

		wg.Wait()

		expectedSize := numGoroutines * numOpsPerGoroutine
		if m.Size() != expectedSize {
			t.Errorf("Size() = %v, want %v", m.Size(), expectedSize)
		}
	})

	t.Run("concurrent Get", func(t *testing.T) {
		const numReaders = 20
		var wg sync.WaitGroup
		wg.Add(numReaders)
		errors := make(chan error, numReaders)

		for i := 0; i < numReaders; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					val, ok := m.Get(j)
					if ok && val != j*10 {
						errors <- fmt.Errorf("Get(%d) = %v, want %v", j, val, j*10)
						return
					}
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Error(err)
		}
	})
}

func TestStdSyncMap_PutIfAbsent(t *testing.T) {
	m := NewStdSyncMap[string, int]()

	val, inserted := m.PutIfAbsent("key", 100)
	if !inserted || val != 100 {
		t.Errorf("PutIfAbsent() = (%v, %v), want (100, true)", val, inserted)
	}

	val, inserted = m.PutIfAbsent("key", 200)
	if inserted || val != 100 {
		t.Errorf("PutIfAbsent() on existing key = (%v, %v), want (100, false)", val, inserted)
	}
}

func TestStdSyncMap_ComputeOperations(t *testing.T) {
	t.Run("Compute new value", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()

		val, ok := m.Compute("key", func(k string, v int, exists bool) (int, bool) {
			if !exists {
				return 100, true
			}
			return v + 1, true
		})

		if !ok || val != 100 {
			t.Errorf("Compute() = (%v, %v), want (100, true)", val, ok)
		}
	})

	t.Run("Compute update existing", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("key", 100)

		val, ok := m.Compute("key", func(k string, v int, exists bool) (int, bool) {
			if exists {
				return v * 2, true
			}
			return 0, false
		})

		if !ok || val != 200 {
			t.Errorf("Compute() = (%v, %v), want (200, true)", val, ok)
		}
	})

	t.Run("Compute remove", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("key", 100)

		_, ok := m.Compute("key", func(k string, v int, exists bool) (int, bool) {
			return 0, false
		})

		if ok {
			t.Error("Compute() should return false when removing")
		}
		if m.ContainsKey("key") {
			t.Error("key should be removed")
		}
	})

	t.Run("ComputeIfAbsent", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()

		val := m.ComputeIfAbsent("key", func(k string) int {
			return 100
		})

		if val != 100 {
			t.Errorf("ComputeIfAbsent() = %v, want 100", val)
		}

		// Should not recompute
		val = m.ComputeIfAbsent("key", func(k string) int {
			return 200
		})

		if val != 100 {
			t.Errorf("ComputeIfAbsent() = %v, want 100 (should not recompute)", val)
		}
	})

	t.Run("ComputeIfPresent", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("key", 100)

		val, ok := m.ComputeIfPresent("key", func(k string, v int) int {
			return v * 3
		})

		if !ok || val != 300 {
			t.Errorf("ComputeIfPresent() = (%v, %v), want (300, true)", val, ok)
		}

		_, ok = m.ComputeIfPresent("missing", func(k string, v int) int {
			return 999
		})

		if ok {
			t.Error("ComputeIfPresent() on missing key should return false")
		}
	})
}

func TestStdSyncMap_Merge(t *testing.T) {
	m := NewStdSyncMap[string, int]()

	t.Run("merge into non-existing key", func(t *testing.T) {
		val := m.Merge("key", 100, func(old, new int) int {
			return old + new
		})

		if val != 100 {
			t.Errorf("Merge() = %v, want 100", val)
		}
	})

	t.Run("merge into existing key", func(t *testing.T) {
		val := m.Merge("key", 50, func(old, new int) int {
			return old + new
		})

		if val != 150 {
			t.Errorf("Merge() = %v, want 150", val)
		}
	})
}

func TestStdSyncMap_ReplaceOperations(t *testing.T) {
	t.Run("Replace existing", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("key", 100)

		oldVal, replaced := m.Replace("key", 200)
		if !replaced || oldVal != 100 {
			t.Errorf("Replace() = (%v, %v), want (100, true)", oldVal, replaced)
		}
	})

	t.Run("Replace non-existing", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()

		_, replaced := m.Replace("missing", 100)
		if replaced {
			t.Error("Replace() on missing key should return false")
		}
	})

	t.Run("ReplaceIf matching", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("key", 100)

		if !m.ReplaceIf("key", 100, 200) {
			t.Error("ReplaceIf() = false, want true")
		}

		val, _ := m.Get("key")
		if val != 200 {
			t.Errorf("Get() = %v, want 200", val)
		}
	})

	t.Run("ReplaceIf non-matching", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("key", 100)

		if m.ReplaceIf("key", 999, 200) {
			t.Error("ReplaceIf() with wrong old value should return false")
		}

		val, _ := m.Get("key")
		if val != 100 {
			t.Errorf("Get() = %v, want 100 (value should not change)", val)
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)
		m.Put("c", 3)

		m.ReplaceAll(func(k string, v int) int {
			return v * 10
		})

		if val, _ := m.Get("a"); val != 10 {
			t.Errorf("Get(a) = %v, want 10", val)
		}
		if val, _ := m.Get("b"); val != 20 {
			t.Errorf("Get(b) = %v, want 20", val)
		}
		if val, _ := m.Get("c"); val != 30 {
			t.Errorf("Get(c) = %v, want 30", val)
		}
	})
}

func TestStdSyncMap_BulkOperations(t *testing.T) {
	t.Run("PutAll", func(t *testing.T) {
		m1 := NewStdSyncMap[string, int]()
		m1.Put("a", 1)
		m1.Put("b", 2)

		m2 := NewStdSyncMap[string, int]()
		m2.PutAll(m1)

		if m2.Size() != 2 {
			t.Errorf("Size() = %v, want 2", m2.Size())
		}
	})

	t.Run("Clear", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)
		m.Clear()

		if !m.IsEmpty() {
			t.Error("map should be empty after Clear()")
		}
	})

	t.Run("Keys, Values, Entries", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)

		keys := m.Keys()
		if len(keys) != 2 {
			t.Errorf("len(Keys()) = %v, want 2", len(keys))
		}

		values := m.Values()
		if len(values) != 2 {
			t.Errorf("len(Values()) = %v, want 2", len(values))
		}

		entries := m.Entries()
		if len(entries) != 2 {
			t.Errorf("len(Entries()) = %v, want 2", len(entries))
		}
	})

	t.Run("ForEach", func(t *testing.T) {
		m := NewStdSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)
		m.Put("c", 3)

		sum := 0
		m.ForEach(func(k string, v int) {
			sum += v
		})

		if sum != 6 {
			t.Errorf("sum = %v, want 6", sum)
		}
	})
}

func TestStdSyncMap_Iterator(t *testing.T) {
	m := NewStdSyncMap[int, int]()
	for i := 0; i < 10; i++ {
		m.Put(i, i*10)
	}

	t.Run("Iter", func(t *testing.T) {
		count := 0
		for k, v := range m.Iter() {
			if k*10 != v {
				t.Errorf("Iter() yielded (%v, %v), expected value = key*10", k, v)
			}
			count++
		}

		if count != 10 {
			t.Errorf("iterated %v items, want 10", count)
		}
	})

	t.Run("IterKeys", func(t *testing.T) {
		count := 0
		for k := range m.IterKeys() {
			if k < 0 || k >= 10 {
				t.Errorf("IterKeys() yielded invalid key %v", k)
			}
			count++
		}

		if count != 10 {
			t.Errorf("iterated %v keys, want 10", count)
		}
	})

	t.Run("IterValues", func(t *testing.T) {
		count := 0
		for v := range m.IterValues() {
			if v%10 != 0 || v < 0 || v >= 100 {
				t.Errorf("IterValues() yielded invalid value %v", v)
			}
			count++
		}

		if count != 10 {
			t.Errorf("iterated %v values, want 10", count)
		}
	})
}

func TestStdSyncMap_Clone(t *testing.T) {
	m := NewStdSyncMap[string, int]()
	m.Put("a", 1)
	m.Put("b", 2)

	cloned := m.Clone()

	t.Run("clone has same content", func(t *testing.T) {
		if cloned.Size() != 2 {
			t.Errorf("cloned Size() = %v, want 2", cloned.Size())
		}
	})

	t.Run("clone is independent", func(t *testing.T) {
		cloned.Put("c", 3)
		m.Put("d", 4)

		if m.ContainsKey("c") {
			t.Error("original should not contain key from clone")
		}
		if cloned.ContainsKey("d") {
			t.Error("clone should not contain key from original")
		}
	})
}

// =============================================================================
// Stress Tests
// =============================================================================

func TestSyncMap_StressConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	m := NewSyncMap[int, int]()
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := rand.Intn(100)
				op := rand.Intn(4)

				switch op {
				case 0: // Put
					m.Put(key, id*1000+j)
				case 1: // Get
					m.Get(key)
				case 2: // Remove
					m.Remove(key)
				case 3: // Compute
					m.Compute(key, func(k int, v int, exists bool) (int, bool) {
						if exists {
							return v + 1, true
						}
						return 1, true
					})
				}
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no panics or deadlocks occur
}

func TestStdSyncMap_StressConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	m := NewStdSyncMap[int, int]()
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := rand.Intn(100)
				op := rand.Intn(5)

				switch op {
				case 0: // Put
					m.Put(key, id*1000+j)
				case 1: // Get
					m.Get(key)
				case 2: // Remove
					m.Remove(key)
				case 3: // PutIfAbsent
					m.PutIfAbsent(key, id*1000+j)
				case 4: // Merge
					m.Merge(key, 1, func(old, new int) int {
						return old + new
					})
				}
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no panics occur
}

// =============================================================================
// Uncomparable Values, Callback Reentrancy and Bulk-Copy Deadlocks
// =============================================================================

// runWithTimeout runs fn in its own goroutine and fails the test when fn does
// not return in time, so a deadlock surfaces as a failure instead of a hang.
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("operation did not finish within %v (deadlock?)", d)
	}
}

// TestStdSyncMap_UncomparableValues verifies that the comparison-based
// operations work for value types that do not support ==: sync.Map's own
// CompareAndSwap / CompareAndDelete panic on those, so none of these paths may
// reach them.
func TestStdSyncMap_UncomparableValues(t *testing.T) {
	t.Run("RemoveIf", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()
		m.Put("k", []int{1, 2})

		if m.RemoveIf("k", []int{3}) {
			t.Error("RemoveIf() = true for a different value, want false")
		}
		if !m.RemoveIf("k", []int{1, 2}) {
			t.Error("RemoveIf() = false for the current value, want true")
		}
		if _, exists := m.Get("k"); exists {
			t.Error("key still present after RemoveIf")
		}
	})

	t.Run("Replace", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()
		m.Put("k", []int{1})

		oldValue, replaced := m.Replace("k", []int{2})
		if !replaced || !reflect.DeepEqual(oldValue, []int{1}) {
			t.Errorf("Replace() = (%v, %v), want ([1], true)", oldValue, replaced)
		}
		if value, _ := m.Get("k"); !reflect.DeepEqual(value, []int{2}) {
			t.Errorf("Get() = %v, want [2]", value)
		}
		if _, replaced := m.Replace("missing", []int{3}); replaced {
			t.Error("Replace() on a missing key reported a replacement")
		}
	})

	t.Run("ReplaceIf", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()
		m.Put("k", []int{1})

		if m.ReplaceIf("k", []int{2}, []int{3}) {
			t.Error("ReplaceIf() = true for a different old value, want false")
		}
		if !m.ReplaceIf("k", []int{1}, []int{3}) {
			t.Error("ReplaceIf() = false for the current value, want true")
		}
		if value, _ := m.Get("k"); !reflect.DeepEqual(value, []int{3}) {
			t.Errorf("Get() = %v, want [3]", value)
		}
	})

	t.Run("Compute", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()
		m.Put("k", []int{1})

		value, ok := m.Compute("k", func(_ string, old []int, exists bool) ([]int, bool) {
			if !exists {
				t.Error("Compute() did not report the existing entry")
			}
			return append(old, 2), true
		})
		if !ok || !reflect.DeepEqual(value, []int{1, 2}) {
			t.Errorf("Compute() = (%v, %v), want ([1 2], true)", value, ok)
		}

		_, ok = m.Compute("k", func(_ string, _ []int, _ bool) ([]int, bool) {
			return nil, false
		})
		if ok {
			t.Error("Compute() = true after the remapping returned false, want false")
		}
		if _, exists := m.Get("k"); exists {
			t.Error("Compute() with shouldPut=false kept the entry")
		}
	})

	t.Run("ComputeIfPresent", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()
		m.Put("k", []int{1})

		value, ok := m.ComputeIfPresent("k", func(_ string, old []int) []int {
			return append(old, 9)
		})
		if !ok || !reflect.DeepEqual(value, []int{1, 9}) {
			t.Errorf("ComputeIfPresent() = (%v, %v), want ([1 9], true)", value, ok)
		}
		if _, ok := m.ComputeIfPresent("missing", func(_ string, old []int) []int {
			return old
		}); ok {
			t.Error("ComputeIfPresent() on a missing key = true, want false")
		}
	})

	t.Run("Merge", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()

		if value := m.Merge("k", []int{1}, func(old, new []int) []int {
			return append(old, new...)
		}); !reflect.DeepEqual(value, []int{1}) {
			t.Errorf("Merge() on a missing key = %v, want [1]", value)
		}

		value := m.Merge("k", []int{2}, func(old, new []int) []int {
			return append(old, new...)
		})
		if !reflect.DeepEqual(value, []int{1, 2}) {
			t.Errorf("Merge() = %v, want [1 2]", value)
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		m := NewStdSyncMap[string, []int]()
		m.Put("a", []int{1})
		m.Put("b", []int{2})

		m.ReplaceAll(func(_ string, old []int) []int {
			return append(old, 0)
		})

		for key, want := range map[string][]int{"a": {1, 0}, "b": {2, 0}} {
			if value, _ := m.Get(key); !reflect.DeepEqual(value, want) {
				t.Errorf("Get(%q) = %v, want %v", key, value, want)
			}
		}
		m.ForEach(func(key string, value []int) {
			if len(value) != 2 || value[1] != 0 {
				t.Errorf("ForEach(%q) = %v, want the replaced value", key, value)
			}
		})
	})
}

// TestStdSyncMap_NilValues verifies that a stored nil value round-trips: the
// value type is an interface here, so the nil interface is stored as such and
// must not turn into a failed type assertion.
func TestStdSyncMap_NilValues(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		m := NewStdSyncMap[string, any]()
		m.Put("k", nil)

		value, exists := m.Get("k")
		if value != nil || !exists {
			t.Errorf("Get() = (%v, %v), want (nil, true)", value, exists)
		}
		if !m.ContainsKey("k") {
			t.Error("ContainsKey() = false for a key mapped to nil")
		}
		if got := m.Size(); got != 1 {
			t.Errorf("Size() = %d, want 1", got)
		}
		if entries := m.Entries(); len(entries) != 1 || entries[0].Value() != nil {
			t.Errorf("Entries() = %v, want one nil-valued entry", entries)
		}
		if values := m.Values(); len(values) != 1 || values[0] != nil {
			t.Errorf("Values() = %v, want [nil]", values)
		}
		if keys := m.Keys(); len(keys) != 1 || keys[0] != "k" {
			t.Errorf("Keys() = %v, want [k]", keys)
		}
		if !m.ContainsValue(nil) {
			t.Error("ContainsValue(nil) = false, want true")
		}
		if got := m.GetOrDefault("k", "fallback"); got != nil {
			t.Errorf("GetOrDefault() = %v, want nil", got)
		}
		m.ForEach(func(_ string, value any) {
			if value != nil {
				t.Errorf("ForEach() yielded %v, want nil", value)
			}
		})
		if !m.RemoveIf("k", nil) {
			t.Error("RemoveIf(nil) = false for a nil-valued entry, want true")
		}
		if m.ContainsKey("k") {
			t.Error("key still present after RemoveIf(nil)")
		}
	})

	t.Run("nil value replaced", func(t *testing.T) {
		m := NewStdSyncMap[string, any]()
		m.Put("k", nil)

		oldValue, replaced := m.Replace("k", 7)
		if oldValue != nil || !replaced {
			t.Errorf("Replace() = (%v, %v), want (nil, true)", oldValue, replaced)
		}
		if value, _ := m.Get("k"); value != 7 {
			t.Errorf("Get() = %v, want 7", value)
		}

		removed, existed := m.Remove("k")
		if removed != 7 || !existed {
			t.Errorf("Remove() = (%v, %v), want (7, true)", removed, existed)
		}
	})

	t.Run("nil key", func(t *testing.T) {
		m := NewStdSyncMap[any, string]()
		m.Put(nil, "value")

		if value, exists := m.Get(nil); value != "value" || !exists {
			t.Errorf("Get(nil) = (%v, %v), want (value, true)", value, exists)
		}
		if keys := m.Keys(); len(keys) != 1 || keys[0] != nil {
			t.Errorf("Keys() = %v, want [<nil>]", keys)
		}
		for key, value := range m.Iter() {
			if key != nil || value != "value" {
				t.Errorf("Iter() yielded (%v, %v), want (<nil>, value)", key, value)
			}
		}
	})
}

// TestSyncMap_UncomparableValues verifies the mutex-based implementation
// compares values that do not support == instead of panicking.
func TestSyncMap_UncomparableValues(t *testing.T) {
	t.Run("Compute", func(t *testing.T) {
		m := NewSyncMap[string, map[string]int]()
		m.Put("k", map[string]int{"n": 1})

		value, ok := m.Compute("k", func(_ string, old map[string]int, exists bool) (map[string]int, bool) {
			if !exists {
				t.Error("Compute() did not report the existing entry")
			}
			old["n"]++
			return old, true
		})
		if !ok || value["n"] != 2 {
			t.Errorf("Compute() = (%v, %v), want (map[n:2], true)", value, ok)
		}
	})

	t.Run("ComputeIfPresent", func(t *testing.T) {
		m := NewSyncMap[string, []int]()
		m.Put("k", []int{1})

		value, ok := m.ComputeIfPresent("k", func(_ string, old []int) []int {
			return append(old, 2)
		})
		if !ok || !reflect.DeepEqual(value, []int{1, 2}) {
			t.Errorf("ComputeIfPresent() = (%v, %v), want ([1 2], true)", value, ok)
		}
	})

	t.Run("Merge", func(t *testing.T) {
		m := NewSyncMap[string, []int]()
		m.Merge("k", []int{1}, func(old, new []int) []int { return append(old, new...) })
		value := m.Merge("k", []int{2}, func(old, new []int) []int { return append(old, new...) })
		if !reflect.DeepEqual(value, []int{1, 2}) {
			t.Errorf("Merge() = %v, want [1 2]", value)
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		m := NewSyncMap[string, []int]()
		m.Put("k", []int{1})

		m.ReplaceAll(func(_ string, old []int) []int { return append(old, 2) })

		if value, _ := m.Get("k"); !reflect.DeepEqual(value, []int{1, 2}) {
			t.Errorf("Get() = %v, want [1 2]", value)
		}
	})
}

// TestSyncMap_ComputeRetriesStaleAttempt verifies the documented retry
// contract: a remapping result computed for a value another writer replaced in
// the meantime is discarded, the function runs again for the new value, and the
// returned value is the one that was stored.
func TestSyncMap_ComputeRetriesStaleAttempt(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Put("k", 1)

	calls := 0
	value, ok := m.Compute("k", func(key string, old int, exists bool) (int, bool) {
		calls++
		if calls == 1 {
			// Stands in for a concurrent writer: the entry changes while the
			// function runs, so the result computed from 1 must not be applied.
			m.Put(key, 100)
			return old * 10, true
		}
		if old != 100 {
			t.Errorf("retry saw old value %d, want 100", old)
		}
		return old + 1, true
	})

	if !ok || value != 101 {
		t.Errorf("Compute() = (%v, %v), want (101, true)", value, ok)
	}
	if calls != 2 {
		t.Errorf("remapping function called %d times, want 2", calls)
	}
	if stored, _ := m.Get("k"); stored != 101 {
		t.Errorf("stored value = %v, want 101", stored)
	}
}

// TestSyncMap_CallbacksMayReenter verifies that no user callback runs while a
// lock is held, so a callback can mutate the map it is called from.
func TestSyncMap_CallbacksMayReenter(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		m := NewSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)

		m.ForEach(func(key string, value int) {
			m.Put(key+"!", value)
		})
		if size := m.Size(); size != 4 {
			t.Errorf("Size() after ForEach mutation = %d, want 4", size)
		}

		m.ReplaceAll(func(key string, value int) int {
			m.Put(key+"?", value)
			return value * 10
		})
		if value, _ := m.Get("a"); value != 10 {
			t.Errorf("Get(a) = %v, want 10", value)
		}
		if _, exists := m.Get("a?"); !exists {
			t.Error("ReplaceAll callback could not add a key")
		}
	})
}

// TestSyncMap_ReplaceAllKeepsConcurrentRemovals verifies that a value computed
// for an entry that was removed while the callback ran is not written back.
func TestSyncMap_ReplaceAllKeepsConcurrentRemovals(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Put("a", 1)
	m.Put("b", 2)

	m.ReplaceAll(func(key string, value int) int {
		if key == "b" {
			// Stands in for another goroutine removing the entry during the
			// callback: the entry must not be resurrected afterwards.
			m.Remove("b")
		}
		return value * 10
	})

	if _, exists := m.Get("b"); exists {
		t.Error("ReplaceAll resurrected an entry removed while the callback ran")
	}
	if value, _ := m.Get("a"); value != 10 {
		t.Errorf("Get(a) = %v, want 10", value)
	}
}

// TestSyncMap_PutAllSelf verifies a map can copy into itself without
// deadlocking, and that the copy is a no-op for its own entries.
func TestSyncMap_PutAllSelf(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		m := NewSyncMap[string, int]()
		m.Put("a", 1)
		m.Put("b", 2)

		m.PutAll(m)

		if size := m.Size(); size != 2 {
			t.Errorf("Size() after PutAll(self) = %d, want 2", size)
		}
		if value, _ := m.Get("a"); value != 1 {
			t.Errorf("Get(a) = %v, want 1", value)
		}
	})
}

// TestSyncMap_PutAllCopyIsBatched verifies the source snapshot is taken before
// the destination lock, so two maps copying into each other concurrently cannot
// deadlock, and a nil source is a no-op.
func TestSyncMap_PutAllCopyIsBatched(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		first := NewSyncMap[string, int]()
		second := NewSyncMap[string, int]()
		first.Put("a", 1)
		second.Put("b", 2)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				first.PutAll(second)
			}()
			go func() {
				defer wg.Done()
				second.PutAll(first)
			}()
		}
		wg.Wait()

		for key, want := range map[string]int{"a": 1, "b": 2} {
			if value, exists := first.Get(key); !exists || value != want {
				t.Errorf("first.Get(%q) = (%v, %v), want (%v, true)", key, value, exists, want)
			}
			if value, exists := second.Get(key); !exists || value != want {
				t.Errorf("second.Get(%q) = (%v, %v), want (%v, true)", key, value, exists, want)
			}
		}
	})

	m := NewSyncMap[string, int]()
	m.Put("a", 1)
	m.PutAll(nil)
	if size := m.Size(); size != 1 {
		t.Errorf("Size() after PutAll(nil) = %d, want 1", size)
	}
}

// =============================================================================
// Performance Benchmarks
// =============================================================================

func BenchmarkSyncMap_Put(b *testing.B) {
	m := NewSyncMap[int, int]()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Put(i, i)
			i++
		}
	})
}

func BenchmarkSyncMap_Get(b *testing.B) {
	m := NewSyncMap[int, int]()
	for i := 0; i < 10000; i++ {
		m.Put(i, i)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Get(i % 10000)
			i++
		}
	})
}

func BenchmarkStdSyncMap_Put(b *testing.B) {
	m := NewStdSyncMap[int, int]()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Put(i, i)
			i++
		}
	})
}

func BenchmarkStdSyncMap_Get(b *testing.B) {
	m := NewStdSyncMap[int, int]()
	for i := 0; i < 10000; i++ {
		m.Put(i, i)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Get(i % 10000)
			i++
		}
	})
}

func BenchmarkSyncMap_ReadHeavy(b *testing.B) {
	m := NewSyncMap[int, int]()
	for i := 0; i < 1000; i++ {
		m.Put(i, i)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				m.Put(i%1000, i)
			} else {
				m.Get(i % 1000)
			}
			i++
		}
	})
}

func BenchmarkStdSyncMap_ReadHeavy(b *testing.B) {
	m := NewStdSyncMap[int, int]()
	for i := 0; i < 1000; i++ {
		m.Put(i, i)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				m.Put(i%1000, i)
			} else {
				m.Get(i % 1000)
			}
			i++
		}
	})
}

func BenchmarkSyncMap_vs_StdSyncMap_ReadHeavy(b *testing.B) {
	b.Run("SyncMap", func(b *testing.B) {
		m := NewSyncMap[int, int]()
		for i := 0; i < 1000; i++ {
			m.Put(i, i)
		}
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%100 == 0 {
					m.Put(i%1000, i)
				} else {
					m.Get(i % 1000)
				}
				i++
			}
		})
	})

	b.Run("StdSyncMap", func(b *testing.B) {
		m := NewStdSyncMap[int, int]()
		for i := 0; i < 1000; i++ {
			m.Put(i, i)
		}
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%100 == 0 {
					m.Put(i%1000, i)
				} else {
					m.Get(i % 1000)
				}
				i++
			}
		})
	})
}

func BenchmarkSyncMap_Compute(b *testing.B) {
	m := NewSyncMap[int, int]()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Compute(i%1000, func(k int, v int, exists bool) (int, bool) {
				if exists {
					return v + 1, true
				}
				return 1, true
			})
			i++
		}
	})
}

func BenchmarkStdSyncMap_Compute(b *testing.B) {
	m := NewStdSyncMap[int, int]()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Compute(i%1000, func(k int, v int, exists bool) (int, bool) {
				if exists {
					return v + 1, true
				}
				return 1, true
			})
			i++
		}
	})
}
