// Package sets provides set implementations and operations over them.
//
// [HashSet] is unordered, [LinkedSet] follows insertion order, and [SyncSet]
// wraps another [Set] behind a read-write mutex; all three satisfy [Set]. The
// package-level operations — [Union], [Intersection], [Difference],
// [SymmetricDifference] and their variadic forms, the subset, superset,
// equality, and disjointness predicates, and [CartesianProduct] — return a new
// set and leave their arguments unchanged.
package sets
