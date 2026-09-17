package bufio

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDropCR(t *testing.T) {
	tests := []struct {
		in, want []byte
	}{
		{nil, nil},
		{[]byte{}, []byte{}},
		{[]byte("hello\r"), []byte("hello")},
		{[]byte("hello"), []byte("hello")},
		{[]byte("hello\n"), []byte("hello\n")},
		{[]byte("\r"), []byte{}},
		{[]byte("hel\rlo"), []byte("hel\rlo")},
	}
	for _, tt := range tests {
		got := dropCR(tt.in)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("dropCR(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScanLinesAllFormats_OneShot(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		atEOF       bool
		wantAdvance int
		wantToken   string
		wantNoToken bool
	}{
		{"empty at eof", "", true, 0, "", true},
		{"empty not eof needs more", "", false, 0, "", true},
		{"LF", "hello\n", false, 6, "hello", false},
		{"empty LF", "\n", false, 1, "", false},
		{"CR not eof needs more", "hello\r", false, 0, "", true},
		{"CR at eof", "hello\r", true, 6, "hello", false},
		{"CRLF", "hello\r\n", false, 7, "hello", false},
		{"LF before CR", "hello\nworld\r", false, 6, "hello", false},
		{"CR before LF non-paired", "hello\rworld\n", false, 6, "hello", false},
		{"no terminator at eof", "tail", true, 4, "tail", false},
		{"no terminator not eof", "tail", false, 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advance, token, err := ScanLinesAllFormats([]byte(tt.data), tt.atEOF)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if advance != tt.wantAdvance {
				t.Errorf("advance = %d, want %d", advance, tt.wantAdvance)
			}
			if tt.wantNoToken {
				if token != nil {
					t.Errorf("token = %q, want nil", token)
				}
				return
			}
			if string(token) != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func scanAll(t *testing.T, in string) []string {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(in))
	sc.Split(ScanLinesAllFormats)
	var out []string
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan err: %v", err)
	}
	return out
}

func TestScanLinesAllFormats_Scanner(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"unix", "a\nb\nc\n", []string{"a", "b", "c"}},
		{"windows", "a\r\nb\r\nc\r\n", []string{"a", "b", "c"}},
		{"old mac", "a\rb\rc\r", []string{"a", "b", "c"}},
		{"mixed", "unix\nwindows\r\nmac\r", []string{"unix", "windows", "mac"}},
		{"trailing no eol", "a\nb\nc", []string{"a", "b", "c"}},
		{"empty lines", "\n\n\n", []string{"", "", ""}},
		{"empty input", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanAll(t, tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// chunkReader delivers its data in fixed-size chunks across Read calls,
// allowing a terminator to straddle a read boundary.
type chunkReader struct {
	data []byte
	step int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(c.step, len(p), len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

func scanAllChunked(t *testing.T, in string, step int) []string {
	t.Helper()
	sc := bufio.NewScanner(&chunkReader{data: []byte(in), step: step})
	sc.Split(ScanLinesAllFormats)
	var out []string
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan err: %v", err)
	}
	return out
}

func TestScanLinesAllFormats_Chunked(t *testing.T) {
	tests := []struct {
		name string
		in   string
		step int
		want []string
	}{
		{"crlf split by 2", "a\r\nb\r\n", 2, []string{"a", "b"}},
		{"crlf split by 1", "a\r\nb\r\n", 1, []string{"a", "b"}},
		{"lone cr split by 2", "a\rb\rc", 2, []string{"a", "b", "c"}},
		{"lone cr split by 1", "a\rb\rc", 1, []string{"a", "b", "c"}},
		{"trailing cr at eof", "a\rb\r", 2, []string{"a", "b"}},
		{"bare lf split by 1", "a\nb\n", 1, []string{"a", "b"}},
		{"no terminator", "abc", 1, []string{"abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanAllChunked(t, tt.in, tt.step)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("plain lf does not wait for eof", func(t *testing.T) {
		pr, pw := io.Pipe()
		defer pr.Close()
		defer pw.Close()
		go func() { _, _ = pw.Write([]byte("a\n")) }()

		done := make(chan string, 1)
		go func() {
			sc := bufio.NewScanner(pr)
			sc.Split(ScanLinesAllFormats)
			if sc.Scan() {
				done <- sc.Text()
			} else {
				done <- ""
			}
		}()

		select {
		case got := <-done:
			if got != "a" {
				t.Errorf("got %q, want %q", got, "a")
			}
		case <-time.After(time.Second):
			t.Fatal("scanner stalled on trailing newline without eof")
		}
	})
}

func BenchmarkScanLinesAllFormats(b *testing.B) {
	in := strings.Repeat("line\r\n", 1000)
	for b.Loop() {
		sc := bufio.NewScanner(strings.NewReader(in))
		sc.Split(ScanLinesAllFormats)
		for sc.Scan() {
		}
	}
}
