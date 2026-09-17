package sets

import "fmt"

// Union returns a new set containing every element of s1 and s2.
func Union[T comparable](s1, s2 Set[T]) Set[T] {
	return UnionAll(s1, s2)
}

// Intersection returns a new set containing the elements present in both s1 and s2.
func Intersection[T comparable](s1, s2 Set[T]) Set[T] {
	return IntersectionAll(s1, s2)
}

// Difference returns a new set containing the elements of s1 that are not in s2.
func Difference[T comparable](s1, s2 Set[T]) Set[T] {
	return DifferenceAll(s1, s2)
}

// SymmetricDifference returns a new set containing the elements present in
// exactly one of s1 and s2.
func SymmetricDifference[T comparable](s1, s2 Set[T]) Set[T] {
	result := NewHashSet[T](s1.Size() + s2.Size())

	for x := range s1.Iter() {
		if !s2.Contains(x) {
			result.Add(x)
		}
	}

	for x := range s2.Iter() {
		if !s1.Contains(x) {
			result.Add(x)
		}
	}

	return result
}

// UnionAll returns a new set containing every element of all input sets.
func UnionAll[T comparable](sets ...Set[T]) Set[T] {
	switch len(sets) {
	case 0:
		return NewHashSet[T]()
	case 1:
		return sets[0].Clone()
	default:
		totalSize := 0
		for _, s := range sets {
			totalSize += s.Size()
		}

		result := NewHashSet[T](totalSize)

		for _, s := range sets {
			for x := range s.Iter() {
				result.Add(x)
			}
		}

		return result
	}
}

// IntersectionAll returns a new set containing the elements present in every
// input set.
func IntersectionAll[T comparable](sets ...Set[T]) Set[T] {
	switch len(sets) {
	case 0:
		return NewHashSet[T]()
	case 1:
		return sets[0].Clone()
	case 2:
		s1, s2 := sets[0], sets[1]

		smaller, larger := s1, s2
		if s2.Size() < s1.Size() {
			smaller, larger = s2, s1
		}

		result := NewHashSet[T](smaller.Size())

		for x := range smaller.Iter() {
			if larger.Contains(x) {
				result.Add(x)
			}
		}

		return result
	default:
		result := sets[0].Clone()

		for i := 1; i < len(sets); i++ {
			if result.IsEmpty() {
				break
			}
			result = IntersectionAll(result, sets[i])
		}

		return result
	}
}

// DifferenceAll returns a new set containing the elements of the first set that
// appear in none of the others.
func DifferenceAll[T comparable](sets ...Set[T]) Set[T] {
	switch len(sets) {
	case 0:
		return NewHashSet[T]()
	case 1:
		return sets[0].Clone()
	case 2:
		s1, s2 := sets[0], sets[1]

		result := NewHashSet[T](s1.Size())

		for x := range s1.Iter() {
			if !s2.Contains(x) {
				result.Add(x)
			}
		}

		return result
	default:
		result := sets[0].Clone()

		for i := 1; i < len(sets); i++ {
			if result.IsEmpty() {
				break
			}
			result = DifferenceAll(result, sets[i])
		}

		return result
	}
}

// Equal reports whether s1 and s2 contain exactly the same elements.
func Equal[T comparable](s1, s2 Set[T]) bool {
	if s1.Size() != s2.Size() {
		return false
	}

	if s1.IsEmpty() {
		return true
	}

	smaller, larger := s1, s2
	if s2.Size() < s1.Size() {
		smaller, larger = s2, s1
	}

	for x := range smaller.Iter() {
		if !larger.Contains(x) {
			return false
		}
	}

	return true
}

// IsSubset reports whether every element of s1 is in s2. The empty set is a
// subset of any set.
func IsSubset[T comparable](s1, s2 Set[T]) bool {
	if s1.Size() > s2.Size() {
		return false
	}

	for x := range s1.Iter() {
		if !s2.Contains(x) {
			return false
		}
	}

	return true
}

// IsSuperset reports whether every element of s2 is in s1.
func IsSuperset[T comparable](s1, s2 Set[T]) bool {
	return IsSubset(s2, s1)
}

// IsProperSubset reports whether s1 is a subset of s2 and strictly smaller.
func IsProperSubset[T comparable](s1, s2 Set[T]) bool {
	return s1.Size() < s2.Size() && IsSubset(s1, s2)
}

// IsProperSuperset reports whether s1 is a superset of s2 and strictly larger.
func IsProperSuperset[T comparable](s1, s2 Set[T]) bool {
	return IsProperSubset(s2, s1)
}

// IsDisjoint reports whether s1 and s2 share no elements.
func IsDisjoint[T comparable](s1, s2 Set[T]) bool {
	smaller, larger := s1, s2
	if s2.Size() < s1.Size() {
		smaller, larger = s2, s1
	}

	for x := range smaller.Iter() {
		if larger.Contains(x) {
			return false
		}
	}

	return true
}

// Pair is an ordered pair of values.
type Pair[T, U comparable] struct {
	First  T
	Second U
}

func (p Pair[T, U]) String() string {
	return fmt.Sprintf("(%v, %v)", p.First, p.Second)
}

// CartesianProduct returns the set of all pairs (x, y) with x from s1 and y
// from s2. The result has |s1|×|s2| elements.
func CartesianProduct[T, U comparable](s1 Set[T], s2 Set[U]) Set[Pair[T, U]] {
	result := NewHashSet[Pair[T, U]](s1.Size() * s2.Size())

	for x := range s1.Iter() {
		for y := range s2.Iter() {
			result.Add(Pair[T, U]{First: x, Second: y})
		}
	}
	return result
}

// Of returns a new set containing items, with duplicates removed.
func Of[T comparable](items ...T) Set[T] {
	result := NewHashSet[T](len(items))

	for _, item := range items {
		result.Add(item)
	}

	return result
}
