package bufio

import "bytes"

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}

// ScanLinesAllFormats is a bufio.SplitFunc that splits on "\n", "\r",
// or "\r\n". The returned token excludes the line terminator and may
// be empty.
//
// Unlike bufio.ScanLines, it accepts classic Mac "\r" terminators in
// addition to Unix "\n" and Windows "\r\n", so the same scanner can
// process inputs from any platform.
//
// Example:
//
//	sc := bufio.NewScanner(r)
//	sc.Split(xbufio.ScanLinesAllFormats)
//	for sc.Scan() {
//	    fmt.Println(sc.Text())
//	}
func ScanLinesAllFormats(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	n := bytes.IndexByte(data, '\n')
	r := bytes.IndexByte(data, '\r')

	switch {
	case n >= 0 && r >= 0:
		// "\r\n" pair: consume both, return everything before \r.
		if n == r+1 {
			return n + 1, dropCR(data[:n]), nil
		}
		// Otherwise consume the earlier terminator.
		i := min(n, r)
		return i + 1, dropCR(data[:i]), nil
	case n >= 0:
		// A newline terminates the line. Return immediately even when
		// !atEOF so a trailing newline is never held back waiting for
		// more data.
		return n + 1, dropCR(data[:n]), nil
	case r >= 0:
		// A lone carriage return. If it is the final byte and more data
		// may follow, the next byte could be a newline forming a CRLF
		// pair, so ask for more input instead of emitting a token now.
		if r == len(data)-1 && !atEOF {
			return 0, nil, nil
		}
		return r + 1, dropCR(data[:r]), nil
	case atEOF:
		return len(data), data, nil
	default:
		return 0, nil, nil
	}
}
