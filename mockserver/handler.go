package mockserver

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/harshhh28/soapbridge/gateway"
	"github.com/harshhh28/soapbridge/model"
)

// Handler answers every operation in def with a synthetic, schema-shaped
// SOAP response (see ExampleValue) — a stand-in SOAP service for
// exercising a soapbridge-generated gateway end to end without a real
// backend. It dispatches by the request body's root element name (see
// gateway.PeekBodyElement).
func Handler(def *model.Definition) http.HandlerFunc {
	byElement := make(map[model.QName]model.Operation, len(def.Operations))
	for _, op := range def.Operations {
		if op.Input != nil {
			byElement[op.Input.Element] = op
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeFault(w, "Client", "reading request body: "+err.Error())
			return
		}
		elem, err := gateway.PeekBodyElement(data)
		if err != nil {
			writeFault(w, "Client", "could not determine the requested operation: "+err.Error())
			return
		}
		op, ok := byElement[elem]
		if !ok {
			writeFault(w, "Client", fmt.Sprintf("no operation bound to request element %q", elem.Local))
			return
		}
		if op.Output == nil || op.Output.Type == nil {
			writeFault(w, "Server", fmt.Sprintf("operation %q has no output message to mock", op.Name))
			return
		}

		body, _ := ExampleValue(op.Output.Type).(map[string]interface{})
		if body == nil {
			body = map[string]interface{}{}
		}
		respXML, err := gateway.BuildResponseEnvelope(op, body)
		if err != nil {
			writeFault(w, "Server", "building mock response: "+err.Error())
			return
		}

		log.Printf("[mock] %s -> synthetic response", op.Name)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write(respXML)
	}
}

func writeFault(w http.ResponseWriter, code, msg string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError) // SOAP 1.1 faults conventionally ride HTTP 500
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(msg))
	_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:%s</faultcode><faultstring>%s</faultstring></soap:Fault></soap:Body></soap:Envelope>`, code, escaped.String())
}
