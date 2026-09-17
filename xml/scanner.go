package xml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type interruptError struct {
	inner error
}

func (e *interruptError) Error() string {
	return e.inner.Error()
}
func (e *interruptError) Unwrap() error {
	return e.inner
}

// ElementListener describes one element to track. MaxBufferSize bounds what the
// scanner may buffer for it while it is open.
type ElementListener struct {
	Name       Name
	OnComplete func(Element) error
	// MaxBufferSize is the enforced upper bound, in bytes, on the buffered
	// representation of this element while it is being parsed. The buffer never
	// grows past the limit: any write that would reach it — content, the text
	// of a nested element, or the closing tag — is dropped instead of appended,
	// and elements no listener tracks share this limit. In StrictMode the scan
	// aborts with an "element buffer overflow" error; in non-strict mode the
	// overflow is reported through OnError and parsing continues with the
	// truncated element. Zero or negative falls back to the default applied by
	// [StreamScannerConfig.ApplyDefaults].
	MaxBufferSize int
	EmitAlways    bool
}

type elementScope struct {
	element  Element
	listener *ElementListener
	// limit is the effective cap, in bytes, on this scope's buffer: the
	// MaxBufferSize its own listener declares, or, for an element no listener
	// tracks, the limit inherited from the enclosing scope — everything
	// buffered here is copied into the enclosing buffer when the scope closes.
	// 0 means unlimited.
	limit int
	// overflow is set once an overflow has been reported, so a scope reports at
	// most one error however many writes it refuses.
	overflow bool
}

// appendCharData appends to the scope's element, merging into a trailing
// CharData so consecutive text stays a single run.
func (s *elementScope) appendCharData(chars CharData) {
	if len(s.element.Contents) == 0 {
		s.element.Contents = append(s.element.Contents, chars)
		return
	}

	lastContent := s.element.Contents[len(s.element.Contents)-1]
	switch typed := lastContent.(type) {
	case CharData:
		typed = append(typed, chars...)
		s.element.Contents[len(s.element.Contents)-1] = typed
	case Element:
		s.element.Contents = append(s.element.Contents, chars)
	}
}

func (s *elementScope) appendElement(element Element) {
	s.element.Contents = append(s.element.Contents, element)
}

// elementStack holds the open nesting scopes with a buffer per scope; scope and
// buffer are parallel slices of the same depth.
type elementStack struct {
	length int
	scope  []*elementScope
	buffer []*bytes.Buffer
}

func newStack() *elementStack {
	return &elementStack{
		scope:  make([]*elementScope, 0, 8),
		buffer: make([]*bytes.Buffer, 0, 8),
	}
}

func (s *elementStack) reset() {
	s.length = 0
	s.scope = s.scope[:0]
	s.buffer = s.buffer[:0]
}

func (s *elementStack) push(scope *elementScope, buffer *bytes.Buffer) {
	s.scope = append(s.scope, scope)
	s.buffer = append(s.buffer, buffer)
	s.length++
}

func (s *elementStack) pop() (*elementScope, *bytes.Buffer) {
	if s.length == 0 {
		return nil, nil
	}

	lastScope := s.scope[len(s.scope)-1]
	lastBuffer := s.buffer[len(s.buffer)-1]

	s.length--
	s.scope = s.scope[:s.length]
	s.buffer = s.buffer[:s.length]

	return lastScope, lastBuffer
}

func (s *elementStack) last() (*elementScope, *bytes.Buffer) {
	if s.length == 0 {
		return nil, nil
	}
	return s.scope[s.length-1], s.buffer[s.length-1]
}

func (s *elementStack) len() int {
	return s.length
}

func (s *elementStack) string() string {
	sb := new(strings.Builder)
	for _, buffer := range s.buffer {
		sb.WriteString(buffer.String())
	}
	return sb.String()
}

// buffers holds the in-progress element tag and the out-of-element text.
type buffers struct {
	element *bytes.Buffer
	text    *bytes.Buffer
}

func newBuffers(size int) *buffers {
	return &buffers{
		element: bytes.NewBuffer(make([]byte, 0)),
		text:    bytes.NewBuffer(make([]byte, 0, size)),
	}
}

func (b *buffers) reset() {
	b.element.Reset()
	b.text.Reset()
}

// elementState is the byte-level state of the tag currently being read.
type elementState struct {
	inElement bool // between '<' and '>'
	inName    bool // within the element name
	inAttrs   bool // within the attribute section
	inString  bool // within a quoted attribute value
	quoteChar byte // the opening quote of that value
}

func newElementState() *elementState {
	return &elementState{}
}

func (s *elementState) reset() {
	s.inElement = false
	s.inName = false
	s.inAttrs = false
	s.inString = false
	s.quoteChar = 0
}

// StreamScannerConfig configures the behavior of StreamScanner.
type StreamScannerConfig struct {
	Listeners       []*ElementListener
	OnText          func(string) error
	MaxNestingLevel int
	BufferSize      int
	// MaxTextBufferSize caps, in bytes, the text buffered outside tracked
	// elements — including a stray closing tag and the unclosed remainder
	// flushed at the end of the scan. 0 applies the default of 1 MiB (1<<20); a
	// negative value disables the cap. On overflow the excess is dropped: in
	// strict mode the scan aborts with an error containing "text buffer
	// overflow", in non-strict mode the overflow is reported through OnError
	// and parsing continues with the truncated text.
	MaxTextBufferSize int
	// StrictMode makes structural errors terminate parsing; otherwise they are
	// treated as text.
	StrictMode bool
	OnError    func(error)
}

const defaultMaxTextBufferSize = 1 << 20

// ApplyDefaults fills zero / negative fields with package defaults.
// MaxTextBufferSize is only filled when zero, because a negative value is
// meaningful: it disables the out-of-element text cap.
func (c *StreamScannerConfig) ApplyDefaults() {
	if c.MaxNestingLevel <= 0 {
		c.MaxNestingLevel = 256
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 4096
	}
	if c.MaxTextBufferSize == 0 {
		c.MaxTextBufferSize = defaultMaxTextBufferSize
	}
	for i := range c.Listeners {
		if c.Listeners[i].MaxBufferSize <= 0 {
			c.Listeners[i].MaxBufferSize = 1024
		}
	}
}

// Validate checks the configuration. Pure check — pair with
// [StreamScannerConfig.ApplyDefaults].
func (c *StreamScannerConfig) Validate() error {
	if len(c.Listeners) == 0 {
		return errors.New("at least one listener is required")
	}
	eleNames := make(map[Name]bool)
	for _, listener := range c.Listeners {
		if listener.Name.String() == "" {
			return errors.New("listener element name must not be empty")
		}
		if eleNames[listener.Name] {
			return fmt.Errorf("duplicate listener for element %q", listener.Name)
		}
		eleNames[listener.Name] = true
	}
	return nil
}

// StreamScanner performs streaming XML parsing with selective element tracking.
type StreamScanner struct {
	bufferSize      int
	maxNestingLevel int
	maxTextSize     int
	// maxTagSize caps the scratch buffer holding the tag currently being read.
	// Nothing else bounds that scratch, so an unterminated tag could otherwise
	// grow with the input; it is the largest cap the caller configured, because
	// a tag that fits in no configured buffer cannot become part of one.
	maxTagSize     int
	textOverflowed bool
	tagOverflowed  bool
	listeners      map[Name]*ElementListener
	onText         func(string) error
	strictMode     bool
	onError        func(error)
	pos            int
	stack          *elementStack
	buffers        *buffers
	elementState   *elementState
}

// NewStreamScanner creates a new stream scanner with the given configuration.
func NewStreamScanner(config StreamScannerConfig) (*StreamScanner, error) {
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	maxTagSize := 0
	if config.MaxTextBufferSize > 0 {
		maxTagSize = config.MaxTextBufferSize
	}
	for _, listener := range config.Listeners {
		if listener.MaxBufferSize > maxTagSize {
			maxTagSize = listener.MaxBufferSize
		}
	}

	scanner := &StreamScanner{
		bufferSize:      config.BufferSize,
		maxNestingLevel: config.MaxNestingLevel,
		maxTextSize:     config.MaxTextBufferSize,
		maxTagSize:      maxTagSize,
		listeners:       make(map[Name]*ElementListener),
		onText:          config.OnText,
		strictMode:      config.StrictMode,
		onError:         config.OnError,
		stack:           newStack(),
		buffers:         newBuffers(config.BufferSize),
		elementState:    newElementState(),
	}

	for _, listener := range config.Listeners {
		scanner.listeners[listener.Name] = listener
	}

	return scanner, nil
}

// Scan processes XML data from the reader until io.EOF. Bytes that the reader
// returns together with io.EOF are processed before the scan is finalized.
func (p *StreamScanner) Scan(reader io.Reader) error {
	defer p.Reset()
	p.Reset()

	buf := make([]byte, p.bufferSize)
	for {
		n, err := reader.Read(buf)
		// io.Reader may return data together with io.EOF, so the bytes read
		// are processed before the error is handled.
		if n > 0 {
			if processErr := p.processChunk(buf[:n]); processErr != nil {
				return processErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return p.finalize()
			}
			return fmt.Errorf("read error: %w", err)
		}
	}
}

// Reset clears the scanner state for reuse.
func (p *StreamScanner) Reset() {
	p.stack.reset()
	p.buffers.reset()
	p.elementState.reset()
	p.textOverflowed = false
	p.tagOverflowed = false
	p.pos = 0
}

func (p *StreamScanner) flushText() error {
	if p.buffers.text.Len() == 0 {
		return nil
	}

	if p.onText != nil {
		if err := p.onText(p.buffers.text.String()); err != nil {
			return &interruptError{
				inner: fmt.Errorf("OnText callback: %w", err),
			}
		}
	}

	p.buffers.text.Reset()
	p.textOverflowed = false
	return nil
}

func (p *StreamScanner) finalize() error {
	if err := p.flushText(); err != nil {
		return err
	}

	unClosed := p.stack.string()
	if unClosed != "" {
		if p.strictMode {
			return fmt.Errorf("%w, unclosed element: %s", io.ErrUnexpectedEOF, unClosed)
		}
		// The unclosed remainder is reported as out-of-element text, so it is
		// charged against the text cap; a refused write is dropped and
		// reported, and the text buffered so far is still flushed.
		if err := p.appendText(unClosed); err != nil {
			p.logError(err)
		}
	}

	return p.flushText()
}

func (p *StreamScanner) processChunk(data []byte) error {
	for _, b := range data {
		p.pos++
		if err := p.processByte(b); err != nil {
			if p.strictMode {
				return err
			}
			// An interruptError stays fatal even in non-strict mode.
			if _, ok := errors.AsType[*interruptError](err); ok {
				return err
			}
			p.logError(err)
		}
	}
	return nil
}

func (p *StreamScanner) processByte(b byte) error {
	if (b == '"' || b == '\'') && p.elementState.inElement {
		if !p.elementState.inString {
			p.elementState.inString = true
			p.elementState.quoteChar = b
		} else if b == p.elementState.quoteChar {
			p.elementState.inString = false
			p.elementState.quoteChar = 0
		}
		return p.writeTagByte(b)
	}

	if (b == '"' || b == '\'') && !p.elementState.inElement {
		return p.writeToCurrentBuffer(b)
	}

	if p.elementState.inString {
		return p.writeTagByte(b)
	}

	if unicode.IsSpace(rune(b)) {
		return p.handleWhitespace(b)
	}

	switch b {
	case '<':
		return p.handleElementOpen(b)
	case '>':
		return p.handleElementClose(b)
	default:
		return p.handleRegularChar(b)
	}
}

func (p *StreamScanner) isInScope() bool {
	return p.stack.len() > 0
}

// scopeFull reports whether writing add more bytes into buffer would reach the
// effective cap of scope. A non-positive cap is disabled.
func scopeFull(scope *elementScope, buffer *bytes.Buffer, add int) bool {
	return scope.limit > 0 && buffer.Len()+add >= scope.limit
}

// textBufferFull reports whether writing add more bytes to the out-of-element
// text buffer would reach maxTextSize. A non-positive cap is disabled.
func (p *StreamScanner) textBufferFull(add int) bool {
	if p.maxTextSize <= 0 {
		return false
	}
	return p.buffers.text.Len()+add >= p.maxTextSize
}

// overflowError returns err wrapped in an [interruptError] when the scanner is
// in strict mode, so that the scan aborts; in non-strict mode err is returned
// unchanged for reporting through OnError.
func (p *StreamScanner) overflowError(err error) error {
	if p.strictMode {
		return &interruptError{inner: err}
	}
	return err
}

// refuseScopeWrite drops a write of add bytes refused by scope's buffer cap and
// reports it. Only the first refusal of a scope reports, so untrusted input
// cannot flood OnError with one error per dropped byte.
func (p *StreamScanner) refuseScopeWrite(scope *elementScope, buffer *bytes.Buffer, add int) error {
	if scope.overflow {
		return nil
	}
	scope.overflow = true
	err := fmt.Errorf("element buffer overflow: limit %d bytes, current %d bytes", scope.limit, buffer.Len()+add)
	return p.overflowError(err)
}

// refuseTextWrite drops a write of add bytes refused by the out-of-element text
// cap and reports it. Only the first refusal of a buffered text region reports.
// The reported error is EOF-flavoured, because the text that would follow it
// has been truncated away.
func (p *StreamScanner) refuseTextWrite(add int) error {
	if p.textOverflowed {
		return nil
	}
	p.textOverflowed = true
	err := fmt.Errorf("%w: text buffer overflow: limit %d bytes, current %d bytes",
		io.ErrUnexpectedEOF, p.maxTextSize, p.buffers.text.Len()+add)
	return p.overflowError(err)
}

// refuseTagWrite drops a write of add bytes refused by the tag cap and reports
// it. Only the first refusal reports: every following byte of the tag is
// dropped too, so reporting each one would flood OnError.
func (p *StreamScanner) refuseTagWrite(add int) error {
	if p.tagOverflowed {
		return nil
	}
	p.tagOverflowed = true
	err := fmt.Errorf("tag buffer overflow: limit %d bytes, current %d bytes", p.maxTagSize, p.buffers.element.Len()+add)
	return p.overflowError(err)
}

func (p *StreamScanner) appendScope(scope *elementScope, buffer *bytes.Buffer, s string) error {
	if scopeFull(scope, buffer, len(s)) {
		return p.refuseScopeWrite(scope, buffer, len(s))
	}
	buffer.WriteString(s)
	return nil
}

func (p *StreamScanner) appendText(s string) error {
	if p.textBufferFull(len(s)) {
		return p.refuseTextWrite(len(s))
	}
	p.buffers.text.WriteString(s)
	return nil
}

func (p *StreamScanner) writeTagByte(b byte) error {
	if p.maxTagSize > 0 && p.buffers.element.Len()+1 >= p.maxTagSize {
		return p.refuseTagWrite(1)
	}
	p.buffers.element.WriteByte(b)
	return nil
}

func (p *StreamScanner) writeToCurrentBuffer(b byte) error {
	if scope, buffer := p.stack.last(); scope != nil {
		if scopeFull(scope, buffer, 1) {
			return p.refuseScopeWrite(scope, buffer, 1)
		}
		scope.appendCharData(CharData{b})
		buffer.WriteByte(b)
		return nil
	}
	if p.textBufferFull(1) {
		return p.refuseTextWrite(1)
	}
	p.buffers.text.WriteByte(b)
	return nil
}

func (p *StreamScanner) writeStringToCurrentBuffer(s string) error {
	if scope, buffer := p.stack.last(); scope != nil {
		if scopeFull(scope, buffer, len(s)) {
			return p.refuseScopeWrite(scope, buffer, len(s))
		}
		scope.appendCharData(CharData(s))
		buffer.WriteString(s)
		return nil
	}
	return p.appendText(s)
}

func (p *StreamScanner) handleWhitespace(b byte) error {
	if p.elementState.inElement {
		if err := p.writeTagByte(b); err != nil {
			return err
		}
		if p.elementState.inName {
			p.elementState.inName = false
			p.elementState.inAttrs = true
		}
		return nil
	}

	return p.writeToCurrentBuffer(b)
}

func (p *StreamScanner) handleElementOpen(b byte) error {
	if p.elementState.inElement {
		return p.writeTagByte(b)
	}

	if !p.isInScope() {
		if err := p.flushText(); err != nil {
			return err
		}
	}

	p.elementState.reset()
	p.elementState.inElement = true
	p.buffers.element.Reset()

	return p.writeTagByte(b)
}

func (p *StreamScanner) handleElementClose(b byte) error {
	if !p.elementState.inElement {
		return p.writeToCurrentBuffer(b)
	}

	if err := p.writeTagByte(b); err != nil {
		return err
	}

	if p.elementState.inString {
		return nil
	}

	eleContent := p.buffers.element.String()

	if !isValidElementSyntax(eleContent) {
		err := p.writeStringToCurrentBuffer(eleContent)
		if err != nil {
			return err
		}
		p.elementState.reset()
		return nil
	}

	p.elementState.reset()

	switch {
	case strings.HasPrefix(eleContent, "</"):
		return p.processCloseElement(eleContent)
	case strings.HasSuffix(eleContent, "/>"):
		return p.processSelfCloseElement(eleContent)
	default:
		return p.processOpenElement(eleContent)
	}
}

func (p *StreamScanner) handleRegularChar(b byte) error {
	if p.elementState.inElement {
		if err := p.writeTagByte(b); err != nil {
			return err
		}

		if !p.elementState.inName && !p.elementState.inAttrs && b != '/' {
			p.elementState.inName = true
			p.elementState.inAttrs = false
		}

		return nil
	}
	return p.writeToCurrentBuffer(b)
}

func (p *StreamScanner) logError(err error) {
	if p.onError != nil {
		p.onError(err)
	}
}

func (p *StreamScanner) processOpenElement(eleContent string) error {
	eleName := extractElementName(eleContent)

	listener, isTracked := p.listeners[eleName]
	if !isTracked && !p.isInScope() {
		return p.writeStringToCurrentBuffer(eleContent)
	}

	if p.stack.len() >= p.maxNestingLevel {
		err := errors.Join(
			p.writeStringToCurrentBuffer(eleContent),
			fmt.Errorf("nesting level %d exceeds maximum %d at position %d",
				p.stack.len()+1, p.maxNestingLevel, p.pos),
		)
		if p.strictMode {
			return &interruptError{
				inner: err,
			}
		}
		return err
	}

	attrs, err := parseAttrs(eleContent)
	if err != nil {
		return errors.Join(
			p.writeStringToCurrentBuffer(eleContent),
			fmt.Errorf("failed to parse attributes for <%s> at position %d: %w",
				eleName, p.pos, err),
		)
	}

	return p.pushScope(eleName, attrs, eleContent, listener)
}

func (p *StreamScanner) processCloseElement(eleContent string) error {
	eleName := extractElementName(eleContent)

	_, isTracked := p.listeners[eleName]
	if !isTracked && !p.isInScope() {
		return p.writeStringToCurrentBuffer(eleContent)
	}

	return p.popScope(eleName, eleContent)
}

func (p *StreamScanner) processSelfCloseElement(eleContent string) error {
	eleName := extractElementName(eleContent)

	_, isTracked := p.listeners[eleName]
	if !isTracked && !p.isInScope() {
		return p.writeStringToCurrentBuffer(eleContent)
	}

	openEle, closeEle := expandSelfCloseElement(eleContent, eleName.String())

	if err := p.processOpenElement(openEle); err != nil {
		return err
	}

	return p.processCloseElement(closeEle)
}

func (p *StreamScanner) pushScope(eleName Name, attrs []Attr, openEle string, listener *ElementListener) error {
	scope := &elementScope{
		element: Element{
			Start: StartElement{
				Name:  eleName,
				Attrs: attrs,
			},
		},
		listener: listener,
		limit:    p.scopeLimit(listener),
	}
	scopeBuffer := bytes.NewBuffer(make([]byte, 0, len(openEle)*2))
	p.stack.push(scope, scopeBuffer)

	return p.appendScope(scope, scopeBuffer, openEle)
}

// scopeLimit resolves the cap of the scope being pushed: what its own listener
// declares, or the limit the enclosing scope is already bound by when no
// listener tracks the element.
func (p *StreamScanner) scopeLimit(listener *ElementListener) int {
	if listener != nil && listener.MaxBufferSize > 0 {
		return listener.MaxBufferSize
	}
	if enclosing, _ := p.stack.last(); enclosing != nil {
		return enclosing.limit
	}
	return 0
}

func (p *StreamScanner) popScope(eleName Name, closeEle string) error {
	currentScope, currentBuffer := p.stack.last()
	if currentScope == nil {
		return p.appendText(closeEle)
	}

	if currentScope.element.Start.Name != eleName {
		writeErr := p.writeStringToCurrentBuffer(closeEle)
		err := fmt.Errorf("mismatched closing element at position %d: expected </%s>, got </%s>",
			p.pos, currentScope.element.Start.Name, eleName)
		return errors.Join(writeErr, err)
	}

	p.stack.pop()
	// The closing tag and the copy handed to the enclosing scope are part of
	// the buffered representation, so they are charged against the caps too. In
	// non-strict mode a refused write is dropped but the element still
	// completes, so the document around it keeps its structure; in strict mode
	// it aborts the scan.
	var refused error
	if err := p.appendScope(currentScope, currentBuffer, closeEle); err != nil {
		if p.strictMode {
			return err
		}
		refused = err
	}
	fullEle := currentBuffer.String()

	element := Element{
		Start:    currentScope.element.Start,
		Contents: currentScope.element.Contents,
		End:      currentScope.element.Start.End(),
	}

	if len(element.Contents) == 0 {
		textContent := extractElementContent(fullEle, currentScope.element.Start.Name.String())
		if len(textContent) > 0 {
			element.Contents = []Content{CharData(textContent)}
		}
	}

	if p.stack.len() > 0 {
		lastScope, lastBuffer := p.stack.last()
		lastScope.appendElement(element)
		if err := p.appendScope(lastScope, lastBuffer, fullEle); err != nil {
			if p.strictMode {
				return err
			}
			refused = errors.Join(refused, err)
		}
	}

	if currentScope.listener == nil {
		return refused
	}
	shouldEmit := currentScope.listener.EmitAlways || p.stack.len() == 0

	if !shouldEmit || currentScope.listener.OnComplete == nil {
		return refused
	}

	if err := currentScope.listener.OnComplete(element); err != nil {
		// External callback errors are always fatal.
		return &interruptError{
			inner: fmt.Errorf("OnComplete callback for <%s>: %w",
				currentScope.element.Start.Name, err),
		}
	}

	return refused
}
