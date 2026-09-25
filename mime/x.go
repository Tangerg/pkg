package mime

import (
	"fmt"
	"strings"
	"sync"
)

// xPrefix marks the legacy "x-" subtype prefix that RFC 6648 folds onto modern
// equivalents.
const xPrefix = "x-"

// xPrefixSubtypeToStandard maps legacy "x-" subtypes onto their RFC 6648
// standard counterparts. Only subtypes with a known equivalent are listed:
// [NormalizeXSubtype] leaves any other "x-" subtype unchanged, so an identity
// entry would be a no-op.
var xPrefixSubtypeToStandard = map[string]string{
	"x-javascript":      "javascript",
	"x-ecmascript":      "ecmascript",
	"x-latex":           "latex",
	"x-sh":              "sh",
	"x-perl":            "perl",
	"x-httpd-php":       "php",
	"x-httpd-cgi":       "cgi",
	"x-dvi":             "dvi",
	"x-gzip":            "gzip",
	"x-compressed":      "compressed",
	"x-zip-compressed":  "zip",
	"x-stuffit":         "stuffit",
	"x-rar-compressed":  "vnd.rar",
	"x-shockwave-flash": "vnd.adobe.flash-movie",
	"x-director":        "vnd.adobe.director",
	"x-msdos-program":   "vnd.microsoft.portable-executable",
	"x-wais-source":     "wais-source",
	"x-csh":             "csh",
	"x-python":          "python",
	"x-ruby":            "ruby",
	"x-json":            "json",
	"x-bytecode.python": "python-bytecode",
	"x-yaml":            "yaml",
	"x-ole-storage":     "vnd.ms-ole-storage",
	"x-tcl":             "tcl",
	"x-pkcs7-signature": "pkcs7-signature",
	"x-pkcs7-mime":      "pkcs7-mime",
	"x-mpeg":            "mpeg",
	"x-mp3":             "mpeg",
	"x-wav":             "wav",
	"x-midi":            "midi",
	"x-aiff":            "aiff",
	"x-realaudio":       "vnd.rn-realaudio",
	"x-pn-realaudio":    "vnd.rn-realaudio",
	"x-ogg":             "ogg",
	"x-flac":            "flac",
	"x-ac3":             "ac3",
	"x-m4a":             "mp4",
	"x-m4r":             "mp4",
	"x-aac":             "aac",
	"x-png":             "png",
	"x-icon":            "vnd.microsoft.icon",
	"x-ms-bmp":          "bmp",
	"x-tiff":            "tiff",
	"x-photoshop":       "vnd.adobe.photoshop",
	"x-webp":            "webp",
	"x-windows-bmp":     "bmp",
	"x-markdown":        "markdown",
	"x-vcard":           "vcard",
	"x-vcalendar":       "calendar",
	"x-csv":             "csv",
	"x-sgml":            "sgml",
	"x-component":       "html-component",
	"x-ms-asf":          "vnd.ms-asf",
	"x-m4v":             "mp4",
	"x-quicktime":       "quicktime",
}

var xPrefixMutex sync.RWMutex

// RegisterXSubtype registers an "x-" subtype mapping consulted by
// [NormalizeXSubtype]. The key and its target are lower-cased, since subtypes
// are lower-cased when parsed. Returns an error for a key without the "x-"
// prefix, which could never match, and for a target that is not a non-empty
// token: [NormalizeXSubtype] substitutes the target into a subtype, so the
// result has to be a MIME whose [MIME.String] [Parse] accepts back. Safe for
// concurrent use.
func RegisterXSubtype(xSubtype, standardSubtype string) error {
	normalizedKey, normalizedSubtype, err := normalizeXSubtypeMapping(xSubtype, standardSubtype)
	if err != nil {
		return err
	}

	xPrefixMutex.Lock()
	defer xPrefixMutex.Unlock()
	xPrefixSubtypeToStandard[normalizedKey] = normalizedSubtype

	return nil
}

// RegisterXSubtypes is the batch form of [RegisterXSubtype]. Every entry is
// validated up front; on the first invalid or duplicated key no mapping is
// installed (all-or-nothing). Keys that differ only in case are rejected. Safe
// for concurrent use.
func RegisterXSubtypes(mappings map[string]string) error {
	registered := make(map[string]string, len(mappings))
	spelledKeys := make(map[string]string, len(mappings))

	for xSubtype, standardSubtype := range mappings {
		normalizedKey, normalizedSubtype, err := normalizeXSubtypeMapping(xSubtype, standardSubtype)
		if err != nil {
			return err
		}
		if otherKey, duplicate := spelledKeys[normalizedKey]; duplicate {
			return fmt.Errorf("duplicate x-subtype %q: %q and %q", normalizedKey, otherKey, xSubtype)
		}

		spelledKeys[normalizedKey] = xSubtype
		registered[normalizedKey] = normalizedSubtype
	}

	xPrefixMutex.Lock()
	defer xPrefixMutex.Unlock()
	for xSubtype, normalizedSubtype := range registered {
		xPrefixSubtypeToStandard[xSubtype] = normalizedSubtype
	}

	return nil
}

// normalizeXSubtypeMapping validates one registration and returns the lower-cased
// key and target to store.
func normalizeXSubtypeMapping(xSubtype, standardSubtype string) (string, string, error) {
	normalizedKey := strings.ToLower(xSubtype)
	if !strings.HasPrefix(normalizedKey, xPrefix) {
		return "", "", fmt.Errorf("invalid x-subtype %q: want a %q prefix", xSubtype, xPrefix)
	}

	normalizedSubtype := strings.ToLower(standardSubtype)
	if normalizedSubtype == "" {
		return "", "", fmt.Errorf("invalid target for x-subtype %q: empty subtype", xSubtype)
	}
	if char, invalid := tokenInvalidChar(normalizedSubtype); invalid {
		return "", "", fmt.Errorf("invalid character %s in target for x-subtype %q: %s",
			string(char), xSubtype, standardSubtype)
	}

	return normalizedKey, normalizedSubtype, nil
}

// NormalizeXSubtype returns a copy of sourceMime with its "x-" subtype
// rewritten to the modern equivalent registered for it. Subtypes without an
// "x-" prefix, and "x-" subtypes with no registered mapping, are returned as
// copies unchanged: dropping the prefix would fabricate a type that does not
// exist. A nil source returns nil.
func NormalizeXSubtype(sourceMime *MIME) *MIME {
	if sourceMime == nil {
		return nil
	}

	if !strings.HasPrefix(sourceMime.subType, xPrefix) {
		return sourceMime.Clone()
	}

	xPrefixMutex.RLock()
	normalizedSubtype, hasMapping := xPrefixSubtypeToStandard[sourceMime.subType]
	xPrefixMutex.RUnlock()

	if !hasMapping {
		return sourceMime.Clone()
	}

	return sourceMime.withSubType(normalizedSubtype)
}
