package retry

import (
	"math"
	"slices"
	"testing"
	"time"
)

// stubJitter replaces the package's jitter source with a deterministic one and
// records the exclusive bounds it is asked for. The previous source is restored
// when the test finishes, so tests using it must not run in parallel.
func stubJitter(t *testing.T, draw func(n int64) int64) *[]int64 {
	t.Helper()
	bounds := &[]int64{}
	previous := randInt64N
	randInt64N = func(n int64) int64 {
		*bounds = append(*bounds, n)
		return draw(n)
	}
	t.Cleanup(func() { randInt64N = previous })
	return bounds
}

// failingJitter fails the test if delay computation consults the random
// source at all.
func failingJitter(t *testing.T) {
	t.Helper()
	stubJitter(t, func(int64) int64 {
		t.Error("random source consulted, want none")
		return 0
	})
}

// captureSleep records the delay requested for each retry instead of waiting.
func captureSleep(delays *[]time.Duration) func(time.Duration) <-chan time.Time {
	return func(d time.Duration) <-chan time.Time {
		*delays = append(*delays, d)
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
}

func TestRandomJitter_UsesDeterministicSource(t *testing.T) {
	const maxJitter = 100 * time.Millisecond
	bounds := stubJitter(t, func(n int64) int64 { return n - 1 })

	got := RandomJitter(0, nil, DelayConfig{MaxJitter: maxJitter})
	if want := maxJitter - 1; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if want := []int64{int64(maxJitter)}; !slices.Equal(*bounds, want) {
		t.Errorf("bounds = %v, want %v", *bounds, want)
	}
}

func TestRandomJitter_Disabled(t *testing.T) {
	failingJitter(t)
	for _, maxJitter := range []time.Duration{0, -time.Millisecond} {
		if got := RandomJitter(0, nil, DelayConfig{MaxJitter: maxJitter}); got != 0 {
			t.Errorf("MaxJitter %v: got %v, want 0", maxJitter, got)
		}
	}
}

func TestFullJitterBackoff_Ceiling(t *testing.T) {
	const base = 100 * time.Millisecond
	tests := []struct {
		name      string
		attempt   int
		cfg       DelayConfig
		wantBound time.Duration
	}{
		{
			name:      "first attempt",
			attempt:   1,
			cfg:       DelayConfig{BaseDelay: base, MaxBackoffStep: 10},
			wantBound: 200 * time.Millisecond,
		},
		{
			name:      "later attempts grow the ceiling",
			attempt:   3,
			cfg:       DelayConfig{BaseDelay: base, MaxBackoffStep: 10},
			wantBound: 800 * time.Millisecond,
		},
		{
			name:      "MaxBackoffStep caps the exponent",
			attempt:   5,
			cfg:       DelayConfig{BaseDelay: base, MaxBackoffStep: 2},
			wantBound: 400 * time.Millisecond,
		},
		{
			name:      "MaxDelay caps the ceiling",
			attempt:   3,
			cfg:       DelayConfig{BaseDelay: base, MaxDelay: 250 * time.Millisecond, MaxBackoffStep: 10},
			wantBound: 250 * time.Millisecond,
		},
		{
			name:      "MaxDelay above the ceiling is ignored",
			attempt:   2,
			cfg:       DelayConfig{BaseDelay: base, MaxDelay: time.Second, MaxBackoffStep: 10},
			wantBound: 400 * time.Millisecond,
		},
		{
			name:      "shift overflow saturates at MaxInt64",
			attempt:   1,
			cfg:       DelayConfig{BaseDelay: 1 << 62},
			wantBound: time.Duration(math.MaxInt64),
		},
		{
			// 1<<62 << 2 wraps past MaxInt64 to exactly 0, which is not
			// negative and so used to slip past the overflow check.
			name:      "wrapped shift saturates at MaxInt64",
			attempt:   2,
			cfg:       DelayConfig{BaseDelay: 1 << 62},
			wantBound: time.Duration(math.MaxInt64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bounds := stubJitter(t, func(n int64) int64 { return n - 1 })

			got := FullJitterBackoff(tt.attempt, nil, tt.cfg)
			if want := tt.wantBound - 1; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
			if want := []int64{int64(tt.wantBound)}; !slices.Equal(*bounds, want) {
				t.Errorf("bounds = %v, want %v", *bounds, want)
			}
		})
	}
}

// TestFullJitterBackoff_ZeroCeiling covers the inputs that produce no jitter at
// all, before the random source is consulted.
func TestFullJitterBackoff_ZeroCeiling(t *testing.T) {
	failingJitter(t)
	tests := []struct {
		name    string
		attempt int
		cfg     DelayConfig
	}{
		{"zero attempt", 0, DelayConfig{BaseDelay: time.Second}},
		{"negative attempt", -1, DelayConfig{BaseDelay: time.Second}},
		{"zero base delay", 1, DelayConfig{}},
		{"negative base delay", 1, DelayConfig{BaseDelay: -time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FullJitterBackoff(tt.attempt, nil, tt.cfg); got != 0 {
				t.Errorf("got %v, want 0", got)
			}
		})
	}
}

func TestWithFixedDelay_ReplacesJitteredDefault(t *testing.T) {
	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(4),
		WithBaseDelay(50*time.Millisecond),
		WithFixedDelay(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}
	want := []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond}
	if !slices.Equal(delays, want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}

func TestWithExponentialBackoff_DoublesEachAttempt(t *testing.T) {
	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(4),
		WithBaseDelay(50*time.Millisecond),
		WithMaxJitter(0),
		WithExponentialBackoff(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	if !slices.Equal(delays, want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}

func TestWithMaxDelay_CapsEveryAttempt(t *testing.T) {
	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(4),
		WithBaseDelay(10*time.Millisecond),
		WithMaxDelay(25*time.Millisecond),
		WithMaxJitter(0),
		WithExponentialBackoff(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}
	want := []time.Duration{20 * time.Millisecond, 25 * time.Millisecond, 25 * time.Millisecond}
	if !slices.Equal(delays, want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}

func TestWithBackoffStep_CapsExponent(t *testing.T) {
	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(5),
		WithBaseDelay(10*time.Millisecond),
		WithBackoffStep(2),
		WithMaxJitter(0),
		WithExponentialBackoff(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}
	want := []time.Duration{
		20 * time.Millisecond,
		40 * time.Millisecond,
		40 * time.Millisecond,
		40 * time.Millisecond,
	}
	if !slices.Equal(delays, want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}

func TestWithMaxJitter_AddsStubbedJitter(t *testing.T) {
	const maxJitter = 10 * time.Millisecond
	bounds := stubJitter(t, func(n int64) int64 { return n - 1 })

	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(3),
		WithBaseDelay(50*time.Millisecond),
		WithMaxJitter(maxJitter),
		WithExponentialBackoff(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}

	want := []time.Duration{
		100*time.Millisecond + maxJitter - 1,
		200*time.Millisecond + maxJitter - 1,
	}
	if !slices.Equal(delays, want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
	wantBounds := []int64{int64(maxJitter), int64(maxJitter)}
	if !slices.Equal(*bounds, wantBounds) {
		t.Errorf("bounds = %v, want %v", *bounds, wantBounds)
	}
}

func TestWithFullJitter_AppliesStubbedDelay(t *testing.T) {
	bounds := stubJitter(t, func(n int64) int64 { return n / 2 })

	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(3),
		WithBaseDelay(100*time.Millisecond),
		WithFullJitter(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}

	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}
	if !slices.Equal(delays, want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
	wantBounds := []int64{int64(200 * time.Millisecond), int64(400 * time.Millisecond)}
	if !slices.Equal(*bounds, wantBounds) {
		t.Errorf("bounds = %v, want %v", *bounds, wantBounds)
	}
}

func TestOptionNormalization(t *testing.T) {
	const base = 100 * time.Millisecond
	tests := []struct {
		name string
		opt  Option
		want DelayConfig
	}{
		{
			name: "negative max delay becomes zero",
			opt:  WithMaxDelay(-time.Second),
			want: DelayConfig{BaseDelay: base, MaxJitter: base},
		},
		{
			name: "negative max jitter becomes zero",
			opt:  WithMaxJitter(-time.Second),
			want: DelayConfig{BaseDelay: base},
		},
		{
			name: "negative backoff step removes the cap",
			opt:  WithBackoffStep(-1),
			want: DelayConfig{BaseDelay: base, MaxJitter: base},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRetrier(tt.opt)
			if got := r.inner.strategy.delayConfig; got != tt.want {
				t.Errorf("delayConfig = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestExponentialBackoff_SaturatesOnOverflow(t *testing.T) {
	const max = time.Duration(math.MaxInt64)
	tests := []struct {
		name    string
		attempt int
		base    time.Duration
		want    time.Duration
	}{
		{"shift goes negative", 1, 1 << 62, max},
		{"shift wraps past MaxInt64 to zero", 2, 1 << 62, max},
		{"shift wraps to a small positive value", 2, (1 << 62) + 1, max},
		{"repeated wrap on a later attempt", 3, (1 << 62) + 3, max},
		{"large attempt on a tiny base", 200, time.Nanosecond, max},
		{"largest representable result stays exact", 1, 1 << 61, 1 << 62},
		{"no saturation below the limit", 2, 100 * time.Millisecond, 400 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExponentialBackoff(tt.attempt, nil, DelayConfig{BaseDelay: tt.base})
			if got != tt.want {
				t.Errorf("ExponentialBackoff(%d, base %v) = %v, want %v", tt.attempt, tt.base, got, tt.want)
			}
		})
	}
}

func TestFullJitterBackoff_SaturatesOnOverflow(t *testing.T) {
	bounds := stubJitter(t, func(n int64) int64 { return n - 1 })

	if got, want := FullJitterBackoff(2, nil, DelayConfig{BaseDelay: 1 << 62}), time.Duration(math.MaxInt64)-1; got != want {
		t.Errorf("got %v, want one below MaxInt64", got)
	}
	if want := []int64{int64(math.MaxInt64)}; !slices.Equal(*bounds, want) {
		t.Errorf("bounds = %v, want %v", *bounds, want)
	}
}

// TestWithBackoffStep_ZeroMeansUncapped pins the single meaning of
// MaxBackoffStep == 0, which the retrier no longer rewrites with a derived cap.
func TestWithBackoffStep_ZeroMeansUncapped(t *testing.T) {
	for _, step := range []int{0, -1} {
		r := NewRetrier(WithBaseDelay(time.Nanosecond), WithBackoffStep(step))
		if got := r.inner.strategy.delayConfig.MaxBackoffStep; got != 0 {
			t.Errorf("WithBackoffStep(%d): MaxBackoffStep = %d, want 0", step, got)
		}
	}

	// 1ns << attempt wrapped to 0 from attempt 64 on before the arithmetic
	// saturated; assert the tail of a long run saturates instead of collapsing.
	var delays []time.Duration
	r := NewRetrier(
		WithMaxAttempts(102),
		WithBaseDelay(time.Nanosecond),
		WithMaxJitter(0),
		WithExponentialBackoff(),
		WithSleep(captureSleep(&delays)),
	)
	if err := r.Do(func() error { return errTemporary }); err == nil {
		t.Fatal("expected error")
	}
	if last := delays[len(delays)-1]; last != time.Duration(math.MaxInt64) {
		t.Errorf("final delay = %v, want MaxInt64", last)
	}
}

func TestMaxAttempts_DefaultsAndNormalization(t *testing.T) {
	tests := []struct {
		name string
		opt  Option
		want int
	}{
		{"default", nil, 3},
		{"explicit", WithMaxAttempts(7), 7},
		{"negative becomes unlimited", WithMaxAttempts(-5), 0},
		{"unlimited shorthand", WithUnlimitedAttempts(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{}
			if tt.opt != nil {
				opts = append(opts, tt.opt)
			}
			r := NewRetrier(opts...)
			if got := r.inner.strategy.maxAttempts; got != tt.want {
				t.Errorf("maxAttempts = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWithBaseDelay_NegativeBecomesZero(t *testing.T) {
	r := NewRetrier(WithBaseDelay(-time.Second))
	if got := r.inner.strategy.delayConfig.BaseDelay; got != 0 {
		t.Errorf("BaseDelay = %v, want 0", got)
	}
}

func TestCombineDelays_SaturatesOnOverflow(t *testing.T) {
	huge := func(int, error, DelayConfig) time.Duration { return time.Duration(math.MaxInt64) }
	one := func(int, error, DelayConfig) time.Duration { return time.Nanosecond }
	saturated := time.Duration(math.MaxInt64)

	if got := CombineDelays(huge, one)(1, nil, DelayConfig{}); got != saturated {
		t.Errorf("huge first: got %v, want MaxInt64", got)
	}
	if got := CombineDelays(one, huge)(1, nil, DelayConfig{}); got != saturated {
		t.Errorf("huge last: got %v, want MaxInt64", got)
	}
}

// TestCombineDelays_NegativeComponents guards the earlier check, which compared
// the running total against MaxInt64 - d: for a negative component that
// subtraction wraps and the check reports a spurious MaxInt64, turning a delay
// offset into a 292-year sleep.
func TestCombineDelays_NegativeComponents(t *testing.T) {
	negative := func(int, error, DelayConfig) time.Duration { return -time.Second }
	positive := func(int, error, DelayConfig) time.Duration { return 3 * time.Second }

	if got := CombineDelays(negative)(1, nil, DelayConfig{}); got != -time.Second {
		t.Errorf("single negative: got %v, want -1s", got)
	}
	if got := CombineDelays(positive, negative)(1, nil, DelayConfig{}); got != 2*time.Second {
		t.Errorf("3s then -1s: got %v, want 2s", got)
	}
	if got := CombineDelays(negative, positive)(1, nil, DelayConfig{}); got != 2*time.Second {
		t.Errorf("-1s then 3s: got %v, want 2s", got)
	}

	floor := func(int, error, DelayConfig) time.Duration { return time.Duration(math.MinInt64) }
	if got, want := CombineDelays(floor, negative)(1, nil, DelayConfig{}), time.Duration(math.MinInt64); got != want {
		t.Errorf("negative overflow: got %v, want MinInt64", got)
	}
}
