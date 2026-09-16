// Package gateway implements the SOAP <-> JSON round trip: build a SOAP
// envelope from a JSON request body using the operation's *model.Type,
// POST it to the real SOAP endpoint, and convert the response (or fault)
// back into JSON. internal/mcpgen's tool handlers call the same Call
// function REST handlers use, so the SOAP-calling code path is not
// duplicated between the two surfaces.
package gateway

import (
	"bytes"
	"encoding/xml"
	"fmt"

	"github.com/harshhh28/soapbridge/model"
)

const (
	nsSOAPEnvelope = "http://schemas.xmlsoap.org/soap/envelope/"
	nsXSI          = "http://www.w3.org/2001/XMLSchema-instance"
	nsWSSE         = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	nsWSU          = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	passwordTextNS = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordText"
)

// Credentials carries WS-Security UsernameToken values sourced from the
// REST-side request (an API key or HTTP Basic auth header) and injected
// into the outgoing SOAP envelope's header. Zero value means "no
// credentials supplied".
type Credentials struct {
	Username string
	Password string
}

func (c Credentials) empty() bool { return c.Username == "" && c.Password == "" }

// BuildEnvelope renders a document/literal SOAP request envelope for op,
// populating its body from a JSON-decoded value (the
// map[string]interface{} tree produced by encoding/json.Unmarshal).
// Callers should validate body against schema.FromType(op.Input.Type)
// first; BuildEnvelope itself only enforces the minimum needed to avoid
// emitting malformed XML (required fields present, arrays are arrays).
func BuildEnvelope(op model.Operation, body map[string]interface{}, creds Credentials) ([]byte, error) {
	if op.Input == nil || op.Input.Type == nil {
		return nil, fmt.Errorf("operation %q has no input message", op.Name)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<soap:Envelope xmlns:soap="` + nsSOAPEnvelope + `" xmlns:xsi="` + nsXSI + `">`)

	buf.WriteString("<soap:Header>")
	if !creds.empty() {
		writeUsernameToken(&buf, creds)
	}
	buf.WriteString("</soap:Header>")

	if err := writeBody(&buf, op.Input.Element, op.Input.Type, body); err != nil {
		return nil, fmt.Errorf("operation %q: %w", op.Name, err)
	}
	buf.WriteString("</soap:Envelope>")

	return buf.Bytes(), nil
}

// BuildResponseEnvelope renders a document/literal SOAP *response* envelope
// for op — the mirror of BuildEnvelope, wrapping op.Output instead of
// op.Input and carrying no WS-Security header (a response doesn't
// authenticate the caller). This is what package mockserver uses to answer
// as a stand-in SOAP service would, from the exact same body-writing logic
// BuildEnvelope uses for requests, so a mock response and a real request
// can never disagree about how a type is supposed to look on the wire.
func BuildResponseEnvelope(op model.Operation, body map[string]interface{}) ([]byte, error) {
	if op.Output == nil || op.Output.Type == nil {
		return nil, fmt.Errorf("operation %q has no output message", op.Name)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<soap:Envelope xmlns:soap="` + nsSOAPEnvelope + `" xmlns:xsi="` + nsXSI + `">`)
	if err := writeBody(&buf, op.Output.Element, op.Output.Type, body); err != nil {
		return nil, fmt.Errorf("operation %q: %w", op.Name, err)
	}
	buf.WriteString("</soap:Envelope>")

	return buf.Bytes(), nil
}

func writeBody(buf *bytes.Buffer, element model.QName, t *model.Type, body map[string]interface{}) error {
	buf.WriteString("<soap:Body>")
	root := element.Local
	ns := element.Space
	buf.WriteString("<" + root + ` xmlns="` + ns + `">`)
	if err := writeObjectChildren(buf, t, body); err != nil {
		return err
	}
	buf.WriteString("</" + root + ">")
	buf.WriteString("</soap:Body>")
	return nil
}

func writeUsernameToken(buf *bytes.Buffer, creds Credentials) {
	buf.WriteString(`<wsse:Security xmlns:wsse="` + nsWSSE + `" xmlns:wsu="` + nsWSU + `" soap:mustUnderstand="1">`)
	buf.WriteString(`<wsse:UsernameToken>`)
	buf.WriteString(`<wsse:Username>`)
	xmlEscapeString(buf, creds.Username)
	buf.WriteString(`</wsse:Username>`)
	buf.WriteString(`<wsse:Password Type="` + passwordTextNS + `">`)
	xmlEscapeString(buf, creds.Password)
	buf.WriteString(`</wsse:Password>`)
	buf.WriteString(`</wsse:UsernameToken></wsse:Security>`)
}

// writeObjectChildren writes the child elements of an object-typed value
// (JSON map -> XML element sequence) without an outer wrapping element;
// the caller has already written the parent start/end tags.
func writeObjectChildren(buf *bytes.Buffer, t *model.Type, value interface{}) error {
	obj, ok := value.(map[string]interface{})
	if !ok {
		if value == nil {
			obj = nil
		} else {
			return fmt.Errorf("expected an object, got %T", value)
		}
	}
	for _, f := range t.Properties {
		v, present := obj[f.Name]
		if !present || v == nil {
			if f.Required && !f.Nillable {
				return fmt.Errorf("missing required field %q", f.Name)
			}
			continue
		}
		if err := writeField(buf, f.Name, f.Type, v); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	return nil
}

// writeField writes one JSON field as one or more sibling XML elements
// sharing the field's name (repeated for array fields, per XSD
// maxOccurs="unbounded" semantics — an array is NOT a single wrapper
// element, unlike a JSON array).
func writeField(buf *bytes.Buffer, name string, t *model.Type, value interface{}) error {
	if t.Kind == model.KindArray {
		items, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("expected an array, got %T", value)
		}
		for _, item := range items {
			if err := writeElement(buf, name, t.Items, item); err != nil {
				return err
			}
		}
		return nil
	}
	return writeElement(buf, name, t, value)
}

func writeElement(buf *bytes.Buffer, name string, t *model.Type, value interface{}) error {
	if value == nil {
		buf.WriteString("<" + name + ` xsi:nil="true"></` + name + ">")
		return nil
	}
	switch t.Kind {
	case model.KindObject:
		buf.WriteString("<" + name + ">")
		if err := writeObjectChildren(buf, t, value); err != nil {
			return err
		}
		buf.WriteString("</" + name + ">")
	case model.KindArray:
		return fmt.Errorf("nested array of array is not supported")
	default:
		s, err := scalarToXMLString(t, value)
		if err != nil {
			return err
		}
		buf.WriteString("<" + name + ">")
		xmlEscapeString(buf, s)
		buf.WriteString("</" + name + ">")
	}
	return nil
}

func scalarToXMLString(t *model.Type, value interface{}) (string, error) {
	switch t.Kind {
	case model.KindBoolean:
		b, ok := value.(bool)
		if !ok {
			return "", fmt.Errorf("expected boolean, got %T", value)
		}
		if b {
			return "true", nil
		}
		return "false", nil
	case model.KindInteger:
		n, ok := asFloat(value)
		if !ok {
			return "", fmt.Errorf("expected integer, got %T", value)
		}
		return fmt.Sprintf("%d", int64(n)), nil
	case model.KindNumber:
		n, ok := asFloat(value)
		if !ok {
			return "", fmt.Errorf("expected number, got %T", value)
		}
		return fmt.Sprintf("%g", n), nil
	default: // string, dateTime, date, any
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("%v", value), nil
		}
		return s, nil
	}
}

func asFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func xmlEscapeString(buf *bytes.Buffer, s string) {
	_ = xml.EscapeText(buf, []byte(s))
}
