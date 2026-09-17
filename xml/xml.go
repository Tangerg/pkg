package xml

import (
	"bytes"
	"encoding/xml"
	"strings"
)

// Name is an XML element name: the local name, without namespace prefix.
type Name struct {
	Local string
}

// String returns the string representation of the name.
func (n Name) String() string {
	return n.Local
}

// Attr represents an XML attribute with a name and value.
type Attr struct {
	Name  Name
	Value string
}

// String returns the attribute formatted as name="value", with the value
// XML-escaped.
func (a Attr) String() string {
	sb := new(strings.Builder)
	xml.Escape(sb, []byte(a.Value))
	return a.Name.String() + `="` + sb.String() + `"`
}

// StartElement represents an XML start tag with its name and attributes.
// Example: <element attr1="value1" attr2="value2">
type StartElement struct {
	Name  Name
	Attrs []Attr
}

// String returns the start tag in valid XML syntax.
func (e StartElement) String() string {
	sb := new(strings.Builder)
	sb.WriteString("<")
	sb.WriteString(e.Name.String())

	for _, attr := range e.Attrs {
		sb.WriteString(" ")
		sb.WriteString(attr.String())
	}

	sb.WriteString(">")
	return sb.String()
}

// Copy returns a deep copy: the Attrs slice is duplicated so the copy shares
// no backing array with e.
func (e StartElement) Copy() StartElement {
	attrs := make([]Attr, len(e.Attrs))
	copy(attrs, e.Attrs)
	e.Attrs = attrs
	return e
}

// End creates a corresponding EndElement for this StartElement.
func (e StartElement) End() EndElement {
	return EndElement{e.Name}
}

// EndElement represents an XML end tag.
// Example: </element>
type EndElement struct {
	Name Name
}

// String returns the string representation of the end element.
func (e EndElement) String() string {
	return "</" + e.Name.String() + ">"
}

// Copy creates a copy of the EndElement.
func (e EndElement) Copy() EndElement {
	return EndElement{e.Name}
}

// Content is any XML content: either an [Element] or [CharData].
type Content interface {
	String() string
	content()
	copy() Content
}

// Element represents a complete XML element with start tag, content, and end tag.
// Example: <element>content</element>
type Element struct {
	Start    StartElement
	Contents []Content
	End      EndElement
}

// Copy returns a deep copy, recursively copying nested contents.
func (e Element) Copy() Element {
	contents := make([]Content, len(e.Contents))
	for i, content := range e.Contents {
		contents[i] = content.copy()
	}
	return Element{
		e.Start.Copy(),
		contents,
		e.End.Copy(),
	}
}

func (e Element) content() {}

func (e Element) copy() Content {
	return e.Copy()
}

// String returns the complete element, from start tag through end tag.
func (e Element) String() string {
	sb := new(strings.Builder)
	sb.WriteString(e.Start.String())

	for _, content := range e.Contents {
		sb.WriteString(content.String())
	}

	sb.WriteString(e.End.String())
	return sb.String()
}

// CharData is XML character data. Its [CharData.String] form is XML-escaped.
type CharData []byte

// Copy creates a copy of the CharData.
func (c CharData) Copy() CharData {
	return CharData(bytes.Clone(c))
}

func (c CharData) content() {}

func (c CharData) copy() Content {
	return c.Copy()
}

// String returns the XML-escaped string representation of the character data.
func (c CharData) String() string {
	sb := new(strings.Builder)
	xml.Escape(sb, c)
	return sb.String()
}
