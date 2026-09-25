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

// FuzzBuilder feeds arbitrary components through the [Builder]. Whatever Build
// accepts must render a canonical string that [Parse] accepts back and that
// compares equal to the built value: a wildcard primary type paired with a
// concrete subtype used to break exactly that.
func FuzzBuilder(f *testing.F) {
	seeds := []struct {
		mimeType   string
		subType    string
		paramKey   string
		paramValue string
	}{
		{mimeType: "text", subType: "html"},
		{mimeType: "text", subType: "*", paramKey: "charset", paramValue: "utf-8"},
		{mimeType: "*", subType: "*"},
		{mimeType: "*", subType: "json"},
		{mimeType: "", subType: "json"},
		{mimeType: "application", subType: "vnd.api+json", paramKey: "profile", paramValue: `"a b"`},
		{mimeType: "text", subType: "plain", paramKey: "name", paramValue: `"a\"b"`},
		{mimeType: "text", subType: "plain", paramKey: "name"},
		{mimeType: "text", subType: "plain", paramKey: "name", paramValue: `""`},
		{mimeType: "text", subType: "plain", paramKey: "charset", paramValue: `"utf 8"`},
		{mimeType: "text", subType: "plain", paramKey: "'name'", paramValue: "'a'"},
		{},
	}
	for _, seed := range seeds {
		f.Add(seed.mimeType, seed.subType, seed.paramKey, seed.paramValue)
	}

	f.Fuzz(func(t *testing.T, mimeType, subType, paramKey, paramValue string) {
		built, err := NewBuilder().
			WithType(mimeType).
			WithSubType(subType).
			WithParam(paramKey, paramValue).
			Build()
		if err != nil {
			return
		}

		canonical := built.String()
		reparsed, err := Parse(canonical)
		if err != nil {
			t.Fatalf("Build(%q, %q, %q, %q) produced %q, which does not parse: %v",
				mimeType, subType, paramKey, paramValue, canonical, err)
		}
		if !reparsed.Equals(built) {
			t.Fatalf("round trip of (%q, %q, %q, %q) = %q, want %q",
				mimeType, subType, paramKey, paramValue, reparsed.String(), canonical)
		}
	})
}
