package wsdl

import "encoding/xml"

// Raw XML structures mirroring the WSDL 1.1 and XML Schema grammars.
//
// Every element/attribute that must be matched regardless of the prefix a
// document happens to use is tagged with its full namespace URI (Go's
// encoding/xml resolves prefixes to URIs before matching struct tags, so
// "wsdl:definitions", "<no prefix>:definitions" and "foo:definitions" all
// match the same `xml:"http://schemas.xmlsoap.org/wsdl/ definitions"` tag).
// Attribute *values* that are themselves QNames (type="tns:Foo") are NOT
// resolved by encoding/xml — those are resolved separately in resolve.go
// using an explicit namespace scope chain built from the xmlns attributes
// captured alongside each element.

const (
	NSWSDL   = "http://schemas.xmlsoap.org/wsdl/"
	NSXSD    = "http://www.w3.org/2001/XMLSchema"
	NSSOAP11 = "http://schemas.xmlsoap.org/wsdl/soap/"
	NSSOAP12 = "http://schemas.xmlsoap.org/wsdl/soap12/"
	NSWSSE   = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
)

// rawAttrs is embedded in every raw struct that may carry xmlns
// declarations relevant to resolving descendant QName attribute values.
type rawAttrs struct {
	Attrs []xml.Attr `xml:",any,attr"`
}

type rawDefinitions struct {
	XMLName         xml.Name      `xml:"http://schemas.xmlsoap.org/wsdl/ definitions"`
	Name            string        `xml:"name,attr"`
	TargetNamespace string        `xml:"targetNamespace,attr"`
	Documentation   string        `xml:"http://schemas.xmlsoap.org/wsdl/ documentation"`
	Types           []rawTypes    `xml:"http://schemas.xmlsoap.org/wsdl/ types"`
	Messages        []rawMessage  `xml:"http://schemas.xmlsoap.org/wsdl/ message"`
	PortTypes       []rawPortType `xml:"http://schemas.xmlsoap.org/wsdl/ portType"`
	Bindings        []rawBinding  `xml:"http://schemas.xmlsoap.org/wsdl/ binding"`
	Services        []rawService  `xml:"http://schemas.xmlsoap.org/wsdl/ service"`
	rawAttrs
}

type rawTypes struct {
	Schemas []rawSchema `xml:"http://www.w3.org/2001/XMLSchema schema"`
	rawAttrs
}

type rawSchema struct {
	TargetNamespace string           `xml:"targetNamespace,attr"`
	Elements        []rawElement     `xml:"http://www.w3.org/2001/XMLSchema element"`
	ComplexTypes    []rawComplexType `xml:"http://www.w3.org/2001/XMLSchema complexType"`
	SimpleTypes     []rawSimpleType  `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
	Imports         []rawImport      `xml:"http://www.w3.org/2001/XMLSchema import"`
	Includes        []rawImport      `xml:"http://www.w3.org/2001/XMLSchema include"`
	rawAttrs
}

type rawImport struct {
	Namespace      string `xml:"namespace,attr"`
	SchemaLocation string `xml:"schemaLocation,attr"`
}

type rawElement struct {
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	Ref         string          `xml:"ref,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	Nillable    string          `xml:"nillable,attr"`
	ComplexType *rawComplexType `xml:"http://www.w3.org/2001/XMLSchema complexType"`
	SimpleType  *rawSimpleType  `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
	Annotation  *rawAnnotation  `xml:"http://www.w3.org/2001/XMLSchema annotation"`
	rawAttrs
}

type rawComplexType struct {
	Name       string         `xml:"name,attr"`
	Sequence   *rawSequence   `xml:"http://www.w3.org/2001/XMLSchema sequence"`
	All        *rawSequence   `xml:"http://www.w3.org/2001/XMLSchema all"`
	Choice     *rawChoice     `xml:"http://www.w3.org/2001/XMLSchema choice"`
	Annotation *rawAnnotation `xml:"http://www.w3.org/2001/XMLSchema annotation"`
	rawAttrs
}

// rawChoice is captured only so Parse can flag <xsd:choice> as an
// unsupported construct instead of silently dropping its fields (choice is
// an explicit v2 non-goal — see README).
type rawChoice struct {
	Elements []rawElement `xml:"http://www.w3.org/2001/XMLSchema element"`
}

type rawSequence struct {
	Elements []rawElement `xml:"http://www.w3.org/2001/XMLSchema element"`
}

type rawSimpleType struct {
	Name        string          `xml:"name,attr"`
	Restriction *rawRestriction `xml:"http://www.w3.org/2001/XMLSchema restriction"`
}

type rawRestriction struct {
	Base         string           `xml:"base,attr"`
	Enumerations []rawEnumeration `xml:"http://www.w3.org/2001/XMLSchema enumeration"`
}

type rawEnumeration struct {
	Value string `xml:"value,attr"`
}

type rawAnnotation struct {
	Documentation string `xml:"http://www.w3.org/2001/XMLSchema documentation"`
}

type rawMessage struct {
	Name  string    `xml:"name,attr"`
	Parts []rawPart `xml:"http://schemas.xmlsoap.org/wsdl/ part"`
}

type rawPart struct {
	Name    string `xml:"name,attr"`
	Element string `xml:"element,attr"`
	Type    string `xml:"type,attr"`
}

type rawPortType struct {
	Name       string          `xml:"name,attr"`
	Operations []rawPTOperation `xml:"http://schemas.xmlsoap.org/wsdl/ operation"`
}

type rawPTOperation struct {
	Name          string          `xml:"name,attr"`
	Documentation string          `xml:"http://schemas.xmlsoap.org/wsdl/ documentation"`
	Input         *rawMessageRef  `xml:"http://schemas.xmlsoap.org/wsdl/ input"`
	Output        *rawMessageRef  `xml:"http://schemas.xmlsoap.org/wsdl/ output"`
}

type rawMessageRef struct {
	Message string `xml:"message,attr"`
}

type rawBinding struct {
	Name       string             `xml:"name,attr"`
	Type       string             `xml:"type,attr"`
	SOAPBind11 *rawSOAPBinding    `xml:"http://schemas.xmlsoap.org/wsdl/soap/ binding"`
	SOAPBind12 *rawSOAPBinding    `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ binding"`
	Operations []rawBindOperation `xml:"http://schemas.xmlsoap.org/wsdl/ operation"`
	rawAttrs
}

type rawSOAPBinding struct {
	Transport string `xml:"transport,attr"`
	Style     string `xml:"style,attr"`
}

type rawBindOperation struct {
	Name       string           `xml:"name,attr"`
	SOAPOp11   *rawSOAPOperation `xml:"http://schemas.xmlsoap.org/wsdl/soap/ operation"`
	SOAPOp12   *rawSOAPOperation `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ operation"`
}

type rawSOAPOperation struct {
	SOAPAction string `xml:"soapAction,attr"`
	Style      string `xml:"style,attr"`
}

type rawService struct {
	Name  string    `xml:"name,attr"`
	Ports []rawPort `xml:"http://schemas.xmlsoap.org/wsdl/ port"`
}

type rawPort struct {
	Name        string       `xml:"name,attr"`
	Binding     string       `xml:"binding,attr"`
	Address11   *rawAddress  `xml:"http://schemas.xmlsoap.org/wsdl/soap/ address"`
	Address12   *rawAddress  `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ address"`
}

type rawAddress struct {
	Location string `xml:"location,attr"`
}
