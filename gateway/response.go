package gateway

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/harshhh28/soapbridge/model"
)

const (
	nsSOAP11Envelope = "http://schemas.xmlsoap.org/soap/envelope/"
	nsSOAP12Envelope = "http://www.w3.org/2003/05/soap-envelope"
)

// Fault is a parsed SOAP fault (SOAP 1.1 faultcode/faultstring/detail, or a
// best-effort text extraction of the differently-shaped SOAP 1.2
// Code/Reason/Detail).
type Fault struct {
	Code    string
	Message string
	Detail  string
}

func (f *Fault) Error() string { return fmt.Sprintf("SOAP fault [%s]: %s", f.Code, f.Message) }

// HTTPStatus maps a SOAP fault to a REST status code: a client-caused
// fault (bad input) becomes 400, anything else (the upstream SOAP server
// itself erroring, protocol mismatches, ...) becomes 502 Bad Gateway since
// from the REST caller's point of view soapbridge is a gateway to a
// misbehaving upstream.
func (f *Fault) HTTPStatus() int {
	c := strings.ToLower(f.Code)
	if strings.Contains(c, "client") || strings.Contains(c, "sender") {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// PeekBodyElement returns the namespace-qualified name of the SOAP body's
// root child element, without decoding anything else. For document/literal
// SOAP, that element *is* the operation being invoked (or answered) — the
// standard way a real SOAP server dispatches a request, and the only
// robust way here: several real WSDLs (this repo's numberconversion.wsdl
// fixture included) declare an empty SOAPAction for every operation, so
// SOAPAction alone can't disambiguate them. Used by package mockserver to
// decide which operation an incoming request is for.
func PeekBodyElement(data []byte) (model.QName, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return model.QName{}, fmt.Errorf("reading SOAP request: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "Body" || (se.Name.Space != nsSOAP11Envelope && se.Name.Space != nsSOAP12Envelope) {
			continue
		}
		for {
			tok2, err := dec.Token()
			if err != nil {
				return model.QName{}, fmt.Errorf("reading soap:Body: %w", err)
			}
			switch t2 := tok2.(type) {
			case xml.StartElement:
				return model.QName{Space: t2.Name.Space, Local: t2.Name.Local}, nil
			case xml.EndElement:
				return model.QName{}, fmt.Errorf("soap:Body is empty")
			}
		}
	}
}

// ParseResponse parses a raw SOAP response body for op, returning either
// the decoded JSON-ready value, a *Fault, or an error if the response
// isn't well-formed SOAP at all.
func ParseResponse(data []byte, op model.Operation) (value interface{}, fault *Fault, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, nil, fmt.Errorf("no soap:Envelope/soap:Body found in response")
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading SOAP response: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "Body" && (se.Name.Space == nsSOAP11Envelope || se.Name.Space == nsSOAP12Envelope) {
			return parseBody(dec, se, op)
		}
	}
}

func parseBody(dec *xml.Decoder, bodyStart xml.StartElement, op model.Operation) (interface{}, *Fault, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("reading soap:Body: %w", err)
		}
		switch se := tok.(type) {
		case xml.StartElement:
			if se.Name.Local == "Fault" {
				f, err := parseFault(dec, se)
				return nil, f, err
			}
			if op.Output == nil || op.Output.Type == nil {
				if err := dec.Skip(); err != nil {
					return nil, nil, err
				}
				return nil, nil, nil
			}
			v, err := decodeElement(dec, se, op.Output.Type)
			return v, nil, err
		case xml.EndElement:
			if se.Name == bodyStart.Name {
				want := "a response element"
				if op.Output != nil {
					want = fmt.Sprintf("%q", op.Output.Element.Local)
				}
				return nil, nil, fmt.Errorf("soap:Body was empty; expected %s", want)
			}
		}
	}
}

func parseFault(dec *xml.Decoder, start xml.StartElement) (*Fault, error) {
	f := &Fault{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("reading soap:Fault: %w", err)
		}
		switch se := tok.(type) {
		case xml.StartElement:
			text, err := readAllText(dec, se)
			if err != nil {
				return nil, err
			}
			switch se.Name.Local {
			case "faultcode", "Code":
				f.Code = text
			case "faultstring", "Reason":
				f.Message = text
			case "detail", "Detail":
				f.Detail = text
			case "faultactor":
				// not surfaced; actor info isn't actionable for a REST caller
			}
		case xml.EndElement:
			if se.Name == start.Name {
				return f, nil
			}
		}
	}
}

// readAllText consumes the subtree rooted at the already-read start token
// and concatenates every character-data run it contains, ignoring element
// structure. Used for fault text (SOAP 1.1's flat faultstring vs SOAP
// 1.2's nested Reason/Text both come out as plain text this way).
func readAllText(dec *xml.Decoder, start xml.StartElement) (string, error) {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch tt := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			sb.Write(tt)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// decodeElement decodes the subtree rooted at the already-read start token
// into a JSON-ready value (map[string]interface{}, []interface{}, string,
// int64, float64, bool, or nil) per t.
func decodeElement(dec *xml.Decoder, start xml.StartElement, t *model.Type) (interface{}, error) {
	if isNil(start) {
		if err := dec.Skip(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	switch t.Kind {
	case model.KindObject:
		return decodeObject(dec, start, t)
	case model.KindArray:
		// A bare array at decode entry (only possible for a malformed or
		// unusually-shaped top-level output) is treated as a single-item
		// array of its element type.
		v, err := decodeElement(dec, start, t.Items)
		if err != nil {
			return nil, err
		}
		return []interface{}{v}, nil
	default:
		text, err := readAllText(dec, start)
		if err != nil {
			return nil, err
		}
		return scalarFromXML(t, text)
	}
}

func decodeObject(dec *xml.Decoder, start xml.StartElement, t *model.Type) (interface{}, error) {
	obj := map[string]interface{}{}
	arrays := map[string][]interface{}{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch se := tok.(type) {
		case xml.StartElement:
			field := findField(t, se.Name.Local)
			if field == nil {
				if err := dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			if field.Type.Kind == model.KindArray {
				v, err := decodeElement(dec, se, field.Type.Items)
				if err != nil {
					return nil, err
				}
				if v != nil {
					arrays[field.Name] = append(arrays[field.Name], v)
				}
			} else {
				v, err := decodeElement(dec, se, field.Type)
				if err != nil {
					return nil, err
				}
				obj[field.Name] = v
			}
		case xml.EndElement:
			if se.Name == start.Name {
				for k, v := range arrays {
					obj[k] = v
				}
				return obj, nil
			}
		}
	}
}

func findField(t *model.Type, local string) *model.Field {
	for i := range t.Properties {
		if t.Properties[i].Name == local {
			return &t.Properties[i]
		}
	}
	return nil
}

func isNil(se xml.StartElement) bool {
	for _, a := range se.Attr {
		if a.Name.Space == nsXSI && a.Name.Local == "nil" {
			return a.Value == "true" || a.Value == "1"
		}
	}
	return false
}

func scalarFromXML(t *model.Type, text string) (interface{}, error) {
	switch t.Kind {
	case model.KindInteger:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing integer %q: %w", text, err)
		}
		return n, nil
	case model.KindNumber:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing number %q: %w", text, err)
		}
		return n, nil
	case model.KindBoolean:
		switch text {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("parsing boolean %q", text)
		}
	default: // string, dateTime, date, any
		return text, nil
	}
}
