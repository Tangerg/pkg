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
			name:      "overflow saturates at MaxInt64",
			attempt:   1,
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
	autoStep := calculateMaxBackoffStep(base)
	tests := []struct {
		name string
		opt  Option
		want DelayConfig
	}{
		{
			name: "negative max delay becomes zero",
			opt:  WithMaxDelay(-time.Second),
			want: DelayConfig{BaseDelay: base, MaxJitter: base, MaxBackoffStep: autoStep},
		},
		{
			name: "negative max jitter becomes zero",
			opt:  WithMaxJitter(-time.Second),
			want: DelayConfig{BaseDelay: base, MaxBackoffStep: autoStep},
		},
		{
			name: "negative backoff step falls back to the derived step",
			opt:  WithBackoffStep(-1),
			want: DelayConfig{BaseDelay: base, MaxJitter: base, MaxBackoffStep: autoStep},
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
	cfg := DelayConfig{BaseDelay: 1 << 62}
	if got, want := ExponentialBackoff(1, nil, cfg), time.Duration(math.MaxInt64); got != want {
		t.Errorf("got %v, want MaxInt64", got)
	}
}

func TestCalculateMaxBackoffStep_UnrepresentableBase(t *testing.T) {
	if got := calculateMaxBackoffStep(time.Duration(math.MaxInt64)); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestWithBackoffStep_ClampedToBaseDelay(t *testing.T) {
	const base = time.Second
	r := NewRetrier(WithBaseDelay(base), WithBackoffStep(1000))
	if got, want := r.inner.strategy.delayConfig.MaxBackoffStep, calculateMaxBackoffStep(base); got != want {
		t.Errorf("MaxBackoffStep = %d, want %d", got, want)
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
