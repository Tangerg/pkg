package stream

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	pkgSlices "github.com/Tangerg/pkg/slices"
)

// ErrStreamClosed is returned by [Writer.Write] and [io.Closer.Close]
// when the stream is already closed.
var ErrStreamClosed = errors.New("stream: already closed")

// Reader reads values of type T from a stream.
type Reader[T any] interface {
	// Read blocks until a value is available, ctx is done, or the
	// stream is closed. It returns io.EOF when the stream is closed
	// and drained.
	Read(ctx context.Context) (T, error)
}

// Writer writes values of type T to a stream.
type Writer[T any] interface {
	// Write blocks until the value is accepted, ctx is done, or the
	// stream is closed. It returns [ErrStreamClosed] for writes to a
	// closed stream.
	Write(ctx context.Context, v T) error
}

// Stream is a bidirectional, closeable channel-backed stream.
type Stream[T any] interface {
	Reader[T]
	Writer[T]
	io.Closer
}

// stream is the channel-based implementation of [Stream]. The value
// channel is never closed: [stream.Close] signals completion through
// done instead, so a writer blocked on a full channel is released rather
// than wedging Close and, with it, every later write and read.
type stream[T any] struct {
	value  chan T
	done   chan struct{}
	closed atomic.Bool
}

// Read implements [Reader.Read]. Once the stream is closed it still
// hands over the values that were already accepted and reports io.EOF
// only after they are drained.
func (s *stream[T]) Read(ctx context.Context) (v T, err error) {
	select {
	case <-ctx.Done():
		return v, ctx.Err()
	case val := <-s.value:
		return val, nil
	case <-s.done:
		// Closed: hand over whatever is still buffered before EOF.
		select {
		case val := <-s.value:
			return val, nil
		default:
			return v, io.EOF
		}
	}
}

// Write implements [Writer.Write]. A write that is already blocked when
// [stream.Close] runs is released with [ErrStreamClosed], which is what
// lets Close return without waiting for writers and keeps in-flight
// writes from racing a channel close.
func (s *stream[T]) Write(ctx context.Context, v T) error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return ErrStreamClosed
	case s.value <- v:
		return nil
	}
}

// Close marks the stream closed and releases every blocked reader and
// writer. Subsequent calls return [ErrStreamClosed].
//
// Close returns promptly even when a writer is blocked on a full channel:
// that writer fails with [ErrStreamClosed], values already accepted stay
// readable until they are drained (then io.EOF), and no goroutine is left
// blocked. A write that was already in flight may still land after Close;
// every write started after Close returned fails with [ErrStreamClosed].
func (s *stream[T]) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return ErrStreamClosed
	}
	close(s.done)
	return nil
}

// IsClosed reports whether [Close] has been called. Callers should
// still handle [ErrStreamClosed] from Write rather than relying on
// this snapshot.
func (s *stream[T]) IsClosed() bool {
	return s.closed.Load()
}

// NewStream returns a new [Stream] with optional buffer size. Only the
// first size is used; negative values become 0 (unbuffered).
//
// Example:
//
//	s := stream.NewStream[int](32)
//	defer s.Close()
//	go producer(s)
//	for {
//	    v, err := s.Read(ctx)
//	    if errors.Is(err, io.EOF) {
//	        return
//	    }
//	    handle(v)
//	}
func NewStream[T any](sizes ...int) Stream[T] {
	size, _ := pkgSlices.First(sizes)
	if size < 0 {
		size = 0
	}
	return &stream[T]{
		value: make(chan T, size),
		done:  make(chan struct{}),
	}
}
