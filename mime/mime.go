package mime

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Tangerg/pkg/maps"
	pkgStrings "github.com/Tangerg/pkg/strings"
)

const (
	wildcardType = "*"
	paramCharset = "charset"
)

// MIME represents a parsed MIME type: a primary type, a subtype, and
// parameters such as the charset. Construct values with [New], [Parse], or
// [Builder]; a built value is immutable and safe for concurrent use.
type MIME struct {
	_type   string
	subType string
	// params maps each parameter name to its canonical spelling: a bare token,
	// or a quoted string when the value cannot be a token. Accessors decode the
	// spelling; [MIME.String] emits it verbatim.
	params maps.HashMap[string, string]
	// cachedString is the [MIME.String] result, computed once by the builder.
	// Values assembled without a builder derive it on every call.
	cachedString string
}

// MarshalJSON encodes m as its canonical string form, JSON-quoted.
func (m *MIME) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return json.Marshal(m.String())
}

// UnmarshalJSON decodes a JSON-quoted MIME type string into m. A nil receiver
// is an error rather than a panic.
func (m *MIME) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("mime: UnmarshalJSON called on a nil *MIME")
	}

	var mimeString string
	if err := json.Unmarshal(data, &mimeString); err != nil {
		return err
	}

	parsed, err := Parse(mimeString)
	if err != nil {
		return err
	}

	*m = *parsed

	return nil
}

// formatStringValue renders the "type/subtype;k=v" form. Parameters are
// emitted in ascending key order so the result depends only on the components.
func (m *MIME) formatStringValue() string {
	var stringBuilder strings.Builder

	stringBuilder.WriteString(m._type)
	stringBuilder.WriteString("/")
	stringBuilder.WriteString(m.subType)

	paramKeys := m.params.Keys()
	slices.Sort(paramKeys)
	for _, paramKey := range paramKeys {
		paramValue, _ := m.params.Get(paramKey)
		stringBuilder.WriteString(";")
		stringBuilder.WriteString(paramKey)
		stringBuilder.WriteString("=")
		stringBuilder.WriteString(paramValue)
	}

	return stringBuilder.String()
}

// Type returns the primary type, e.g. "text" for "text/html".
func (m *MIME) Type() string {
	return m._type
}

// SubType returns the subtype, e.g. "html" for "text/html".
func (m *MIME) SubType() string {
	return m.subType
}

// TypeAndSubType returns "type/subtype" without parameters.
func (m *MIME) TypeAndSubType() string {
	return m._type + "/" + m.subType
}

// FullType is an alias for [MIME.TypeAndSubType].
func (m *MIME) FullType() string {
	return m.TypeAndSubType()
}

// Charset returns the decoded charset parameter value, or "" if unset.
func (m *MIME) Charset() string {
	charsetValue, _ := m.params.Get(paramCharset)
	return decodeParamValue(charsetValue)
}

// Param returns the decoded value of the named parameter and whether it is set.
func (m *MIME) Param(paramKey string) (string, bool) {
	paramValue, ok := m.params.Get(paramKey)
	return decodeParamValue(paramValue), ok
}

// Params returns a copy of the parameters with values decoded. Mutating the
// result does not affect m.
func (m *MIME) Params() map[string]string {
	params := make(map[string]string, m.params.Size())
	for paramKey, paramValue := range m.params {
		params[paramKey] = decodeParamValue(paramValue)
	}
	return params
}

// String returns the canonical "type/subtype;k=v" form, with parameters in
// ascending key order and values in canonical spelling. A nil receiver
// returns "".
func (m *MIME) String() string {
	if m == nil {
		return ""
	}
	if m.cachedString != "" {
		return m.cachedString
	}
	return m.formatStringValue()
}

// stripQuotes removes every layer of surrounding quotes. Single-layer
// [pkgStrings.UnQuote] would not be idempotent, and a value that stays quoted
// after one pass could be emptied by the next one.
func stripQuotes(value string) string {
	for pkgStrings.IsQuoted(value) {
		value = pkgStrings.UnQuote(value)
	}
	return value
}

// normalizeTypeComponent returns the primary type or subtype the builder
// stores: lower-cased, with every layer of surrounding quotes removed.
func normalizeTypeComponent(component string) string {
	return stripQuotes(strings.ToLower(component))
}

// normalizeParamKey returns the name a parameter is stored under: lower-cased,
// with every layer of surrounding quotes removed. Spellings that normalize to
// the same name denote the same parameter.
func normalizeParamKey(paramKey string) string {
	return stripQuotes(strings.ToLower(paramKey))
}

// isQuotedSpelling reports whether value is written as an RFC 2045
// quoted-string, which uses the double quote only. A single-quoted spelling is
// deliberately not recognized: "'" is a valid token character, so reading runs
// of it as quoting would make a spelled value ambiguous when it is parsed back.
func isQuotedSpelling(value string) bool {
	return len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"'
}

// decodeParamValue returns the value a parameter spelling denotes: a quoted
// spelling loses its surrounding quotes and the backslashes that escape the
// character after them.
func decodeParamValue(spelling string) string {
	if !isQuotedSpelling(spelling) {
		return spelling
	}

	quoted := spelling[1 : len(spelling)-1]
	if !strings.ContainsRune(quoted, '\\') {
		return quoted
	}

	var decoded strings.Builder
	decoded.Grow(len(quoted))
	for i := 0; i < len(quoted); i++ {
		if quoted[i] == '\\' && i+1 < len(quoted) {
			i++
		}
		decoded.WriteByte(quoted[i])
	}

	return decoded.String()
}

// encodeParamValue returns the canonical spelling of a parameter value: the
// value itself when it is a token, otherwise a quoted string with '"' and '\'
// escaped.
func encodeParamValue(value string) string {
	if isToken(value) {
		return value
	}

	var encoded strings.Builder
	encoded.Grow(len(value) + 2)
	encoded.WriteByte('"')
	for i := 0; i < len(value); i++ {
		if value[i] == '"' || value[i] == '\\' {
			encoded.WriteByte('\\')
		}
		encoded.WriteByte(value[i])
	}
	encoded.WriteByte('"')

	return encoded.String()
}

// IsWildcardType reports whether the primary type is "*".
func (m *MIME) IsWildcardType() bool {
	return m._type == wildcardType
}

// IsWildcardSubType reports whether the subtype is "*" or "*+suffix".
func (m *MIME) IsWildcardSubType() bool {
	return m.subType == wildcardType || strings.HasPrefix(m.subType, "*+")
}

// IsConcrete reports whether neither the type nor the subtype is a
// wildcard.
func (m *MIME) IsConcrete() bool {
	return !m.IsWildcardType() && !m.IsWildcardSubType()
}

// GetSubtypeSuffix returns the part after the last '+' in the subtype,
// or "" if none. For "application/vnd.api+json" it returns "json".
func (m *MIME) GetSubtypeSuffix() string {
	plusIndex := strings.LastIndexByte(m.subType, '+')
	if plusIndex != -1 && len(m.subType) > plusIndex {
		return m.subType[plusIndex+1:]
	}
	return ""
}

// Includes reports whether m is a superset of otherMime: the type and subtype
// must cover otherMime under the MIME wildcard rules, and every parameter of m
// must be present in otherMime with the same value. For example, "text/*"
// includes "text/html", "application/*+json" includes
// "application/vnd.api+json", and "text/html;charset=UTF-8" includes
// "text/html;charset=UTF-8;version=1" but not a bare "text/html".
func (m *MIME) Includes(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}

	return m.coversTypeAndSubtype(otherMime) && m.paramsIncludedIn(otherMime)
}

// coversTypeAndSubtype reports whether m's type and subtype cover otherMime's
// under the MIME wildcard rules.
func (m *MIME) coversTypeAndSubtype(otherMime *MIME) bool {
	if m.IsWildcardType() {
		return true
	}

	if !m.EqualsType(otherMime) {
		return false
	}

	if m.EqualsSubtype(otherMime) {
		return true
	}

	if !m.IsWildcardSubType() {
		return false
	}

	plusIndex := strings.LastIndexByte(m.subType, '+')
	if plusIndex == -1 {
		return true
	}

	// m is a "*+suffix" pattern, so the part before '+' is the wildcard itself.
	otherPlusIndex := strings.LastIndexByte(otherMime.subType, '+')
	if otherPlusIndex == -1 {
		return false
	}

	return m.subType[plusIndex+1:] == otherMime.subType[otherPlusIndex+1:]
}

// paramsIncludedIn reports whether every parameter of m is present in
// otherMime with the same decoded value.
func (m *MIME) paramsIncludedIn(otherMime *MIME) bool {
	for paramKey, paramValue := range m.params {
		otherValue, ok := otherMime.params.Get(paramKey)
		if !ok || decodeParamValue(paramValue) != decodeParamValue(otherValue) {
			return false
		}
	}

	return true
}

// IsCompatibleWith reports whether either value includes the other, so a
// wildcard or a parameter-less value is compatible with a narrower one.
func (m *MIME) IsCompatibleWith(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return m.Includes(otherMime) || otherMime.Includes(m)
}

// EqualsType reports whether the primary types match.
func (m *MIME) EqualsType(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return m._type == otherMime._type
}

// EqualsSubtype reports whether the subtypes match.
func (m *MIME) EqualsSubtype(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return m.subType == otherMime.subType
}

// EqualsTypeAndSubtype reports whether both type and subtype match,
// ignoring parameters.
func (m *MIME) EqualsTypeAndSubtype(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return m.EqualsType(otherMime) && m.EqualsSubtype(otherMime)
}

// EqualsParams reports whether both MIMEs carry the same parameters, comparing
// decoded values.
func (m *MIME) EqualsParams(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}

	if m.params.Size() != otherMime.params.Size() {
		return false
	}

	for paramKey, paramValue := range m.params {
		otherValue, ok := otherMime.params.Get(paramKey)
		if !ok || decodeParamValue(paramValue) != decodeParamValue(otherValue) {
			return false
		}
	}

	return true
}

// EqualsCharset reports whether the charset values match.
func (m *MIME) EqualsCharset(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return m.Charset() == otherMime.Charset()
}

// Equals reports whether m and otherMime have the same type, subtype,
// charset, and parameters.
func (m *MIME) Equals(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return m.EqualsTypeAndSubtype(otherMime) &&
		m.EqualsCharset(otherMime) &&
		m.EqualsParams(otherMime)
}

// IsPresentIn reports whether any type in mimeList includes m, so an entry may
// be a wildcard ("text/*") or carry parameters that m has to satisfy. A nil
// entry never matches. Use [MIME.EqualsTypeAndSubtype] for literal equality of
// type and subtype.
func (m *MIME) IsPresentIn(mimeList []*MIME) bool {
	for _, mimeType := range mimeList {
		if mimeType == nil {
			continue
		}
		if mimeType.Includes(m) {
			return true
		}
	}
	return false
}

// IsMoreSpecific reports whether m is strictly more specific than
// otherMime: concrete components beat wildcards, and for equal
// type/subtype the value with more parameters wins.
func (m *MIME) IsMoreSpecific(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}

	// Check type specificity
	if m.IsWildcardType() && !otherMime.IsWildcardType() {
		return false
	}
	if !m.IsWildcardType() && otherMime.IsWildcardType() {
		return true
	}

	// Check subtype specificity
	if m.IsWildcardSubType() && !otherMime.IsWildcardSubType() {
		return false
	}
	if !m.IsWildcardSubType() && otherMime.IsWildcardSubType() {
		return true
	}

	if m.EqualsTypeAndSubtype(otherMime) {
		return m.params.Size() > otherMime.params.Size()
	}

	return false
}

// IsLessSpecific reports whether m is strictly less specific than otherMime,
// that is, whether otherMime is more specific than m. Equally specific and
// uncomparable values are false for both predicates.
func (m *MIME) IsLessSpecific(otherMime *MIME) bool {
	if otherMime == nil {
		return false
	}
	return otherMime.IsMoreSpecific(m)
}

// Clone returns a copy of m with its own parameter map. A nil receiver returns
// nil.
func (m *MIME) Clone() *MIME {
	if m == nil {
		return nil
	}

	cloned := *m
	cloned.params = m.params.Clone().(maps.HashMap[string, string])

	return &cloned
}

// withSubType returns a copy of m with subType replaced, without validation.
// It derives a new value from a known-good one, as [NormalizeXSubtype] does
// with its registered mappings.
func (m *MIME) withSubType(subType string) *MIME {
	derived := m.Clone()
	derived.subType = subType
	derived.cachedString = derived.formatStringValue()
	return derived
}
