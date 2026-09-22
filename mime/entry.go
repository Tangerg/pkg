package mime

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gabriel-vasile/mimetype"

	"github.com/Tangerg/pkg/assert"
	"github.com/Tangerg/pkg/maps"
)

// Category prototypes used by the Is* helpers; only their type and
// subtype fields are consulted.
var (
	all         = MIME{_type: wildcardType, subType: wildcardType}
	text        = MIME{_type: "text", subType: wildcardType}
	video       = MIME{_type: "video", subType: wildcardType}
	audio       = MIME{_type: "audio", subType: wildcardType}
	image       = MIME{_type: "image", subType: wildcardType}
	application = MIME{_type: "application", subType: wildcardType}
)

// ErrorInvalidMimeType is returned by [Parse] when the input does not
// conform to RFC 2045 / 2046 syntax.
var ErrorInvalidMimeType = errors.New("invalid mime type")

// New returns a [MIME] with the given primary type and subtype and no
// parameters.
func New(mimeType string, subType string) (*MIME, error) {
	return NewBuilder().
		WithType(mimeType).
		WithSubType(subType).
		Build()
}

// MustNew is like [New] but panics on error.
func MustNew(mimeType string, subType string) *MIME {
	return assert.Must(New(mimeType, subType))
}

// Parse decodes a MIME type string such as "text/html; charset=UTF-8"
// into a [MIME]. A bare "*" is treated as "*/*". Double-quoted parameter values
// (RFC 2045) are decoded and re-encoded in canonical spelling; [MIME.Param] and
// [MIME.String] therefore work with the value rather than the quoting.
//
// Malformed parameters are reported instead of dropped: a parameter without
// "=", a parameter without a name, or a repeated parameter name (compared
// case-insensitively) returns [ErrorInvalidMimeType]. An empty value and empty
// segments, such as a trailing ";", are accepted.
func Parse(mimeString string) (*MIME, error) {
	segments := paramSegments(mimeString)
	typeSubtypeString := strings.TrimSpace(segments[0])

	if typeSubtypeString == "" {
		return nil, fmt.Errorf("%w: 'mime type' must not be empty", ErrorInvalidMimeType)
	}

	// Handle the special case of "*" as shorthand for "*/*"
	if typeSubtypeString == wildcardType {
		typeSubtypeString = "*/*"
	}

	slashIndex := strings.Index(typeSubtypeString, "/")
	if slashIndex == -1 {
		return nil, fmt.Errorf("%w: does not contain '/'", ErrorInvalidMimeType)
	}
	if slashIndex == len(typeSubtypeString)-1 {
		return nil, fmt.Errorf("%w: does not contain subtype after '/'", ErrorInvalidMimeType)
	}

	primaryType := normalizeTypeComponent(typeSubtypeString[:slashIndex])
	subType := normalizeTypeComponent(typeSubtypeString[slashIndex+1:])

	// Quoting is stripped by the builder, so an empty component has to be
	// rejected here rather than silently become the wildcard default.
	if primaryType == "" {
		return nil, fmt.Errorf("%w: does not contain type before '/'", ErrorInvalidMimeType)
	}
	if subType == "" {
		return nil, fmt.Errorf("%w: does not contain subtype after '/'", ErrorInvalidMimeType)
	}

	if primaryType == wildcardType && subType != wildcardType {
		return nil, fmt.Errorf("%w: wildcard type is legal only in '*/*' (all mime types)", ErrorInvalidMimeType)
	}

	parameterMap := maps.NewHashMap[string, string]()
	for _, segment := range segments[1:] {
		parameterString := strings.TrimSpace(segment)
		if parameterString == "" {
			continue
		}

		rawKey, rawValue, hasValue := strings.Cut(parameterString, "=")
		if !hasValue {
			return nil, fmt.Errorf("%w: parameter %q has no '='", ErrorInvalidMimeType, parameterString)
		}

		paramKey := normalizeParamKey(strings.TrimSpace(rawKey))
		if paramKey == "" {
			return nil, fmt.Errorf("%w: parameter %q has no name", ErrorInvalidMimeType, parameterString)
		}
		if parameterMap.ContainsKey(paramKey) {
			return nil, fmt.Errorf("%w: duplicate parameter %q", ErrorInvalidMimeType, paramKey)
		}

		parameterMap.Put(paramKey, strings.TrimSpace(rawValue))
	}

	mimeResult, err := NewBuilder().
		WithType(primaryType).
		WithSubType(subType).
		WithParams(parameterMap).
		Build()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrorInvalidMimeType, err)
	}

	return mimeResult, nil
}

// paramSegments splits a MIME string on the ';' separators outside quoted
// strings, whose first segment is the type/subtype part. A backslash inside a
// quoted string escapes the next character, so an escaped quote does not end
// the value.
func paramSegments(mimeString string) []string {
	segments := make([]string, 0, 3)
	segmentStart := 0
	isQuoted := false

	for i := 0; i < len(mimeString); i++ {
		switch {
		case isQuoted && mimeString[i] == '\\' && i+1 < len(mimeString):
			i++
		case mimeString[i] == '"':
			isQuoted = !isQuoted
		case mimeString[i] == ';' && !isQuoted:
			segments = append(segments, mimeString[segmentStart:i])
			segmentStart = i + 1
		}
	}

	return append(segments, mimeString[segmentStart:])
}

// Detect returns the MIME type inferred from the magic bytes of
// dataBytes.
func Detect(dataBytes []byte) (*MIME, error) {
	detectedMime := mimetype.Detect(dataBytes)
	return Parse(detectedMime.String())
}

// DetectReader returns the MIME type inferred from the leading bytes
// of reader. Only a small prefix is consumed.
func DetectReader(reader io.Reader) (*MIME, error) {
	detectedMime, err := mimetype.DetectReader(reader)
	if err != nil {
		return nil, err
	}
	return Parse(detectedMime.String())
}

// DetectFile returns the MIME type inferred from the contents of the
// file at filePath.
func DetectFile(filePath string) (*MIME, error) {
	detectedMime, err := mimetype.DetectFile(filePath)
	if err != nil {
		return nil, err
	}
	return Parse(detectedMime.String())
}

// IsVideo reports whether mimeType has primary type "video".
func IsVideo(mimeType *MIME) bool {
	return video.EqualsType(mimeType)
}

// IsAudio reports whether mimeType has primary type "audio".
func IsAudio(mimeType *MIME) bool {
	return audio.EqualsType(mimeType)
}

// IsImage reports whether mimeType has primary type "image".
func IsImage(mimeType *MIME) bool {
	return image.EqualsType(mimeType)
}

// IsText reports whether mimeType has primary type "text".
func IsText(mimeType *MIME) bool {
	return text.EqualsType(mimeType)
}

// IsApplication reports whether mimeType has primary type "application".
func IsApplication(mimeType *MIME) bool {
	return application.EqualsType(mimeType)
}
