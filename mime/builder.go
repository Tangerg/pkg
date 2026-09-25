package mime

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bits-and-blooms/bitset"

	"github.com/Tangerg/pkg/assert"
	"github.com/Tangerg/pkg/maps"
)

// tokenBitSet marks the ASCII characters allowed in a MIME token: RFC 2045
// excludes control characters, space, tab, and tspecials, and this table is
// stricter by also excluding "{" and "}".
var tokenBitSet *bitset.BitSet

func init() {
	controlChars := bitset.New(128)
	for i := uint(0); i <= 31; i++ {
		controlChars.Set(i)
	}
	controlChars.Set(127)

	separatorChars := bitset.New(128)
	// ASCII codes of the characters a token must not contain: RFC 2045
	// tspecials, plus space, tab, and the "{" and "}" tokenBitSet excludes.
	separatorPositions := []uint{40, 41, 60, 62, 64, 44, 59, 58, 92, 34, 47, 91, 93, 63, 61, 123, 125, 32, 9}
	for _, position := range separatorPositions {
		separatorChars.Set(position)
	}

	validTokenChars := bitset.New(128)
	for i := uint(0); i < 128; i++ {
		validTokenChars.Set(i)
	}

	validTokenChars = validTokenChars.Difference(controlChars)
	validTokenChars = validTokenChars.Difference(separatorChars)
	tokenBitSet = validTokenChars
}

// Builder constructs a [MIME] using a fluent, validating API.
// A zero Builder is not usable; obtain one from [NewBuilder].
type Builder struct {
	mime *MIME
}

// NewBuilder returns a [Builder] initialized with wildcard type and
// subtype ("*/*") and no parameters.
func NewBuilder() *Builder {
	return &Builder{
		mime: &MIME{
			_type:   wildcardType,
			subType: wildcardType,
			params:  maps.NewHashMap[string, string](),
		},
	}
}

// tokenInvalidChar returns the first rune of token that RFC 2045 does not
// allow in a token, and whether such a rune exists.
func tokenInvalidChar(token string) (rune, bool) {
	for _, char := range token {
		if !tokenBitSet.Test(uint(char)) {
			return char, true
		}
	}
	return 0, false
}

// isToken reports whether token contains only token characters.
func isToken(token string) bool {
	_, invalid := tokenInvalidChar(token)
	return !invalid
}

// checkToken returns an error if token contains a non-token character.
func (b *Builder) checkToken(token string) error {
	if char, invalid := tokenInvalidChar(token); invalid {
		return fmt.Errorf("invalid character %s in token: %s", string(char), token)
	}
	return nil
}

// checkParam validates a parameter key/value pair. The key must be a token; the
// value must be a token or a double-quoted string whose contents are accepted
// without further checks.
func (b *Builder) checkParam(paramKey string, paramValue string) error {
	if err := b.checkToken(paramKey); err != nil {
		return err
	}

	if isQuotedSpelling(paramValue) {
		return nil
	}

	return b.checkToken(paramValue)
}

// checkParams validates every parameter currently set on the builder.
func (b *Builder) checkParams() error {
	for paramKey, paramValue := range b.mime.params {
		if err := b.checkParam(paramKey, paramValue); err != nil {
			return err
		}
	}
	return nil
}

// WithType sets the primary type, lower-casing the input and stripping
// surrounding double quotes.
func (b *Builder) WithType(mimeType string) *Builder {
	b.mime._type = normalizeTypeComponent(mimeType)
	return b
}

// WithSubType sets the subtype, lower-casing the input and stripping
// surrounding double quotes.
func (b *Builder) WithSubType(mimeSubType string) *Builder {
	b.mime.subType = normalizeTypeComponent(mimeSubType)
	return b
}

// WithCharset sets the charset parameter, upper-casing the value because
// charset names are case-insensitive. A double-quoted value is decoded first;
// an empty value is a no-op, so it cannot clear an existing charset.
func (b *Builder) WithCharset(charsetValue string) *Builder {
	normalizedCharset := strings.ToUpper(decodeParamValue(charsetValue))
	if normalizedCharset == "" {
		return b
	}

	spelling := normalizedCharset
	if isQuotedSpelling(charsetValue) {
		spelling = encodeParamValue(normalizedCharset)
	}

	b.mime.params.Put(paramCharset, spelling)
	return b
}

// WithParam adds a parameter. The key is lower-cased and stripped of
// surrounding double quotes; an empty key is a no-op. A "charset" key is
// forwarded to [Builder.WithCharset]. A double-quoted value is decoded and
// re-encoded in canonical spelling; an unquoted value has to be a token, which
// [Builder.Build] verifies.
func (b *Builder) WithParam(paramKey string, paramValue string) *Builder {
	normalizedKey := normalizeParamKey(paramKey)
	if normalizedKey == "" {
		return b
	}

	if normalizedKey == paramCharset {
		return b.WithCharset(paramValue)
	}

	if isQuotedSpelling(paramValue) {
		paramValue = encodeParamValue(decodeParamValue(paramValue))
	}

	b.mime.params.Put(normalizedKey, paramValue)
	return b
}

// WithParams adds every entry of paramMap via [Builder.WithParam].
func (b *Builder) WithParams(paramMap map[string]string) *Builder {
	// Applying in a fixed order keeps the result stable when the map holds two
	// spellings of one parameter name.
	paramKeys := make([]string, 0, len(paramMap))
	for paramKey := range paramMap {
		paramKeys = append(paramKeys, paramKey)
	}
	slices.Sort(paramKeys)

	for _, paramKey := range paramKeys {
		b.WithParam(paramKey, paramMap[paramKey])
	}
	return b
}

// FromMime copies type, subtype, and parameters from sourceMime into the
// builder. A nil source is a no-op. The canonical string is not copied; the
// next [Builder.Build] derives it from the copied components.
func (b *Builder) FromMime(sourceMime *MIME) *Builder {
	if sourceMime == nil {
		return b
	}

	b.mime._type = sourceMime._type
	b.mime.subType = sourceMime.subType
	b.mime.params = sourceMime.params.Clone().(maps.HashMap[string, string])

	return b
}

// Build validates the configured components and returns the assembled [MIME].
// Empty type or subtype default to "*"; a wildcard primary type is legal only
// in "*/*", so an empty type combines only with a wildcard subtype. An invalid
// token in any component produces an error. The result shares no state with the
// builder.
func (b *Builder) Build() (*MIME, error) {
	if b.mime._type == "" {
		b.mime._type = wildcardType
	} else if err := b.checkToken(b.mime._type); err != nil {
		return nil, err
	}

	if b.mime.subType == "" {
		b.mime.subType = wildcardType
	} else if err := b.checkToken(b.mime.subType); err != nil {
		return nil, err
	}

	// A wildcard primary type covers every subtype in [MIME.Includes], so it has
	// no meaning outside "*/*". [Parse] builds through here and relies on this
	// check: without it, "*" with a concrete subtype would render a canonical
	// string that [Parse] rejects.
	if b.mime._type == wildcardType && b.mime.subType != wildcardType {
		return nil, errors.New("wildcard type is legal only in '*/*' (all mime types)")
	}

	if err := b.checkParams(); err != nil {
		return nil, err
	}

	built := *b.mime
	built.params = b.mime.params.Clone().(maps.HashMap[string, string])
	built.cachedString = built.formatStringValue()

	return &built, nil
}

// MustBuild calls [Builder.Build] and panics on error.
func (b *Builder) MustBuild() *MIME {
	return assert.Must(b.Build())
}
