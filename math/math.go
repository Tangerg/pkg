package math

import (
	"errors"
	"math"
)

// NumericType is the type constraint covering every built-in Go numeric
// kind, including types defined by them via the ~ operator.
type NumericType interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Abs returns the absolute value of x for any [NumericType]. Floating
// point inputs delegate to math.Abs; integers are negated when negative.
// Negating a signed-integer minimum (math.MinInt64 and the like) overflows,
// and Go defines signed overflow as the wrapping bit pattern, so Abs returns
// that minimum unchanged. [MultiplyExact] detects the case when needed.
func Abs[T NumericType](x T) T {
	switch v := any(x).(type) {
	case float32:
		return T(math.Abs(float64(v)))
	case float64:
		return T(math.Abs(v))
	}
	if x < 0 {
		return -x
	}
	return x
}

// ErrOverflow is returned by [MultiplyExact] and [DivideExact] when the
// operation cannot be represented as an int64.
var ErrOverflow = errors.New("math: int64 overflow")

// MultiplyExact returns x*y or [ErrOverflow] if the result does not fit
// in int64. The check covers the special MinInt64 * -1 case.
func MultiplyExact(x, y int64) (int64, error) {
	r := x * y
	if Abs(x)|Abs(y)>>31 != 0 {
		if (y != 0 && r/y != x) || (x == math.MinInt64 && y == -1) {
			return 0, ErrOverflow
		}
	}
	return r, nil
}

// DivideExact returns x/y or [ErrOverflow] for the single overflowing
// case, MinInt64 / -1. It panics on division by zero, matching the /
// operator.
func DivideExact(x, y int64) (int64, error) {
	q := x / y
	if x&y&q >= 0 {
		return q, nil
	}
	return 0, ErrOverflow
}

// IsNumericType reports whether v is one of the built-in numeric kinds.
// User-defined types whose underlying type is numeric return false; use
// the [NumericType] constraint at compile time when generic dispatch is
// possible.
func IsNumericType(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}
