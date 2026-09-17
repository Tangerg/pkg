package json

import (
	"bytes"
	"testing"
)

// FuzzStreamParser feeds arbitrary bytes through the incremental JSON stream
// parser. The parser must never panic, regardless of how malformed the input
// is.
func FuzzStreamParser(f *testing.F) {
	seeds := []string{
		`{"name":"Alice","age":30}`,
		`[1,2,3]`,
		`123 "text" true null`,
		`{"a":{"b":[1,2,{"c":null}]}}`,
		`{"id":1}{"id":2}`,
		`"escaped \"quote\" and \\backslash"`,
		`{"unclosed":"`,
		`[1,2`,
		`{`,
		``,
		`}`,
		`{"a":1},{"b":[true,false]}`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		parser, err := NewStreamParser(StreamParserConfig{
			Reader:     bytes.NewReader(data),
			BufferSize: 16,
			// Small limits so arbitrary input exercises the cap and depth
			// abort paths, not just well-formed documents.
			MaxBufferSize: 64,
			MaxDepth:      4,
			OnObject:      func(map[string]any) error { return nil },
			OnArray:       func([]any) error { return nil },
			OnValue:       func(any) error { return nil },
			OnError:       func(error) {},
		})
		if err != nil {
			return
		}
		_ = parser.Parse()
	})
}
