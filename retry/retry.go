package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Operation is a retryable function with no return value.
type Operation func() error

// OperationWithResult is a retryable function that returns a value of type T.
type OperationWithResult[T any] func() (T, error)

// DelayConfig groups the parameters consumed by built-in delay
// functions ([ExponentialBackoff], [FullJitterBackoff], …).
type DelayConfig struct {
	// BaseDelay is the initial delay between retries.
	BaseDelay time.Duration
	// MaxDelay caps the computed delay; 0 means uncapped.
	MaxDelay time.Duration
	// MaxJitter is the upper bound of the random jitter added by
	// [RandomJitter]; 0 disables jitter.
	MaxJitter time.Duration
	// MaxBackoffStep caps the exponent used by exponential strategies.
	// 0 means no cap; the delay arithmetic saturates rather than overflowing.
	MaxBackoffStep int
}

// Strategy holds a fully resolved retry configuration. It is built by
// [NewRetrier] / [NewResultRetrier] from a list of [Option] values and
// is safe to share across goroutines as long as no further mutation
// occurs.
type Strategy struct {
	context      context.Context
	maxAttempts  int
	onRetry      func(attempt int, err error)
	onSuccess    func(attempt int)
	shouldRetry  func(err error) bool
	computeDelay func(attempt int, err error, config DelayConfig) time.Duration
	sleep        func(d time.Duration) <-chan time.Time
	delayConfig  DelayConfig
}

func defaultStrategy() *Strategy {
	return &Strategy{
		context:      context.Background(),
		maxAttempts:  3,
		onRetry:      func(int, error) {},
		onSuccess:    func(int) {},
		shouldRetry:  func(error) bool { return true },
		computeDelay: CombineDelays(ExponentialBackoff, RandomJitter),
		sleep:        time.After,
		delayConfig: DelayConfig{
			BaseDelay: 100 * time.Millisecond,
			MaxJitter: 100 * time.Millisecond,
		},
	}
}

// Option configures a [Strategy].
type Option func(*Strategy)

// WithContext sets the context that bounds the entire retry. Canceling
// it terminates retries with a wrapped context error. A nil context is
// ignored.
func WithContext(ctx context.Context) Option {
	return func(s *Strategy) {
		if ctx != nil {
			s.context = ctx
		}
	}
}

// WithMaxAttempts caps the total number of attempts. Use 0 (or
// [WithUnlimitedAttempts]) for unlimited retries; pair with a context
// deadline. Negative values are treated as 0.
func WithMaxAttempts(n int) Option {
	return func(s *Strategy) {
		if n < 0 {
			n = 0
		}
		s.maxAttempts = n
	}
}

// WithUnlimitedAttempts is shorthand for WithMaxAttempts(0).
func WithUnlimitedAttempts() Option { return WithMaxAttempts(0) }

// WithOnRetry installs a callback fired before each delay, after a
// failed attempt. attempt starts at 1.
func WithOnRetry(fn func(attempt int, err error)) Option {
	return func(s *Strategy) {
		if fn != nil {
			s.onRetry = fn
		}
	}
}

// WithOnSuccess installs a callback fired once the operation succeeds.
// attempt is the 1-based attempt that succeeded.
func WithOnSuccess(fn func(attempt int)) Option {
	return func(s *Strategy) {
		if fn != nil {
			s.onSuccess = fn
		}
	}
}

// WithRetryCondition sets a predicate deciding whether to retry on a
// given error. Return false to stop retrying immediately.
func WithRetryCondition(fn func(err error) bool) Option {
	return func(s *Strategy) {
		if fn != nil {
			s.shouldRetry = fn
		}
	}
}

// WithDelayFunc sets a custom delay function. See [ExponentialBackoff]
// and [CombineDelays] for building blocks.
func WithDelayFunc(fn func(attempt int, err error, cfg DelayConfig) time.Duration) Option {
	return func(s *Strategy) {
		if fn != nil {
			s.computeDelay = fn
		}
	}
}

// WithSleep replaces the underlying sleep function. Tests can pass a
// mock that returns immediately to avoid real waiting.
func WithSleep(fn func(d time.Duration) <-chan time.Time) Option {
	return func(s *Strategy) {
		if fn != nil {
			s.sleep = fn
		}
	}
}

// WithBaseDelay sets the initial delay. Negative values become 0.
func WithBaseDelay(d time.Duration) Option {
	return func(s *Strategy) {
		if d < 0 {
			d = 0
		}
		s.delayConfig.BaseDelay = d
	}
}

// WithMaxDelay caps the per-attempt delay. 0 disables the cap.
func WithMaxDelay(d time.Duration) Option {
	return func(s *Strategy) {
		if d < 0 {
			d = 0
		}
		s.delayConfig.MaxDelay = d
	}
}

// WithMaxJitter sets the upper bound on random jitter for
// [RandomJitter]. Negative values become 0.
func WithMaxJitter(d time.Duration) Option {
	return func(s *Strategy) {
		if d < 0 {
			d = 0
		}
		s.delayConfig.MaxJitter = d
	}
}

// WithBackoffStep caps the exponent in exponential strategies.
// 0 or a negative value removes the cap.
func WithBackoffStep(step int) Option {
	return func(s *Strategy) {
		if step < 0 {
			step = 0
		}
		s.delayConfig.MaxBackoffStep = step
	}
}

// WithDelayConfig replaces the entire [DelayConfig]. Negative fields
// are normalized to 0.
func WithDelayConfig(cfg DelayConfig) Option {
	return func(s *Strategy) {
		if cfg.BaseDelay < 0 {
			cfg.BaseDelay = 0
		}
		if cfg.MaxDelay < 0 {
			cfg.MaxDelay = 0
		}
		if cfg.MaxJitter < 0 {
			cfg.MaxJitter = 0
		}
		if cfg.MaxBackoffStep < 0 {
			cfg.MaxBackoffStep = 0
		}
		s.delayConfig = cfg
	}
}

// WithExponentialBackoff selects exponential backoff plus random
// jitter, the package default.
func WithExponentialBackoff() Option {
	return WithDelayFunc(CombineDelays(ExponentialBackoff, RandomJitter))
}

// WithFixedDelay selects a constant delay equal to BaseDelay.
func WithFixedDelay() Option { return WithDelayFunc(FixedDelay) }

// WithFullJitter selects AWS-style full jitter:
// random(0, min(MaxDelay, BaseDelay·2^attempt)).
//
// Reference: https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/
func WithFullJitter() Option { return WithDelayFunc(FullJitterBackoff) }

// ExponentialBackoff returns BaseDelay·2^attempt, capped by MaxDelay
// and MaxBackoffStep, saturating at math.MaxInt64 on overflow.
func ExponentialBackoff(attempt int, _ error, cfg DelayConfig) time.Duration {
	if attempt <= 0 || cfg.BaseDelay <= 0 {
		return cfg.BaseDelay
	}
	d := shiftSaturating(cfg.BaseDelay, cappedStep(attempt, cfg.MaxBackoffStep))
	if cfg.MaxDelay > 0 && d > cfg.MaxDelay {
		d = cfg.MaxDelay
	}
	return d
}

// FixedDelay always returns cfg.BaseDelay.
func FixedDelay(_ int, _ error, cfg DelayConfig) time.Duration {
	return cfg.BaseDelay
}

// randInt64N is the jitter source for [RandomJitter] and
// [FullJitterBackoff]. It is a variable so tests can substitute a
// deterministic sequence: jitter is the only nondeterministic input to
// delay computation, and math/rand/v2 exposes no seedable global source.
var randInt64N = rand.Int64N

// RandomJitter returns a uniform random duration in [0, MaxJitter).
// Returns 0 if MaxJitter <= 0.
func RandomJitter(_ int, _ error, cfg DelayConfig) time.Duration {
	if cfg.MaxJitter <= 0 {
		return 0
	}
	return time.Duration(randInt64N(int64(cfg.MaxJitter)))
}

// FullJitterBackoff returns a uniform random duration in
// [0, min(MaxDelay, BaseDelay·2^attempt)).
func FullJitterBackoff(attempt int, _ error, cfg DelayConfig) time.Duration {
	if attempt <= 0 || cfg.BaseDelay <= 0 {
		return 0
	}
	ceiling := shiftSaturating(cfg.BaseDelay, cappedStep(attempt, cfg.MaxBackoffStep))
	if cfg.MaxDelay > 0 && ceiling > cfg.MaxDelay {
		ceiling = cfg.MaxDelay
	}
	return time.Duration(randInt64N(int64(ceiling)))
}

// cappedStep returns attempt, capped at maxStep when maxStep is positive. This
// is the only place that decides what MaxBackoffStep == 0 means, so the
// "uncapped" reading cannot drift apart from the delay functions using it.
func cappedStep(attempt, maxStep int) int {
	if maxStep > 0 && attempt > maxStep {
		return maxStep
	}
	return attempt
}

// shiftSaturating returns base<<step, saturating at math.MaxInt64 instead of
// wrapping. base must be positive and step at least 1, which the callers
// guarantee; the result is then always at least 2, and [FullJitterBackoff]
// relies on that to keep a zero ceiling away from rand.Int64N, which panics on
// a zero argument.
func shiftSaturating(base time.Duration, step int) time.Duration {
	if base > time.Duration(math.MaxInt64)>>step {
		return time.Duration(math.MaxInt64)
	}
	return base << step
}

// addSaturating returns a+b, saturating at the int64 bounds instead of
// wrapping. Combined delay components need not be positive, so both directions
// have to be detected: testing against math.MaxInt64 alone reports a spurious
// MaxInt64 as soon as a component is negative.
func addSaturating(a, b time.Duration) time.Duration {
	sum := a + b
	if b > 0 && sum < a {
		return time.Duration(math.MaxInt64)
	}
	if b < 0 && sum > a {
		return time.Duration(math.MinInt64)
	}
	return sum
}

// CombineDelays returns a delay function whose result is the sum of
// the supplied functions, saturating instead of wrapping on overflow.
//
// Example:
//
//	retry.CombineDelays(retry.ExponentialBackoff, retry.RandomJitter)
func CombineDelays(funcs ...func(int, error, DelayConfig) time.Duration) func(int, error, DelayConfig) time.Duration {
	return func(attempt int, err error, cfg DelayConfig) time.Duration {
		var total time.Duration
		for _, fn := range funcs {
			total = addSaturating(total, fn(attempt, err, cfg))
		}
		return total
	}
}

func doRetry[T any](op OperationWithResult[T], s *Strategy) (T, error) {
	var zero T
	if err := s.context.Err(); err != nil {
		return zero, fmt.Errorf("context canceled before first attempt: %w", err)
	}
	for attempt := 1; ; attempt++ {
		v, err := op()
		if err == nil {
			s.onSuccess(attempt)
			return v, nil
		}
		if !s.shouldRetry(err) {
			return zero, fmt.Errorf("operation failed after %d attempts (aborted by retry condition): %w", attempt, err)
		}
		if s.maxAttempts > 0 && attempt >= s.maxAttempts {
			return zero, fmt.Errorf("operation failed after %d attempts (max attempts reached): %w", attempt, err)
		}
		s.onRetry(attempt, err)
		select {
		case <-s.sleep(s.computeDelay(attempt, err, s.delayConfig)):
		case <-s.context.Done():
			ctxErr := s.context.Err()
			if s.maxAttempts == 0 {
				return zero, fmt.Errorf("operation canceled after %d attempts (unlimited retry mode): %w (last error: %v)",
					attempt, ctxErr, err)
			}
			return zero, fmt.Errorf("operation canceled after %d attempts: %w (last error: %v)",
				attempt, ctxErr, err)
		}
	}
}

// ResultRetrier executes [OperationWithResult] values with a fixed
// configuration. It is safe for concurrent use.
type ResultRetrier[T any] struct {
	strategy *Strategy
}

// NewResultRetrier builds a [ResultRetrier] from the supplied options.
func NewResultRetrier[T any](opts ...Option) *ResultRetrier[T] {
	s := defaultStrategy()
	for _, opt := range opts {
		opt(s)
	}
	return &ResultRetrier[T]{strategy: s}
}

// Do runs op with the configured retry policy.
func (r *ResultRetrier[T]) Do(op OperationWithResult[T]) (T, error) {
	return doRetry(op, r.strategy)
}

// Retrier executes [Operation] values with a fixed configuration.
type Retrier struct {
	inner *ResultRetrier[any]
}

// NewRetrier builds a [Retrier] from the supplied options.
func NewRetrier(opts ...Option) *Retrier {
	return &Retrier{inner: NewResultRetrier[any](opts...)}
}

// Do runs op with the configured retry policy.
func (r *Retrier) Do(op Operation) error {
	_, err := r.inner.Do(func() (any, error) { return nil, op() })
	return err
}

// Do is shorthand for NewRetrier(opts...).Do(op).
func Do(op Operation, opts ...Option) error {
	return NewRetrier(opts...).Do(op)
}

// DoWithResult is shorthand for NewResultRetrier[T](opts...).Do(op).
func DoWithResult[T any](op OperationWithResult[T], opts ...Option) (T, error) {
	return NewResultRetrier[T](opts...).Do(op)
}
