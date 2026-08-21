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

// TestStreamScannerConfig_ApplyDefaults verifies zero fields get defaults.
func TestStreamScannerConfig_ApplyDefaults(t *testing.T) {
	cfg := StreamScannerConfig{
		Listeners: []*ElementListener{{Name: Name{Local: "a"}}},
	}
	cfg.ApplyDefaults()
	assert.Equal(t, 256, cfg.MaxNestingLevel)
	assert.Equal(t, 4096, cfg.BufferSize)
	assert.Equal(t, 1024, cfg.Listeners[0].MaxBufferSize)
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
// overflow through OnError, keeps parsing, and still completes the element
// with the content accumulated so far.
func TestStreamScanner_BufferOverflowNonStrict(t *testing.T) {
	var errs []string
	var names []Element
	scan(t, StreamScannerConfig{
		Listeners: []*ElementListener{
			{Name: Name{Local: "name"}, MaxBufferSize: 8, OnComplete: func(e Element) error {
				names = append(names, e)
				return nil
			}},
			captureListener("other", &[]Element{}),
		},
		OnError: func(err error) {
			errs = append(errs, err.Error())
		},
	}, `<name>AB</name><other>OK</other>`)

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "element buffer overflow")
	require.Len(t, names, 1)
	assert.Equal(t, `<name>AB</name>`, names[0].String())
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
