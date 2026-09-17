package xml

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureListener returns a listener that appends every completed element.
func captureListener(name string, out *[]Element) *ElementListener {
	return &ElementListener{
		Name: Name{Local: name},
		OnComplete: func(e Element) error {
			*out = append(*out, e)
			return nil
		},
	}
}

// contentStrings renders each Content to its string form for assertions.
func contentStrings(contents []Content) []string {
	out := make([]string, 0, len(contents))
	for _, c := range contents {
		out = append(out, c.String())
	}
	return out
}

// scan runs a scanner over input with the supplied config and asserts success.
func scan(t *testing.T, config StreamScannerConfig, input string) *StreamScanner {
	t.Helper()
	scanner, err := NewStreamScanner(config)
	require.NoError(t, err)
	require.NoError(t, scanner.Scan(strings.NewReader(input)))
	return scanner
}

// TestStreamScannerConfig_Validate tests configuration validation.
func TestStreamScannerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  StreamScannerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no_listeners",
			config:  StreamScannerConfig{},
			wantErr: true,
			errMsg:  "at least one listener",
		},
		{
			name:    "empty_name",
			config:  StreamScannerConfig{Listeners: []*ElementListener{{Name: Name{}}}},
			wantErr: true,
			errMsg:  "must not be empty",
		},
		{
			name: "duplicate_listener",
			config: StreamScannerConfig{Listeners: []*ElementListener{
				{Name: Name{Local: "a"}},
				{Name: Name{Local: "a"}},
			}},
			wantErr: true,
			errMsg:  "duplicate listener",
		},
		{
			name: "valid",
			config: StreamScannerConfig{Listeners: []*ElementListener{
				{Name: Name{Local: "a"}},
			}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestStreamScannerConfig_ApplyDefaults verifies zero fields get defaults and
// that a caller-supplied text buffer cap is left alone.
func TestStreamScannerConfig_ApplyDefaults(t *testing.T) {
	cfg := StreamScannerConfig{
		Listeners: []*ElementListener{{Name: Name{Local: "a"}}},
	}
	cfg.ApplyDefaults()
	assert.Equal(t, 256, cfg.MaxNestingLevel)
	assert.Equal(t, 4096, cfg.BufferSize)
	assert.Equal(t, 1024, cfg.Listeners[0].MaxBufferSize)
	assert.Equal(t, 1<<20, cfg.MaxTextBufferSize)

	t.Run("positive_value_kept", func(t *testing.T) {
		custom := StreamScannerConfig{
			MaxTextBufferSize: 2048,
			Listeners:         []*ElementListener{{Name: Name{Local: "a"}}},
		}
		custom.ApplyDefaults()
		assert.Equal(t, 2048, custom.MaxTextBufferSize)
	})

	t.Run("negative_disables_cap", func(t *testing.T) {
		unlimited := StreamScannerConfig{
			MaxTextBufferSize: -1,
			Listeners:         []*ElementListener{{Name: Name{Local: "a"}}},
		}
		unlimited.ApplyDefaults()
		assert.Equal(t, -1, unlimited.MaxTextBufferSize)
	})
}

// TestStreamScanner_ParseTrackedElements verifies a nested tracked document
// fires top-level OnComplete with a fully-built Element tree.
func TestStreamScanner_ParseTrackedElements(t *testing.T) {
	var persons []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			captureListener("person", &persons),
			captureListener("name", &[]Element{}),
			captureListener("age", &[]Element{}),
		},
	}, `<person id="1"><name>ToM</name><age>30</age></person>`)

	require.Len(t, persons, 1)

	person := persons[0]
	assert.Equal(t, "person", person.Start.Name.String())
	assert.Equal(t, "person", person.End.Name.String())
	assert.Equal(t, []Attr{{Name: Name{Local: "id"}, Value: "1"}}, person.Start.Attrs)
	assert.Equal(t, []string{"<name>ToM</name>", "<age>30</age>"}, contentStrings(person.Contents))

	name, ok := person.Contents[0].(Element)
	require.True(t, ok)
	assert.Equal(t, []string{"ToM"}, contentStrings(name.Contents))
	age, ok := person.Contents[1].(Element)
	require.True(t, ok)
	assert.Equal(t, []string{"30"}, contentStrings(age.Contents))

	assert.Equal(t, `<person id="1"><name>ToM</name><age>30</age></person>`, person.String())
}

// TestStreamScanner_SelfClosingElement verifies self-closing tags emit.
func TestStreamScanner_SelfClosingElement(t *testing.T) {
	var elements []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("person", &elements)},
	}, `<person id="1"/><person/>`)

	require.Len(t, elements, 2)
	assert.Empty(t, elements[0].Contents)
	assert.Equal(t, "person", elements[0].Start.Name.String())
	assert.Equal(t, []Attr{{Name: Name{Local: "id"}, Value: "1"}}, elements[0].Start.Attrs)
	assert.Empty(t, elements[1].Contents)
}

// TestStreamScanner_UntrackedTopLevelPassthrough verifies untracked
// top-level markup is forwarded verbatim to OnText.
func TestStreamScanner_UntrackedTopLevelPassthrough(t *testing.T) {
	var texts []string
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("person", &[]Element{})},
		OnText: func(s string) error {
			texts = append(texts, s)
			return nil
		},
	}, `<unknown>x</unknown>`)

	// The "<" of the closing tag flushes the accumulated passthrough text,
	// so untracked markup surfaces as two OnText segments.
	assert.Equal(t, []string{`<unknown>x`, `</unknown>`}, texts)
}

// TestStreamScanner_OnTextCallbacks verifies text segments outside tracked
// elements are reported in order.
func TestStreamScanner_OnTextCallbacks(t *testing.T) {
	var texts []string
	var persons []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("person", &persons)},
		OnText: func(s string) error {
			texts = append(texts, s)
			return nil
		},
	}, `before <person>hi</person> after`)

	assert.Equal(t, []string{"before ", " after"}, texts)
	require.Len(t, persons, 1)
	assert.Equal(t, []string{"hi"}, contentStrings(persons[0].Contents))
}

// TestStreamScanner_NestedEmitAlways verifies a nested listener with
// EmitAlways=true fires as soon as its element closes.
func TestStreamScanner_NestedEmitAlways(t *testing.T) {
	var names []Element
	var persons []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			captureListener("person", &persons),
			{
				Name:       Name{Local: "name"},
				EmitAlways: true,
				OnComplete: func(e Element) error {
					names = append(names, e)
					return nil
				},
			},
		},
	}, `<person><name>ToM</name></person>`)

	require.Len(t, names, 1)
	assert.Equal(t, `<name>ToM</name>`, names[0].String())
	require.Len(t, persons, 1)
	assert.Equal(t, `<person><name>ToM</name></person>`, persons[0].String())
}

// TestStreamScanner_NestedNoEmitWithoutEmitAlways verifies a nested tracked
// element without EmitAlways stays silent and is folded into its parent.
func TestStreamScanner_NestedNoEmitWithoutEmitAlways(t *testing.T) {
	var names []Element
	var persons []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			captureListener("person", &persons),
			captureListener("name", &names),
		},
	}, `<person><name>ToM</name></person>`)

	assert.Empty(t, names)
	require.Len(t, persons, 1)
	assert.Equal(t, `<person><name>ToM</name></person>`, persons[0].String())
}

// TestStreamScanner_UntrackedNestedFolded verifies untracked elements nested
// inside a tracked scope become child Elements of the tracked element.
func TestStreamScanner_UntrackedNestedFolded(t *testing.T) {
	var persons []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("person", &persons)},
	}, `<person><unknown>y</unknown></person>`)

	require.Len(t, persons, 1)
	assert.Equal(t, []string{"<unknown>y</unknown>"}, contentStrings(persons[0].Contents))
}

// TestStreamScanner_ConsecutiveCharDataMerged verifies adjacent text runs
// inside a tracked element collapse into a single CharData.
func TestStreamScanner_ConsecutiveCharDataMerged(t *testing.T) {
	var persons []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("person", &persons)},
	}, `<person>abc def</person>`)

	require.Len(t, persons, 1)
	assert.Equal(t, []string{"abc def"}, contentStrings(persons[0].Contents))
}

// TestStreamScanner_BufferOverflowStrict verifies strict mode aborts on
// exceeding a listener's MaxBufferSize.
func TestStreamScanner_BufferOverflowStrict(t *testing.T) {
	var names []Element
	scanner, err := NewStreamScanner(StreamScannerConfig{
		StrictMode: true,
		Listeners: []*ElementListener{
			{Name: Name{Local: "name"}, MaxBufferSize: 8, OnComplete: func(e Element) error {
				names = append(names, e)
				return nil
			}},
		},
	})
	require.NoError(t, err)

	err = scanner.Scan(strings.NewReader(`<name>AB</name>`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "element buffer overflow")
	assert.Empty(t, names)
}

// TestStreamScanner_BufferOverflowNonStrict verifies non-strict mode reports
// overflow through OnError, keeps parsing, and completes the element with the
// content accumulated before the cap was reached: everything past the limit is
// dropped.
func TestStreamScanner_BufferOverflowNonStrict(t *testing.T) {
	var errs []string
	var names []Element
	var others []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			{Name: Name{Local: "name"}, MaxBufferSize: 8, OnComplete: func(e Element) error {
				names = append(names, e)
				return nil
			}},
			captureListener("other", &others),
		},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, `<name>AB</name><other>OK</other>`)

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "element buffer overflow")
	require.Len(t, names, 1)
	// The byte that would reach the 8-byte limit is dropped; the closing
	// `</name>` is appended by the pop path, outside the capped write.
	assert.Equal(t, `<name>A</name>`, names[0].String())
	// Parsing continues with the element following the truncated one.
	require.Len(t, others, 1)
	assert.Equal(t, `<other>OK</other>`, others[0].String())
}

// TestStreamScanner_TextBufferOverflowNonStrict verifies that text outside
// tracked elements is capped, that the overflow is reported once, and that
// parsing continues with the truncated text.
func TestStreamScanner_TextBufferOverflowNonStrict(t *testing.T) {
	var errs []string
	var texts []string
	var names []Element
	scan(t, StreamScannerConfig{
		MaxTextBufferSize: 16,
		Listeners:         []*ElementListener{captureListener("name", &names)},
		OnText: func(s string) error {
			texts = append(texts, s)
			return nil
		},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, strings.Repeat("x", 1000)+`<name>ok</name>`)

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "text buffer overflow")
	require.Len(t, texts, 1)
	assert.LessOrEqual(t, len(texts[0]), 16)
	assert.Equal(t, strings.Repeat("x", len(texts[0])), texts[0], "only the leading payload bytes may be buffered")
	require.Len(t, names, 1)
	assert.Equal(t, `<name>ok</name>`, names[0].String())
}

// TestStreamScanner_TextBufferOverflowStrict verifies that strict mode aborts
// the scan once the out-of-element text cap is reached.
func TestStreamScanner_TextBufferOverflowStrict(t *testing.T) {
	scanner, err := NewStreamScanner(StreamScannerConfig{
		StrictMode:        true,
		MaxTextBufferSize: 16,
		Listeners:         []*ElementListener{captureListener("name", &[]Element{})},
	})
	require.NoError(t, err)

	err = scanner.Scan(strings.NewReader(strings.Repeat("x", 1000) + `<name>ok</name>`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text buffer overflow")
}

// TestStreamScanner_TextBufferUnlimited verifies that a negative
// MaxTextBufferSize disables the out-of-element text cap.
func TestStreamScanner_TextBufferUnlimited(t *testing.T) {
	var texts []string
	var errs []string
	payload := strings.Repeat("x", 1000)
	scan(t, StreamScannerConfig{
		MaxTextBufferSize: -1,
		Listeners:         []*ElementListener{captureListener("name", &[]Element{})},
		OnText: func(s string) error {
			texts = append(texts, s)
			return nil
		},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, payload)

	assert.Empty(t, errs)
	assert.Equal(t, []string{payload}, texts)
}

// TestStreamScanner_TextCapIgnoredInsideScope verifies that the text cap only
// applies outside tracked elements: element content is governed by the
// listener's MaxBufferSize instead.
func TestStreamScanner_TextCapIgnoredInsideScope(t *testing.T) {
	var errs []string
	var names []Element
	scan(t, StreamScannerConfig{
		MaxTextBufferSize: 4,
		Listeners:         []*ElementListener{captureListener("name", &names)},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, `<name>abcdefghij</name>`)

	assert.Empty(t, errs)
	require.Len(t, names, 1)
	assert.Equal(t, []string{"abcdefghij"}, contentStrings(names[0].Contents))
}

// TestStreamScanner_BufferOverflowReportsOnce verifies that the element cap
// refuses every write past the limit but reports the overflow only once, so a
// long body cannot flood OnError with one error per dropped byte.
func TestStreamScanner_BufferOverflowReportsOnce(t *testing.T) {
	var errs []string
	var names []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			{Name: Name{Local: "name"}, MaxBufferSize: 8, OnComplete: func(e Element) error {
				names = append(names, e)
				return nil
			}},
		},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, `<name>`+strings.Repeat("A", 100)+`</name>`)

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "element buffer overflow")
	require.Len(t, names, 1)
	assert.Equal(t, `<name>A</name>`, names[0].String())
}

// maxScopeBufferedBytes reports the most bytes held by any single open scope
// buffer, i.e. the buffers [ElementListener.MaxBufferSize] bounds.
func maxScopeBufferedBytes(p *StreamScanner) int {
	maxLen := 0
	for _, buffer := range p.stack.buffer {
		if n := buffer.Len(); n > maxLen {
			maxLen = n
		}
	}
	return maxLen
}

// sampleCaps feeds payload to scanner in small chunks and reports the largest
// element buffer it ever held, checking the caps after every chunk. processChunk
// is used instead of Scan so the buffers stay inspectable.
func sampleCaps(t *testing.T, p *StreamScanner, payload string) int {
	t.Helper()
	const chunk = 64
	peak := 0
	for i := 0; i < len(payload); i += chunk {
		require.NoError(t, p.processChunk([]byte(payload[i:min(i+chunk, len(payload))])))
		peak = max(peak, maxScopeBufferedBytes(p))
	}
	return peak
}

// TestStreamScanner_ScopeCapCoversNestedUntrackedContent verifies that content
// buffered for an element no listener tracks is charged against the cap of the
// tracked element enclosing it: nesting cannot buffer past that cap.
func TestStreamScanner_ScopeCapCoversNestedUntrackedContent(t *testing.T) {
	const limit = 32
	var errs []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{
			{Name: Name{Local: "item"}, MaxBufferSize: limit, OnComplete: func(Element) error { return nil }},
		},
		OnError: func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	peak := sampleCaps(t, scanner, `<item><unknown>`+strings.Repeat("x", 4096)+`</unknown></item>`)

	assert.LessOrEqual(t, peak, limit, "buffered bytes must stay within the enclosing element's cap")
	require.Len(t, errs, 2, "the untracked scope and its enclosing scope each report once")
	for _, msg := range errs {
		assert.Contains(t, msg, "element buffer overflow")
	}
}

// TestStreamScanner_ScopeCapCoversMismatchedClosingTag verifies that the text of
// a closing tag that does not match the open element is charged against that
// element's cap instead of being appended without limit.
func TestStreamScanner_ScopeCapCoversMismatchedClosingTag(t *testing.T) {
	const limit = 32
	var errs []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{
			{Name: Name{Local: "item"}, MaxBufferSize: limit, OnComplete: func(Element) error { return nil }},
		},
		OnError: func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	peak := sampleCaps(t, scanner, `<item>`+strings.Repeat(`</zzz>`, 500))

	assert.LessOrEqual(t, peak, limit, "mismatched closing tags must not grow the buffer past the cap")
	overflows := 0
	for _, msg := range errs {
		if strings.Contains(msg, "element buffer overflow") {
			overflows++
		}
	}
	assert.Equal(t, 1, overflows, "the cap is reported once, not once per dropped tag")
}

// TestStreamScanner_ScopeCapCoversChildElementText verifies that the copy of a
// completed nested element handed to its enclosing scope is charged against the
// enclosing cap.
func TestStreamScanner_ScopeCapCoversChildElementText(t *testing.T) {
	const (
		outerLimit = 16
		innerLimit = 128
	)
	var errs []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{
			{Name: Name{Local: "item"}, MaxBufferSize: outerLimit, OnComplete: func(Element) error { return nil }},
			{Name: Name{Local: "sub"}, MaxBufferSize: innerLimit, EmitAlways: true, OnComplete: func(Element) error { return nil }},
		},
		OnError: func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	payload := `<item><sub>` + strings.Repeat("y", 100) + `</sub></item>`
	peak := 0
	for i := 0; i < len(payload); i += 16 {
		require.NoError(t, scanner.processChunk([]byte(payload[i:min(i+16, len(payload))])))
		if scanner.stack.len() > 0 {
			peak = max(peak, scanner.stack.buffer[0].Len())
		}
	}

	assert.LessOrEqual(t, peak, outerLimit, "the nested element's text must not grow the outer buffer past its cap")
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "element buffer overflow")
}

// TestStreamScanner_TextCapCoversStrayClosingTag verifies that a closing tag with
// no open element is charged against the text cap like any other passed-through
// text.
func TestStreamScanner_TextCapCoversStrayClosingTag(t *testing.T) {
	const maxText = 16
	var errs []string
	var texts []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		MaxTextBufferSize: maxText,
		Listeners:         []*ElementListener{captureListener("item", &[]Element{})},
		OnText:            func(s string) error { texts = append(texts, s); return nil },
		OnError:           func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	// Space padding makes one closing tag longer than the cap while its name
	// still matches a listener, which is what routes it here with no open scope.
	require.NoError(t, scanner.processChunk([]byte(`</item`+strings.Repeat(" ", 64)+`>`)))

	assert.LessOrEqual(t, scanner.buffers.text.Len(), maxText)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "text buffer overflow")
	assert.Empty(t, texts, "a stray closing tag past the cap is dropped, not passed through")
}

// TestStreamScanner_TextCapCoversUnclosedRemainder verifies that the remainder of
// an element left open at end of input is charged against the text cap, the same
// as the text it is reported as.
func TestStreamScanner_TextCapCoversUnclosedRemainder(t *testing.T) {
	const maxText = 16
	var errs []string
	var texts []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		MaxTextBufferSize: maxText,
		Listeners: []*ElementListener{
			{Name: Name{Local: "item"}, MaxBufferSize: 128, OnComplete: func(Element) error { return nil }},
		},
		OnText:  func(s string) error { texts = append(texts, s); return nil },
		OnError: func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	require.NoError(t, scanner.processChunk([]byte(`<item>`+strings.Repeat("x", 64))))
	require.NoError(t, scanner.finalize())

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "text buffer overflow")
	assert.Empty(t, texts, "an unclosed remainder past the cap is dropped, not truncated into the buffer")
}

// TestStreamScanner_TagCapCoversUnterminatedTag verifies that the tag being read
// is capped too: an unterminated tag must not grow without bound even though no
// element or text is buffered yet. The cap is the largest one configured, here
// the listener's.
func TestStreamScanner_TagCapCoversUnterminatedTag(t *testing.T) {
	const (
		maxText = 8
		tagCap  = 16
	)
	var errs []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		MaxTextBufferSize: maxText,
		Listeners: []*ElementListener{
			{Name: Name{Local: "item"}, MaxBufferSize: tagCap, OnComplete: func(Element) error { return nil }},
		},
		OnError: func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	require.NoError(t, scanner.processChunk([]byte("<"+strings.Repeat("x", 4096))))

	assert.LessOrEqual(t, scanner.buffers.element.Len(), tagCap)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "tag buffer overflow")
}

// TestStreamScanner_TagCapCoversRepeatedOpenAngle verifies that a '<' arriving
// while a tag is already open is capped too: a run of '<' must not grow the tag
// buffer without bound.
func TestStreamScanner_TagCapCoversRepeatedOpenAngle(t *testing.T) {
	const (
		maxText = 8
		tagCap  = 16
	)
	var errs []string
	scanner, err := NewStreamScanner(StreamScannerConfig{
		MaxTextBufferSize: maxText,
		Listeners: []*ElementListener{
			{Name: Name{Local: "item"}, MaxBufferSize: tagCap, OnComplete: func(Element) error { return nil }},
		},
		OnError: func(err error) { errs = append(errs, err.Error()) },
	})
	require.NoError(t, err)

	require.NoError(t, scanner.processChunk([]byte(strings.Repeat("<", 4096))))

	assert.LessOrEqual(t, scanner.buffers.element.Len(), tagCap)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "tag buffer overflow")
}

// TestStreamScanner_NestingLevelExceededStrict verifies strict mode aborts
// once MaxNestingLevel is exceeded.
func TestStreamScanner_NestingLevelExceededStrict(t *testing.T) {
	scanner, err := NewStreamScanner(StreamScannerConfig{
		StrictMode:      true,
		MaxNestingLevel: 1,
		Listeners:       []*ElementListener{captureListener("a", &[]Element{})},
	})
	require.NoError(t, err)

	err = scanner.Scan(strings.NewReader(`<a><b>x</b></a>`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nesting level 2 exceeds maximum 1")
}

// TestStreamScanner_NestingLevelExceededNonStrict verifies non-strict mode
// reports the nesting violation through OnError without aborting.
func TestStreamScanner_NestingLevelExceededNonStrict(t *testing.T) {
	var errs []string
	scan(t, StreamScannerConfig{
		MaxNestingLevel: 1,
		Listeners:       []*ElementListener{captureListener("a", &[]Element{})},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, `<a><b>x</b></a>`)

	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "nesting level 2 exceeds maximum 1")
}

// TestStreamScanner_MismatchedClosingElementStrict verifies strict mode
// aborts on a closing tag that does not match the open scope.
func TestStreamScanner_MismatchedClosingElementStrict(t *testing.T) {
	scanner, err := NewStreamScanner(StreamScannerConfig{
		StrictMode: true,
		Listeners: []*ElementListener{
			captureListener("a", &[]Element{}),
			captureListener("b", &[]Element{}),
		},
	})
	require.NoError(t, err)

	err = scanner.Scan(strings.NewReader(`<a><b></a>`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatched closing element")
}

// TestStreamScanner_MismatchedClosingElementNonStrict verifies non-strict
// mode reports the mismatch through OnError and finishes without error.
func TestStreamScanner_MismatchedClosingElementNonStrict(t *testing.T) {
	var errs []string
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			captureListener("a", &[]Element{}),
			captureListener("b", &[]Element{}),
		},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, `<a><b></a>`)

	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "mismatched closing element")
}

// TestStreamScanner_UnclosedElementStrict verifies strict mode reports an
// unclosed element at EOF as an unexpected EOF error.
func TestStreamScanner_UnclosedElementStrict(t *testing.T) {
	scanner, err := NewStreamScanner(StreamScannerConfig{
		StrictMode: true,
		Listeners:  []*ElementListener{captureListener("a", &[]Element{})},
	})
	require.NoError(t, err)

	err = scanner.Scan(strings.NewReader(`<a>x`))
	require.Error(t, err)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Contains(t, err.Error(), "unclosed element")
}

// TestStreamScanner_UnclosedElementNonStrict verifies non-strict mode flushes
// the unclosed remainder to OnText instead of failing.
func TestStreamScanner_UnclosedElementNonStrict(t *testing.T) {
	var texts []string
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("a", &[]Element{})},
		OnText: func(s string) error {
			texts = append(texts, s)
			return nil
		},
	}, `<a>x`)

	assert.Equal(t, []string{`<a>x`}, texts)
}

// TestStreamScanner_CallbackErrorAlwaysFatal verifies a listener callback
// error aborts parsing in both strict and non-strict modes.
func TestStreamScanner_CallbackErrorAlwaysFatal(t *testing.T) {
	for _, strict := range []bool{true, false} {
		name := "non_strict"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			scanner, err := NewStreamScanner(StreamScannerConfig{
				StrictMode: strict,
				Listeners: []*ElementListener{
					{
						Name: Name{Local: "a"},
						OnComplete: func(Element) error {
							return errors.New("boom")
						},
					},
				},
			})
			require.NoError(t, err)

			err = scanner.Scan(strings.NewReader(`<a>1</a>`))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "OnComplete callback")
			assert.Contains(t, err.Error(), "boom")
		})
	}
}

// TestStreamScanner_OnTextErrorFatal verifies an OnText callback error aborts
// parsing.
func TestStreamScanner_OnTextErrorFatal(t *testing.T) {
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("a", &[]Element{})},
		OnText: func(string) error {
			return errors.New("text boom")
		},
	})
	require.NoError(t, err)

	err = scanner.Scan(strings.NewReader(`hello`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OnText callback")
	assert.Contains(t, err.Error(), "text boom")
}

// TestStreamScanner_ReadError verifies reader errors are wrapped and returned.
func TestStreamScanner_ReadError(t *testing.T) {
	readErr := errors.New("read error")
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("a", &[]Element{})},
	})
	require.NoError(t, err)

	err = scanner.Scan(&errorReader{err: readErr})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read error")
}

// eofWithDataReader returns its whole payload together with io.EOF on the first
// Read call, which io.Reader explicitly allows.
type eofWithDataReader struct {
	data []byte
	done bool
}

// Read implements io.Reader, reporting data and io.EOF in a single call.
func (r *eofWithDataReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	if n < len(r.data) {
		r.data = r.data[n:]
		return n, nil
	}
	r.done = true
	return n, io.EOF
}

// TestStreamScanner_DataWithEOF verifies that bytes returned together with
// io.EOF are parsed instead of being dropped.
func TestStreamScanner_DataWithEOF(t *testing.T) {
	var items []Element
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("item", &items)},
	})
	require.NoError(t, err)

	require.NoError(t, scanner.Scan(&eofWithDataReader{data: []byte(`<r><item>a</item></r>`)}))
	require.Len(t, items, 1)
	assert.Equal(t, `<item>a</item>`, items[0].String())
	assert.Equal(t, []string{"a"}, contentStrings(items[0].Contents))
}

// TestStreamScanner_ReuseAfterReset verifies a scanner can be reused across
// multiple Scan calls.
func TestStreamScanner_ReuseAfterReset(t *testing.T) {
	var names []Element
	scanner, err := NewStreamScanner(StreamScannerConfig{
		Listeners: []*ElementListener{captureListener("name", &names)},
	})
	require.NoError(t, err)

	require.NoError(t, scanner.Scan(strings.NewReader(`<name>A</name>`)))
	require.NoError(t, scanner.Scan(strings.NewReader(`<name>B</name>`)))
	require.Len(t, names, 2)
	assert.Equal(t, `<name>A</name>`, names[0].String())
	assert.Equal(t, `<name>B</name>`, names[1].String())
}

// TestStreamScanner_EmptyAndTextOnlyInput verifies degenerate inputs.
func TestStreamScanner_EmptyAndTextOnlyInput(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		scan(t, StreamScannerConfig{
			Listeners: []*ElementListener{captureListener("a", &[]Element{})},
		}, "")
	})
	t.Run("text_only", func(t *testing.T) {
		var texts []string
		scan(t, StreamScannerConfig{
			Listeners: []*ElementListener{captureListener("a", &[]Element{})},
			OnText: func(s string) error {
				texts = append(texts, s)
				return nil
			},
		}, "just text")
		assert.Equal(t, []string{"just text"}, texts)
	})
}

// errorReader is a mock reader that always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) { return 0, r.err }

// TestElementHelpers covers the exported helper functions in element.go.
func TestElementHelpers(t *testing.T) {
	t.Run("element_type_predicates", func(t *testing.T) {
		assert.True(t, IsStartElement(`<a>`))
		assert.True(t, IsStartElement(`<a x="1">`))
		assert.False(t, IsStartElement(`</a>`))
		assert.False(t, IsStartElement(`<a/>`))
		assert.True(t, IsEndElement(`</a>`))
		assert.False(t, IsEndElement(`<a>`))
		assert.True(t, IsSelfClosingElement(`<a/>`))
		assert.True(t, IsSelfClosingElement(`<a x="1"/>`))
		assert.False(t, IsSelfClosingElement(`<a>`))
		assert.False(t, IsSelfClosingElement(`</a>`))
	})

	t.Run("extract_element_name", func(t *testing.T) {
		name, err := ExtractElementName(`<a x="1">`)
		require.NoError(t, err)
		assert.Equal(t, "a", name.String())
		name, err = ExtractElementName(`</a>`)
		require.NoError(t, err)
		assert.Equal(t, "a", name.String())
	})

	t.Run("parse_attrs", func(t *testing.T) {
		attrs, err := ParseAttrs(`<a x='1' y="two">`)
		require.NoError(t, err)
		assert.Equal(t, []Attr{
			{Name: Name{Local: "x"}, Value: "1"},
			{Name: Name{Local: "y"}, Value: "two"},
		}, attrs)
	})

	t.Run("expand_self_close", func(t *testing.T) {
		start, end, err := ExpandSelfCloseElement(`<a x="1"/>`)
		require.NoError(t, err)
		assert.Equal(t, `<a x="1">`, start)
		assert.Equal(t, `</a>`, end)
	})

	t.Run("extract_content", func(t *testing.T) {
		content, err := ExtractElementContent(`<a>hello</a>`)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(content))
	})

	t.Run("copy_is_deep", func(t *testing.T) {
		element := Element{
			Start: StartElement{Name: Name{Local: "a"}, Attrs: []Attr{{Name: Name{Local: "x"}, Value: "1"}}},
			Contents: []Content{
				CharData("hi"),
				Element{
					Start:    StartElement{Name: Name{Local: "b"}},
					Contents: []Content{CharData("inner")},
					End:      EndElement{Name: Name{Local: "b"}},
				},
			},
			End: EndElement{Name: Name{Local: "a"}},
		}
		clone := element.Copy()
		assert.Equal(t, `<a x="1">hi<b>inner</b></a>`, element.String())
		assert.Equal(t, element.String(), clone.String())
		// Mutating the clone's deepest data must not affect the original.
		clone.Contents[0].(CharData)[0] = 'X'
		clone.Contents[1].(Element).Contents[0].(CharData)[0] = 'Y'
		clone.Start.Attrs[0].Value = "mutated"
		assert.Equal(t, []string{"hi", "<b>inner</b>"}, contentStrings(element.Contents))
		assert.Equal(t, []Attr{{Name: Name{Local: "x"}, Value: "1"}}, element.Start.Attrs)
		assert.Equal(t, []string{"Xi", "<b>Ynner</b>"}, contentStrings(clone.Contents))
	})
}
