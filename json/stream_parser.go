package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode"
)

// StreamParserConfig configures a [StreamParser].
type StreamParserConfig struct {
	// BufferSize is the read-buffer size in bytes. Defaults to 4096.
	BufferSize int
	// MaxBufferSize caps, in bytes, how much the parser buffers for the
	// in-flight top-level value, counted across every open nesting scope.
	// Parsing stops with an error as soon as the cap would be exceeded, so
	// the parser stays safe on untrusted input (LLM output, network JSON).
	//
	// Zero selects the package default (16 MiB). A negative value disables
	// the cap entirely, which is only safe for trusted input.
	MaxBufferSize int
	// MaxDepth caps the nesting depth (objects plus arrays) of a top-level
	// value. Deeper input is rejected as soon as the next scope would open.
	//
	// Zero selects the package default (10000, the same limit encoding/json
	// enforces). A negative value disables the cap entirely, which is only
	// safe for trusted input.
	MaxDepth int
	// Reader is the source of JSON bytes. Required.
	Reader io.Reader
	// OnArray is called once per top-level JSON array.
	OnArray func([]any) error
	// OnObject is called once per top-level JSON object.
	OnObject func(map[string]any) error
	// OnValue is called once per top-level primitive value.
	OnValue func(any) error
	// OnError receives parser-detected errors before they are returned by
	// [StreamParser.Parse]: malformed JSON (unmatched or unexpected
	// brackets, unexpected EOF, failed unmarshal) and exceeded
	// MaxBufferSize / MaxDepth limits. Each such error is reported exactly
	// once. It never observes errors returned by the user's own OnArray /
	// OnObject / OnValue callbacks nor underlying reader (I/O) failures,
	// and it cannot resume parsing — the error is still returned.
	OnError func(error)
}

const defaultParserBufSize = 4096

// defaultParserMaxBufferSize is the default cap, in bytes, on how much the
// parser buffers for a single in-flight top-level value (16 MiB).
const defaultParserMaxBufferSize = 16 << 20

// defaultParserMaxDepth is the default nesting-depth cap (objects plus
// arrays); it mirrors the limit encoding/json enforces.
const defaultParserMaxDepth = 10000

// Validate reports configuration errors. Pure check — no mutation;
// pair with [StreamParserConfig.ApplyDefaults] to fill in defaults.
func (c *StreamParserConfig) Validate() error {
	if c.Reader == nil {
		return errors.New("json: reader required")
	}
	return nil
}

// ApplyDefaults fills unset fields with package defaults: BufferSize when it
// is zero or negative, and the MaxBufferSize / MaxDepth caps when they are
// exactly zero. Negative caps are preserved verbatim because they mean
// "explicitly unlimited" — that is why the caps cannot be defaulted on
// <= 0. Pointer receiver — call on the local copy inside the constructor;
// the caller's value is untouched.
func (c *StreamParserConfig) ApplyDefaults() {
	if c.BufferSize <= 0 {
		c.BufferSize = defaultParserBufSize
	}
	if c.MaxBufferSize == 0 {
		c.MaxBufferSize = defaultParserMaxBufferSize
	}
	if c.MaxDepth == 0 {
		c.MaxDepth = defaultParserMaxDepth
	}
}

// StreamParser parses a JSON stream incrementally and emits each
// top-level value through the configured callbacks. It is not safe
// for concurrent use; create one parser per stream.
//
// Example:
//
//	p, _ := json.NewStreamParser(json.StreamParserConfig{
//	    Reader: r,
//	    OnObject: func(o map[string]any) error { handle(o); return nil },
//	})
//	if err := p.Parse(); err != nil {
//	    return err
//	}
type StreamParser struct {
	bufferSize int
	// maxBufferSize is the byte cap for the in-flight top-level value;
	// <= 0 means unlimited.
	maxBufferSize int
	// maxDepth is the nesting-depth cap; <= 0 means unlimited.
	maxDepth int
	// buffered counts the bytes currently held for the in-flight top-level
	// value across topBuf and every buffer in buffers.
	buffered int
	scopes   []string
	reader   io.Reader
	buffers  []*bytes.Buffer
	topBuf   *bytes.Buffer
	inString bool
	escaped  bool
	pos      int
	onArray  func([]any) error
	onObject func(map[string]any) error
	onValue  func(any) error
	onError  func(error)
}

// NewStreamParser returns a parser configured by config.
func NewStreamParser(config StreamParserConfig) (*StreamParser, error) {
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &StreamParser{
		bufferSize:    config.BufferSize,
		maxBufferSize: config.MaxBufferSize,
		maxDepth:      config.MaxDepth,
		scopes:        make([]string, 0, 8),
		reader:        config.Reader,
		buffers:       make([]*bytes.Buffer, 0, 8),
		topBuf:        new(bytes.Buffer),
		onArray:       config.OnArray,
		onObject:      config.OnObject,
		onValue:       config.OnValue,
		onError:       config.OnError,
	}, nil
}

// Parse reads from the configured reader until io.EOF, dispatching
// each top-level value to the appropriate callback. It returns an
// error on read failure, callback failure, malformed JSON, or when a
// configured [StreamParserConfig.MaxBufferSize] /
// [StreamParserConfig.MaxDepth] limit is exceeded. Parser-detected
// errors are routed through OnError before they are returned.
func (p *StreamParser) Parse() error {
	buf := make([]byte, p.bufferSize)
	for {
		n, err := p.reader.Read(buf)
		if n > 0 {
			if procErr := p.processBytes(buf[:n]); procErr != nil {
				return procErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if flushErr := p.flushTopLevel(); flushErr != nil {
					return flushErr
				}
				if len(p.scopes) > 0 {
					return p.fail(fmt.Errorf("json: unexpected EOF: unclosed %s", p.scopes[len(p.scopes)-1]))
				}
				return nil
			}
			return fmt.Errorf("json: read: %w", err)
		}
	}
}

func (p *StreamParser) processBytes(data []byte) error {
	for _, b := range data {
		if err := p.processChar(b); err != nil {
			return err
		}
	}
	return nil
}

func (p *StreamParser) processChar(c byte) error {
	p.pos++
	if c == '"' && !p.escaped {
		p.inString = !p.inString
		return p.write(c)
	}
	if p.inString {
		if err := p.write(c); err != nil {
			return err
		}
		switch {
		case p.escaped:
			p.escaped = false
		case c == '\\':
			p.escaped = true
		}
		return nil
	}
	switch c {
	case '{':
		if err := p.startScope("object"); err != nil {
			return err
		}
		return p.write(c)
	case '}':
		if err := p.write(c); err != nil {
			return err
		}
		return p.endScope("object")
	case '[':
		if err := p.startScope("array"); err != nil {
			return err
		}
		return p.write(c)
	case ']':
		if err := p.write(c); err != nil {
			return err
		}
		return p.endScope("array")
	case ',':
		if len(p.scopes) == 0 {
			return p.flushTopLevel()
		}
		return p.write(c)
	default:
		if unicode.IsSpace(rune(c)) {
			if len(p.scopes) == 0 && p.topBuf.Len() > 0 && p.isCompletePrimitive() {
				return p.flushTopLevel()
			}
			if len(p.scopes) > 0 {
				return p.write(c)
			}
			return nil
		}
		return p.write(c)
	}
}

// isCompletePrimitive checks whether the top-level buffer parses as
// a JSON value. [json.Valid] scans without allocating, unlike the
// throwaway Unmarshal the dispatch path would otherwise duplicate.
func (p *StreamParser) isCompletePrimitive() bool {
	data := bytes.TrimSpace(p.topBuf.Bytes())
	if len(data) == 0 {
		return false
	}
	return json.Valid(data)
}

// flushTopLevel parses and dispatches a primitive top-level value.
func (p *StreamParser) flushTopLevel() error {
	data := bytes.TrimSpace(p.topBuf.Bytes())
	if len(data) == 0 {
		return nil
	}
	defer func() {
		p.topBuf.Reset()
		p.buffered = 0
	}()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return p.fail(fmt.Errorf("json: unmarshal top-level @%d: %w", p.pos, err))
	}
	if p.onValue != nil {
		if err := p.onValue(v); err != nil {
			return fmt.Errorf("json: onValue: %w", err)
		}
	}
	return nil
}

// write appends c to the current scope's buffer (or top-level if outside any
// scope), charging the byte to the in-flight top-level budget. It returns an
// error as soon as MaxBufferSize would be exceeded, before buffering
// anything more.
func (p *StreamParser) write(c byte) error {
	if p.maxBufferSize > 0 && p.buffered+1 > p.maxBufferSize {
		return p.fail(fmt.Errorf("json: buffer limit %d bytes exceeded @%d", p.maxBufferSize, p.pos))
	}
	p.buffered++
	if n := len(p.buffers); n > 0 {
		p.buffers[n-1].WriteByte(c)
		return nil
	}
	p.topBuf.WriteByte(c)
	return nil
}

// startScope pushes a new "object" or "array" scope after checking MaxDepth.
// It reports an error, without allocating anything, when the scope would
// exceed the configured nesting depth.
func (p *StreamParser) startScope(kind string) error {
	if p.maxDepth > 0 && len(p.scopes)+1 > p.maxDepth {
		return p.fail(fmt.Errorf("json: max depth %d exceeded @%d", p.maxDepth, p.pos))
	}
	if len(p.buffers) == 0 && p.topBuf.Len() > 0 {
		p.topBuf.Reset()
		p.buffered = 0
	}
	p.scopes = append(p.scopes, kind)
	p.buffers = append(p.buffers, new(bytes.Buffer))
	return nil
}

// endScope pops a scope, validates the matching delimiter, and
// dispatches the completed value when the stack returns to top level.
func (p *StreamParser) endScope(kind string) error {
	if len(p.scopes) == 0 {
		return p.fail(fmt.Errorf("json: unexpected '%s' @%d", closingChar(kind), p.pos))
	}
	cur := p.scopes[len(p.scopes)-1]
	if cur != kind {
		return p.fail(fmt.Errorf("json: mismatched brackets @%d: want '%s', got '%s'", p.pos, closingChar(cur), closingChar(kind)))
	}
	p.scopes = p.scopes[:len(p.scopes)-1]

	curBuf := p.buffers[len(p.buffers)-1]
	p.buffers = p.buffers[:len(p.buffers)-1]
	data := curBuf.Bytes()

	if len(p.buffers) == 0 {
		var err error
		switch kind {
		case "object":
			err = p.dispatchObject(data)
		case "array":
			err = p.dispatchArray(data)
		}
		curBuf.Reset()
		// The top-level value is done (dispatched or rejected): the
		// budget restarts for the next one.
		p.buffered = 0
		return err
	}
	p.buffers[len(p.buffers)-1].Write(data)
	curBuf.Reset()
	return nil
}

func (p *StreamParser) dispatchObject(data []byte) error {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return p.fail(fmt.Errorf("json: unmarshal object @%d: %w", p.pos, err))
	}
	if p.onObject != nil {
		if err := p.onObject(obj); err != nil {
			return fmt.Errorf("json: onObject: %w", err)
		}
	}
	return nil
}

func (p *StreamParser) dispatchArray(data []byte) error {
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return p.fail(fmt.Errorf("json: unmarshal array @%d: %w", p.pos, err))
	}
	if p.onArray != nil {
		if err := p.onArray(arr); err != nil {
			return fmt.Errorf("json: onArray: %w", err)
		}
	}
	return nil
}

func (p *StreamParser) notify(err error) {
	if p.onError != nil {
		p.onError(err)
	}
}

func closingChar(kind string) string {
	switch kind {
	case "object":
		return "}"
	case "array":
		return "]"
	}
	return ""
}

// fail reports err to OnError and returns it. Every parser-detected error —
// malformed JSON and limit violations — goes through this single path so
// OnError sees each one exactly once. Errors owned by the caller (callback
// returns) or by the reader are returned directly and never notified.
func (p *StreamParser) fail(err error) error {
	p.notify(err)
	return err
}
