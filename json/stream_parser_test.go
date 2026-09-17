package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tangerg/pkg/random"
)

// TestStreamParserConfig_Validate tests configuration validation.
func TestStreamParserConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  StreamParserConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "nil_reader",
			config: StreamParserConfig{
				Reader: nil,
			},
			wantErr: true,
			errMsg:  "reader required",
		},
		{
			name: "valid_config_with_buffer_size",
			config: StreamParserConfig{
				Reader:     strings.NewReader("{}"),
				BufferSize: 1024,
			},
			wantErr: false,
		},
		{
			name: "zero_buffer_size_sets_default",
			config: StreamParserConfig{
				Reader:     strings.NewReader("{}"),
				BufferSize: 0,
			},
			wantErr: false,
		},
		{
			name: "negative_buffer_size_sets_default",
			config: StreamParserConfig{
				Reader:     strings.NewReader("{}"),
				BufferSize: -1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}
			require.NoError(t, err)
			// ApplyDefaults is the path that mutates — verify
			// zero/negative buffer sizes get filled in.
			cfg := tt.config
			cfg.ApplyDefaults()
			if tt.config.BufferSize <= 0 {
				assert.Equal(t, 4096, cfg.BufferSize, "Should set default buffer size")
			}
		})
	}
}

// TestNewStreamParser tests parser initialization.
func TestNewStreamParser(t *testing.T) {
	tests := []struct {
		name    string
		config  StreamParserConfig
		wantErr bool
	}{
		{
			name: "valid_config",
			config: StreamParserConfig{
				Reader:     strings.NewReader("{}"),
				BufferSize: 1024,
			},
			wantErr: false,
		},
		{
			name: "invalid_config",
			config: StreamParserConfig{
				Reader: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewStreamParser(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, parser)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, parser)
				assert.NotNil(t, parser.scopes)
				assert.NotNil(t, parser.buffers)
				assert.NotNil(t, parser.topBuf)
			}
		})
	}
}

// TestStreamParser_ParseObject tests parsing JSON objects.
func TestStreamParser_ParseObject(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []map[string]any
		wantErr  bool
	}{
		{
			name:  "single_object",
			input: `{"name":"Alice","age":30}`,
			expected: []map[string]any{
				{"name": "Alice", "age": float64(30)},
			},
			wantErr: false,
		},
		{
			name:  "multiple_objects",
			input: `{"id":1}{"id":2}{"id":3}`,
			expected: []map[string]any{
				{"id": float64(1)},
				{"id": float64(2)},
				{"id": float64(3)},
			},
			wantErr: false,
		},
		{
			name:  "nested_object",
			input: `{"user":{"name":"Bob","address":{"city":"NYC"}}}`,
			expected: []map[string]any{
				{
					"user": map[string]any{
						"name": "Bob",
						"address": map[string]any{
							"city": "NYC",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:  "object_with_special_chars",
			input: `{"text":"Hello \"World\"","path":"C:\\Users"}`,
			expected: []map[string]any{
				{"text": `Hello "World"`, "path": `C:\Users`},
			},
			wantErr: false,
		},
		{
			name:     "empty_object",
			input:    `{}`,
			expected: []map[string]any{{}},
			wantErr:  false,
		},
		{
			name:    "unclosed_object",
			input:   `{"name":"Alice"`,
			wantErr: true,
		},
		{
			name:    "invalid_json",
			input:   `{"name":}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var results []map[string]any
			var parseErrors []error

			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnObject: func(obj map[string]any) error {
					results = append(results, obj)
					return nil
				},
				OnError: func(err error) {
					parseErrors = append(parseErrors, err)
				},
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.expected), len(results))
				for i, expected := range tt.expected {
					assert.Equal(t, expected, results[i])
				}
			}
		})
	}
}

// TestStreamParser_ParseArray tests parsing JSON arrays.
func TestStreamParser_ParseArray(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected [][]any
		wantErr  bool
	}{
		{
			name:  "simple_array",
			input: `[1,2,3,4,5]`,
			expected: [][]any{
				{float64(1), float64(2), float64(3), float64(4), float64(5)},
			},
			wantErr: false,
		},
		{
			name:  "multiple_arrays",
			input: `[1,2][3,4][5,6]`,
			expected: [][]any{
				{float64(1), float64(2)},
				{float64(3), float64(4)},
				{float64(5), float64(6)},
			},
			wantErr: false,
		},
		{
			name:  "nested_array",
			input: `[[1,2],[3,4],[[5,6]]]`,
			expected: [][]any{
				{
					[]any{float64(1), float64(2)},
					[]any{float64(3), float64(4)},
					[]any{[]any{float64(5), float64(6)}},
				},
			},
			wantErr: false,
		},
		{
			name:  "mixed_types",
			input: `[1,"text",true,null,3.14]`,
			expected: [][]any{
				{float64(1), "text", true, nil, float64(3.14)},
			},
			wantErr: false,
		},
		{
			name:     "empty_array",
			input:    `[]`,
			expected: [][]any{{}},
			wantErr:  false,
		},
		{
			name:    "unclosed_array",
			input:   `[1,2,3`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var results [][]any

			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnArray: func(arr []any) error {
					results = append(results, arr)
					return nil
				},
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.expected), len(results))
				for i, expected := range tt.expected {
					assert.Equal(t, expected, results[i])
				}
			}
		})
	}
}

// TestStreamParser_ParsePrimitives tests parsing primitive values.
func TestStreamParser_ParsePrimitives(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []any
		wantErr  bool
	}{
		{
			name:     "integer",
			input:    `123`,
			expected: []any{float64(123)},
			wantErr:  false,
		},
		{
			name:     "negative_integer",
			input:    `-456`,
			expected: []any{float64(-456)},
			wantErr:  false,
		},
		{
			name:     "float",
			input:    `3.14159`,
			expected: []any{float64(3.14159)},
			wantErr:  false,
		},
		{
			name:     "string",
			input:    `"hello world"`,
			expected: []any{"hello world"},
			wantErr:  false,
		},
		{
			name:     "boolean_true",
			input:    `true`,
			expected: []any{true},
			wantErr:  false,
		},
		{
			name:     "boolean_false",
			input:    `false`,
			expected: []any{false},
			wantErr:  false,
		},
		{
			name:     "null",
			input:    `null`,
			expected: []any{nil},
			wantErr:  false,
		},
		{
			name:  "multiple_primitives",
			input: `123 "text" true null`,
			expected: []any{
				float64(123),
				"text",
				true,
				nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var results []any

			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnValue: func(v any) error {
					results = append(results, v)
					return nil
				},
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.expected), len(results))
				for i, expected := range tt.expected {
					assert.Equal(t, expected, results[i])
				}
			}
		})
	}
}

// TestStreamParser_MixedContent tests parsing mixed JSON content.
func TestStreamParser_MixedContent(t *testing.T) {
	input := `{"name":"Alice","age":30}
              [1,2,3]
              123
              "hello"
              true
              null
              {"id":1}
              [true,false]`

	var objects []map[string]any
	var arrays [][]any
	var values []any

	config := StreamParserConfig{
		Reader:     strings.NewReader(input),
		BufferSize: 64,
		OnObject: func(obj map[string]any) error {
			objects = append(objects, obj)
			return nil
		},
		OnArray: func(arr []any) error {
			arrays = append(arrays, arr)
			return nil
		},
		OnValue: func(v any) error {
			values = append(values, v)
			return nil
		},
	}

	parser, err := NewStreamParser(config)
	require.NoError(t, err)

	err = parser.Parse()
	require.NoError(t, err)

	assert.Len(t, objects, 2)
	assert.Len(t, arrays, 2)
	assert.Len(t, values, 4)

	// Verify objects
	assert.Equal(t, "Alice", objects[0]["name"])
	assert.Equal(t, float64(30), objects[0]["age"])
	assert.Equal(t, float64(1), objects[1]["id"])

	// Verify arrays
	assert.Equal(t, []any{float64(1), float64(2), float64(3)}, arrays[0])
	assert.Equal(t, []any{true, false}, arrays[1])

	// Verify values
	assert.Equal(t, float64(123), values[0])
	assert.Equal(t, "hello", values[1])
	assert.Equal(t, true, values[2])
	assert.Equal(t, nil, values[3])
}

// TestStreamParser_WithWhitespace tests parsing with various whitespace.
func TestStreamParser_WithWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name: "spaces",
			input: `   {  "key"  :  "value"  }   
                    [  1  ,  2  ,  3  ]`,
			expected: 2,
		},
		{
			name: "tabs_and_newlines",
			input: "{\n\t\"key\"\t:\t\"value\"\n}\n" +
				"[\n\t1,\n\t2,\n\t3\n]",
			expected: 2,
		},
		{
			name:     "mixed_whitespace",
			input:    "  \t\n  {\"a\":1}  \n\t  [2,3]  \t\n  ",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := 0

			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnObject: func(obj map[string]any) error {
					count++
					return nil
				},
				OnArray: func(arr []any) error {
					count++
					return nil
				},
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, count)
		})
	}
}

// TestStreamParser_StringEscaping tests string escape sequences.
func TestStreamParser_StringEscaping(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "escaped_quotes",
			input:    `{"text":"He said \"Hello\""}`,
			expected: `He said "Hello"`,
		},
		{
			name:     "escaped_backslash",
			input:    `{"text":"C:\\Users\\Documents"}`,
			expected: `C:\Users\Documents`,
		},
		{
			name:     "escaped_newline",
			input:    `{"text":"Line1\nLine2"}`,
			expected: "Line1\nLine2",
		},
		{
			name:     "escaped_tab",
			input:    `{"text":"Col1\tCol2"}`,
			expected: "Col1\tCol2",
		},
		{
			name:     "multiple_escapes",
			input:    `{"text":"\"\\\/\b\f\n\r\t"}`,
			expected: "\"\\/\b\f\n\r\t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]any

			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnObject: func(obj map[string]any) error {
					result = obj
					return nil
				},
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result["text"])
		})
	}
}

// TestStreamParser_ErrorHandling tests error scenarios.
func TestStreamParser_ErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
		errType   string
	}{
		{
			name:      "mismatched_brackets_object",
			input:     `{"key":"value"]`,
			expectErr: true,
			errType:   "mismatched",
		},
		{
			name:      "mismatched_brackets_array",
			input:     `[1,2,3}`,
			expectErr: true,
			errType:   "mismatched",
		},
		{
			name:      "unexpected_closing_bracket",
			input:     `}`,
			expectErr: true,
			errType:   "unexpected",
		},
		{
			name:      "unclosed_string",
			input:     `{"key":"value`,
			expectErr: true,
		},
		{
			name:      "invalid_number",
			input:     `123.45.67`,
			expectErr: true,
		},
		{
			name:      "invalid_keyword",
			input:     `tru`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var parseErrors []error

			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnObject: func(obj map[string]any) error {
					return nil
				},
				OnArray: func(arr []any) error {
					return nil
				},
				OnError: func(err error) {
					parseErrors = append(parseErrors, err)
				},
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()

			if tt.expectErr {
				assert.Error(t, err)
				if tt.errType != "" {
					assert.Contains(t, err.Error(), tt.errType)
				}
			}
			// Parse errors are delivered to OnError before being
			// returned, exactly once.
			require.Len(t, parseErrors, 1, "parse errors must reach OnError exactly once")
			assert.Equal(t, err.Error(), parseErrors[0].Error())
		})
	}
}

// TestStreamParser_CallbackErrors tests callback error handling.
func TestStreamParser_CallbackErrors(t *testing.T) {
	testErr := errors.New("callback error")

	tests := []struct {
		name     string
		input    string
		onObject func(map[string]any) error
		onArray  func([]any) error
		onValue  func(any) error
		wantErr  bool
	}{
		{
			name:  "object_callback_error",
			input: `{"key":"value"}`,
			onObject: func(obj map[string]any) error {
				return testErr
			},
			wantErr: true,
		},
		{
			name:  "array_callback_error",
			input: `[1,2,3]`,
			onArray: func(arr []any) error {
				return testErr
			},
			wantErr: true,
		},
		{
			name:  "value_callback_error",
			input: `123`,
			onValue: func(v any) error {
				return testErr
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := StreamParserConfig{
				Reader:     strings.NewReader(tt.input),
				BufferSize: 64,
				OnObject:   tt.onObject,
				OnArray:    tt.onArray,
				OnValue:    tt.onValue,
			}

			parser, err := NewStreamParser(config)
			require.NoError(t, err)

			err = parser.Parse()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "callback error")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestStreamParser_LargeInput tests parsing large JSON documents.
func TestStreamParser_LargeInput(t *testing.T) {
	// Generate a large JSON array
	var buf bytes.Buffer
	buf.WriteString("[")
	for i := 0; i < 10000; i++ {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(`{"id":`)
		buf.WriteString(cast.ToString(random.IntRange(0, i+1)))
		buf.WriteString(`,"name":"user`)
		buf.WriteString(cast.ToString(random.IntRange(0, i+1)))
		buf.WriteString(`"}`)
	}
	buf.WriteString("]")

	var count int

	config := StreamParserConfig{
		Reader:     &buf,
		BufferSize: 1024,
		OnArray: func(arr []any) error {
			count = len(arr)
			return nil
		},
	}

	parser, err := NewStreamParser(config)
	require.NoError(t, err)

	err = parser.Parse()
	require.NoError(t, err)
	assert.Equal(t, 10000, count)
}

// TestStreamParser_SmallBufferSize tests parsing with small buffer.
func TestStreamParser_SmallBufferSize(t *testing.T) {
	input := `{"name":"Alice","age":30,"city":"NYC"}`

	var result map[string]any

	config := StreamParserConfig{
		Reader:     strings.NewReader(input),
		BufferSize: 4, // Very small buffer
		OnObject: func(obj map[string]any) error {
			result = obj
			return nil
		},
	}

	parser, err := NewStreamParser(config)
	require.NoError(t, err)

	err = parser.Parse()
	require.NoError(t, err)
	assert.Equal(t, "Alice", result["name"])
	assert.Equal(t, float64(30), result["age"])
	assert.Equal(t, "NYC", result["city"])
}

// TestStreamParser_EmptyInput tests parsing empty input.
func TestStreamParser_EmptyInput(t *testing.T) {
	config := StreamParserConfig{
		Reader:     strings.NewReader(""),
		BufferSize: 64,
	}

	parser, err := NewStreamParser(config)
	require.NoError(t, err)

	err = parser.Parse()
	assert.NoError(t, err)
}

// TestStreamParser_JSONLines tests parsing JSON Lines format.
func TestStreamParser_JSONLines(t *testing.T) {
	input := `{"id":1,"name":"Alice"}
{"id":2,"name":"Bob"}
{"id":3,"name":"Charlie"}`

	var results []map[string]any

	config := StreamParserConfig{
		Reader:     strings.NewReader(input),
		BufferSize: 64,
		OnObject: func(obj map[string]any) error {
			results = append(results, obj)
			return nil
		},
	}

	parser, err := NewStreamParser(config)
	require.NoError(t, err)

	err = parser.Parse()
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.Equal(t, float64(1), results[0]["id"])
	assert.Equal(t, float64(2), results[1]["id"])
	assert.Equal(t, float64(3), results[2]["id"])
}

// TestStreamParser_ReadError tests handling of read errors.
func TestStreamParser_ReadError(t *testing.T) {
	readErr := errors.New("read error")

	config := StreamParserConfig{
		Reader:     &errorReader{err: readErr},
		BufferSize: 64,
	}

	parser, err := NewStreamParser(config)
	require.NoError(t, err)

	err = parser.Parse()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read error")
}

// errorReader is a mock reader that returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

// BenchmarkStreamParser_Objects benchmarks parsing objects.
func BenchmarkStreamParser_Objects(b *testing.B) {
	input := `{"name":"Alice","age":30,"city":"NYC"}`

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		config := StreamParserConfig{
			Reader:     strings.NewReader(input),
			BufferSize: 64,
			OnObject:   func(obj map[string]any) error { return nil },
		}

		parser, _ := NewStreamParser(config)
		_ = parser.Parse()
	}
}

// BenchmarkStreamParser_Arrays benchmarks parsing arrays.
func BenchmarkStreamParser_Arrays(b *testing.B) {
	input := `[1,2,3,4,5,6,7,8,9,10]`

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		config := StreamParserConfig{
			Reader:     strings.NewReader(input),
			BufferSize: 64,
			OnArray:    func(arr []any) error { return nil },
		}

		parser, _ := NewStreamParser(config)
		_ = parser.Parse()
	}
}

// BenchmarkStreamParser_Mixed benchmarks parsing mixed content.
func BenchmarkStreamParser_Mixed(b *testing.B) {
	input := `{"id":1}[1,2,3]"text"123 true null`

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		config := StreamParserConfig{
			Reader:     strings.NewReader(input),
			BufferSize: 64,
			OnObject:   func(obj map[string]any) error { return nil },
			OnArray:    func(arr []any) error { return nil },
			OnValue:    func(v any) error { return nil },
		}

		parser, _ := NewStreamParser(config)
		_ = parser.Parse()
	}
}

// BenchmarkStreamParser_LargeDocument benchmarks parsing large documents.
func BenchmarkStreamParser_LargeDocument(b *testing.B) {
	// Generate a large JSON array
	var buf bytes.Buffer
	buf.WriteString("[")
	for i := 0; i < 1000; i++ {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(`{"id":`)
		json.NewEncoder(&buf).Encode(i)
		buf.WriteString(`,"name":"user"}`)
	}
	buf.WriteString("]")
	input := buf.String()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		config := StreamParserConfig{
			Reader:     strings.NewReader(input),
			BufferSize: 4096,
			OnArray:    func(arr []any) error { return nil },
		}

		parser, _ := NewStreamParser(config)
		_ = parser.Parse()
	}
}

// chunkedStreamReader serves payload in chunkSize pieces so a test value
// arrives incrementally, and it counts the bytes the parser consumed.
type chunkedStreamReader struct {
	payload   []byte
	chunkSize int
	off       int
}

// Read implements io.Reader.
func (r *chunkedStreamReader) Read(p []byte) (int, error) {
	if r.off >= len(r.payload) {
		return 0, io.EOF
	}
	n := min(len(p), r.chunkSize, len(r.payload)-r.off)
	copy(p, r.payload[r.off:r.off+n])
	r.off += n
	return n, nil
}

// consumed reports how many payload bytes the reader has handed out.
func (r *chunkedStreamReader) consumed() int { return r.off }

// TestStreamParser_MaxBufferSize verifies that the total buffered-bytes cap
// stops an oversized in-flight value immediately instead of buffering it.
func TestStreamParser_MaxBufferSize(t *testing.T) {
	const limit = 1024

	// `{"a":"` followed by 64 KiB of payload with no closing quote or brace:
	// without a cap the parser would keep every byte in p.buffers[0].
	payload := []byte(`{"a":"` + strings.Repeat("x", 64*1024))
	reader := &chunkedStreamReader{payload: payload, chunkSize: 257}

	var objects, values int
	var notified []error

	parser, err := NewStreamParser(StreamParserConfig{
		Reader:        reader,
		BufferSize:    512,
		MaxBufferSize: limit,
		OnObject: func(map[string]any) error {
			objects++
			return nil
		},
		OnValue: func(any) error {
			values++
			return nil
		},
		OnError: func(err error) {
			notified = append(notified, err)
		},
	})
	require.NoError(t, err)

	err = parser.Parse()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json: buffer limit 1024 bytes exceeded")
	assert.Zero(t, objects, "OnObject must not fire for an aborted value")
	assert.Zero(t, values, "OnValue must not fire for an aborted value")
	require.Len(t, notified, 1, "the limit error reaches OnError once")
	assert.Equal(t, err.Error(), notified[0].Error())

	// Parsing stopped at the cap instead of draining the 64 KiB payload.
	assert.LessOrEqual(t, reader.consumed(), 4*1024)
}

// TestStreamParser_MaxBufferSizeDefaults pins how zero and negative
// MaxBufferSize values map onto "package default" and "unlimited".
func TestStreamParser_MaxBufferSizeDefaults(t *testing.T) {
	t.Run("zero_selects_package_default", func(t *testing.T) {
		cfg := StreamParserConfig{Reader: strings.NewReader(`{}`)}
		cfg.ApplyDefaults()
		assert.Equal(t, defaultParserMaxBufferSize, cfg.MaxBufferSize)
		assert.Equal(t, 16<<20, cfg.MaxBufferSize)

		parser, err := NewStreamParser(StreamParserConfig{Reader: strings.NewReader(`{}`)})
		require.NoError(t, err)
		assert.Equal(t, defaultParserMaxBufferSize, parser.maxBufferSize)
	})

	t.Run("negative_means_unlimited", func(t *testing.T) {
		cfg := StreamParserConfig{Reader: strings.NewReader(`{}`), MaxBufferSize: -1}
		cfg.ApplyDefaults()
		assert.Equal(t, -1, cfg.MaxBufferSize, "a negative cap stays explicitly unlimited")

		var values []any
		// A 1 MiB top-level string parses fine when the cap is disabled.
		input := `"` + strings.Repeat("x", 1<<20) + `"`
		parser, err := NewStreamParser(StreamParserConfig{
			Reader:        strings.NewReader(input),
			MaxBufferSize: -1,
			OnValue: func(v any) error {
				values = append(values, v)
				return nil
			},
		})
		require.NoError(t, err)
		assert.Less(t, parser.maxBufferSize, 0, "negative config disables the cap")

		require.NoError(t, parser.Parse())
		require.Len(t, values, 1)
		assert.Len(t, values[0], 1<<20)
	})
}

// TestStreamParser_MaxDepth verifies the nesting-depth cap and its switch.
func TestStreamParser_MaxDepth(t *testing.T) {
	const depth = 20
	input := strings.Repeat("[", depth) + strings.Repeat("]", depth)

	t.Run("zero_selects_package_default", func(t *testing.T) {
		var arrays int
		parser, err := NewStreamParser(StreamParserConfig{
			Reader:  strings.NewReader(input),
			OnArray: func([]any) error { arrays++; return nil },
		})
		require.NoError(t, err)
		assert.Equal(t, defaultParserMaxDepth, parser.maxDepth)

		require.NoError(t, parser.Parse())
		assert.Equal(t, 1, arrays)
	})

	t.Run("over_limit_errors", func(t *testing.T) {
		var arrays int
		var notified []error
		parser, err := NewStreamParser(StreamParserConfig{
			Reader:   strings.NewReader(input),
			MaxDepth: 8,
			OnArray:  func([]any) error { arrays++; return nil },
			OnError:  func(err error) { notified = append(notified, err) },
		})
		require.NoError(t, err)
		assert.Equal(t, 8, parser.maxDepth)

		err = parser.Parse()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json: max depth 8 exceeded")
		assert.Zero(t, arrays, "nothing is dispatched once the depth cap trips")
		require.Len(t, notified, 1, "the depth error reaches OnError once")
		assert.Equal(t, err.Error(), notified[0].Error())
	})

	t.Run("negative_disables_cap", func(t *testing.T) {
		var arrays int
		parser, err := NewStreamParser(StreamParserConfig{
			Reader:   strings.NewReader(input),
			MaxDepth: -1,
			OnArray:  func([]any) error { arrays++; return nil },
		})
		require.NoError(t, err)
		assert.Less(t, parser.maxDepth, 0, "negative config disables the cap")

		require.NoError(t, parser.Parse())
		assert.Equal(t, 1, arrays)
	})
}

// liveBufferBytes reports how many bytes the parser currently holds across the
// top-level buffer and every open scope.
func liveBufferBytes(p *StreamParser) int {
	total := p.topBuf.Len()
	for _, buf := range p.buffers {
		total += buf.Len()
	}
	return total
}

// TestStreamParser_BufferBudgetAccounting verifies p.buffered counts exactly
// the bytes held for the in-flight value at every step — including top-level
// bytes dropped when a value starts, the cross-scope copies made when a nested
// value closes, and the reset after a value completes: it is the number the
// MaxBufferSize cap is enforced on.
func TestStreamParser_BufferBudgetAccounting(t *testing.T) {
	inputs := []string{
		// Nested completes, cross-scope copies, several top-level values.
		`{"a":[1,2,{"b":"c"}],"d":1} [true,false,[]] 42 {"e":{}}`,
		// Garbage before a scope: the top-level bytes are dropped when the
		// scope opens, so the budget must restart with them.
		`xyz{"a":1}`,
		// Top-level primitives separated by commas, each flushed in turn.
		`1,2,3`,
		// Deep nesting, one buffer per level.
		`[[[[1]]]]`,
		// A string as the only top-level value.
		`"only"`,
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			parser, err := NewStreamParser(StreamParserConfig{
				Reader:        strings.NewReader(""),
				MaxBufferSize: 4096,
				OnArray:       func([]any) error { return nil },
				OnObject:      func(map[string]any) error { return nil },
				OnValue:       func(any) error { return nil },
			})
			require.NoError(t, err)

			for i := range len(input) {
				require.NoError(t, parser.processBytes([]byte{input[i]}), "byte %d (%q)", i, input[i])
				require.Equal(t, liveBufferBytes(parser), parser.buffered,
					"buffered budget after byte %d (%q)", i, input[i])
			}
		})
	}
}

// TestStreamParser_MaxBufferSizeCountsOpenScopes verifies the cap counts the
// bytes of every open nesting scope, not just the innermost buffer: a nested
// value whose total exceeds the cap is rejected even though no single buffer
// reaches it.
func TestStreamParser_MaxBufferSizeCountsOpenScopes(t *testing.T) {
	const limit = 64

	// 20 one-byte scopes plus a 50-byte string: 71 buffered bytes at the
	// deepest point, none of them in a buffer that alone reaches the cap.
	input := strings.Repeat("[", 20) + `"` + strings.Repeat("x", 50) + `"` + strings.Repeat("]", 20)

	var notified []error
	parser, err := NewStreamParser(StreamParserConfig{
		Reader:        strings.NewReader(input),
		MaxBufferSize: limit,
		OnArray:       func([]any) error { t.Error("OnArray fired for an aborted value"); return nil },
		OnError:       func(err error) { notified = append(notified, err) },
	})
	require.NoError(t, err)

	err = parser.Parse()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json: buffer limit 64 bytes exceeded")
	require.Len(t, notified, 1, "the limit error reaches OnError once")
}
