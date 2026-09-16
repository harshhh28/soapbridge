// Package model is the single internal representation produced by the WSDL
// parser (internal/wsdl) and consumed by every generator (schema, gateway,
// mcpgen, openapigen). No generator should read WSDL/XSD structures directly;
// they all read this package instead, so REST, MCP and OpenAPI output never
// drift apart.
package model

// QName is a namespace-qualified name: a namespace URI plus a local part.
// Namespace prefixes (tns:, xs:, s:, ...) are resolved to URIs at parse
// time, so QName equality does not depend on which prefix a document used.
type QName struct {
	Space string
	Local string
}

func (q QName) String() string {
	if q.Space == "" {
		return q.Local
	}
	return q.Space + "#" + q.Local
}

// Kind is the fundamental shape of a Type, independent of its XSD origin.
type Kind int

const (
	KindString Kind = iota
	KindInteger
	KindNumber
	KindBoolean
	KindDateTime
	KindDate
	KindObject
	KindArray
	KindAny // unresolved / unsupported XSD construct; treated as opaque JSON
)

func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindInteger:
		return "integer"
	case KindNumber:
		return "number"
	case KindBoolean:
		return "boolean"
	case KindDateTime:
		return "dateTime"
	case KindDate:
		return "date"
	case KindObject:
		return "object"
	case KindArray:
		return "array"
	default:
		return "any"
	}
}

// Type is the resolved, in-memory representation of an XSD type: either a
// named global type (Name.Local != "") or an anonymous inline type.
type Type struct {
	Name QName // zero value for anonymous types
	Kind Kind

	// KindObject
	Properties []Field

	// KindArray
	Items *Type

	// KindString with xsd:enumeration restrictions
	Enum []string

	// Doc is documentation pulled from <xsd:annotation><xsd:documentation>.
	Doc string
}

// Field is a member of an object Type, derived from an <xsd:element> inside
// a <xsd:sequence>.
type Field struct {
	Name     string
	Type     *Type
	Required bool // derived from minOccurs > 0
	Nillable bool
	Doc      string
}

// Part describes one WSDL message part resolved to a concrete Type: the
// operation's input or output body.
type Part struct {
	Name    string
	Element QName // global element this part binds to (document style)
	Type    *Type
}

// Operation is one callable SOAP operation, with everything the gateway,
// MCP and OpenAPI generators need to build a request and interpret a
// response.
type Operation struct {
	Name          string
	Documentation string
	SOAPAction    string
	Style         string // "document" or "rpc"
	Input         *Part
	Output        *Part
}

// Binding describes how to reach the SOAP service for one WSDL binding
// (SOAP 1.1 or 1.2 endpoint).
type Binding struct {
	Name         string
	SOAPVersion  string // "1.1" or "1.2"
	EndpointURL  string
	Transport    string
}

// Definition is the root of the parsed, resolved model for one WSDL
// document: everything downstream code needs, with no remaining XML/XSD
// concepts (prefixes, refs, raw QName strings) left unresolved.
type Definition struct {
	Name            string
	TargetNamespace string
	Documentation   string

	Operations []Operation
	Bindings   []Binding // preference order: SOAP 1.1 first if present

	// WSSecurity is true when the WSDL's binding/policy indicates a
	// WS-Security UsernameToken is expected (detected heuristically; see
	// wsdl.Parse for the exact signal).
	WSSecurity bool
}

// PrimaryBinding returns the SOAP 1.1 binding if one exists, else the first
// binding, else nil. SOAP 1.2 support is out of scope for v1 (see README),
// so generators should always call this rather than picking Bindings[0].
func (d *Definition) PrimaryBinding() *Binding {
	if d == nil || len(d.Bindings) == 0 {
		return nil
	}
	for i := range d.Bindings {
		if d.Bindings[i].SOAPVersion == "1.1" {
			return &d.Bindings[i]
		}
	}
	return &d.Bindings[0]
}
