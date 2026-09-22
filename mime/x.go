package mime

import (
	"strings"
	"sync"
)

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

// xPrefixMutex guards xPrefixSubtypeToStandard.
var xPrefixMutex sync.RWMutex

// RegisterXSubtype registers an "x-" subtype mapping consulted by
// [NormalizeXSubtype]. The key is lower-cased, since subtypes are lower-cased
// when parsed. Safe for concurrent use.
func RegisterXSubtype(xSubtype, standardSubtype string) {
	xPrefixMutex.Lock()
	defer xPrefixMutex.Unlock()
	xPrefixSubtypeToStandard[strings.ToLower(xSubtype)] = strings.ToLower(standardSubtype)
}

// RegisterXSubtypes is the batch form of [RegisterXSubtype].
func RegisterXSubtypes(mappings map[string]string) {
	xPrefixMutex.Lock()
	defer xPrefixMutex.Unlock()
	for xSubtype, standardSubtype := range mappings {
		xPrefixSubtypeToStandard[strings.ToLower(xSubtype)] = strings.ToLower(standardSubtype)
	}
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

	if !strings.HasPrefix(sourceMime.subType, "x-") {
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
