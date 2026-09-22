package mime

import "testing"

// FuzzParse feeds arbitrary strings through Parse. The parser must never
// panic, and a value it accepts must survive a canonical round trip through
// [MIME.String]: the canonical form has to parse again to an equal MIME.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"text/html",
		"text/html; charset=UTF-8",
		`text/plain; name="file name.txt"`,
		`text/plain; name="a\";b"`,
		`text/plain; boundary="a;b"`,
		`text/html; charset="utf 8"`,
		"*",
		"*/*",
		"application/*+json",
		"application/vnd.api+json",
		"text/html;",
		"text/html;;charset=UTF-8;",
		"text/html; charset=",
		"text/html; charset",
		"text/html; =UTF-8",
		"text/html; charset=UTF-8; CHARSET=ISO-8859-1",
		"text/ht ml",
		"*/html",
		"text/",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, mimeString string) {
		parsed, err := Parse(mimeString)
		if err != nil {
			return
		}

		canonical := parsed.String()
		reparsed, err := Parse(canonical)
		if err != nil {
			t.Fatalf("Parse(%q) produced %q, which does not parse: %v", mimeString, canonical, err)
		}
		if !reparsed.Equals(parsed) {
			t.Fatalf("round trip of %q = %q, want %q", mimeString, reparsed.String(), canonical)
		}
	})
}
