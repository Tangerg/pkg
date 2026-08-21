package xml

import (
	"bytes"
	"testing"
)

// FuzzStreamScanner feeds arbitrary bytes through the streaming XML scanner
// across both strict and non-strict modes. The scanner must never panic,
// regardless of how malformed the input is.
func FuzzStreamScanner(f *testing.F) {
	seeds := []string{
		`<name>ToM</name>`,
		`<person id="1"><name>ToM</name><age>30</age></person>`,
		`<person id="1"/>`,
		`<a><b><c>deep</c></b></a>`,
		`plain text without elements`,
		`<name>`,
		`<name>unclosed`,
		`</name>`,
		`<person><name>mismatch</person></name>`,
		`><<>&"'`,
		`<name a="1" b='2'>x</name>`,
		`<name a=unquoted>x</name>`,
		`<a attr="<b>inside</b>">z</a>`,
		`<a><b></a>`,
		`  <a>  x  </a>  `,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		scanner, err := NewStreamScanner(StreamScannerConfig{
			Listeners: []*ElementListener{
				{Name: Name{Local: "name"}, OnComplete: func(Element) error { return nil }},
				{Name: Name{Local: "person"}, OnComplete: func(Element) error { return nil }},
			},
			OnText:     func(string) error { return nil },
			OnError:    func(error) {},
			StrictMode: len(data)%2 == 0,
			BufferSize: 7,
		})
		if err != nil {
			return
		}
		_ = scanner.Scan(bytes.NewReader(data))
	})
}
