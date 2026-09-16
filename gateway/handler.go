package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/schema"
)

// errorBody is the JSON shape returned for any non-2xx response, including
// SOAP faults translated into REST errors (see Fault.HTTPStatus).
type errorBody struct {
	Error         string   `json:"error"`
	Details       []string `json:"details,omitempty"`
	SOAPFaultCode string   `json:"soap_fault_code,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, details ...string) {
	writeJSON(w, status, errorBody{Error: msg, Details: details})
}

func writeFault(w http.ResponseWriter, f *Fault) {
	body := errorBody{Error: f.Message, SOAPFaultCode: f.Code}
	if f.Detail != "" {
		body.Details = []string{f.Detail}
	}
	writeJSON(w, f.HTTPStatus(), body)
}

// CredentialsFromRequest extracts WS-Security credentials for the outgoing
// SOAP call from the incoming REST request: HTTP Basic auth first, else an
// "X-API-Key: username:password" header (or a bare key used as both).
func CredentialsFromRequest(r *http.Request) Credentials {
	if u, p, ok := r.BasicAuth(); ok {
		return Credentials{Username: u, Password: p}
	}
	if key := r.Header.Get("X-API-Key"); key != "" {
		if i := strings.IndexByte(key, ':'); i >= 0 {
			return Credentials{Username: key[:i], Password: key[i+1:]}
		}
		return Credentials{Username: key, Password: key}
	}
	return Credentials{}
}

// Handler returns the REST handler for one SOAP operation: decode JSON,
// validate against its input JSON Schema, call the SOAP endpoint, and
// encode the JSON result (or a translated error/fault).
func Handler(op model.Operation, client *Client, inSchema *schema.JSONSchema) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed; use POST")
			return
		}

		var body interface{} = map[string]interface{}{}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
				writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
				return
			}
		}
		if body == nil {
			body = map[string]interface{}{}
		}
		if errs := schema.Validate(inSchema, body); len(errs) > 0 {
			writeError(w, http.StatusBadRequest, "request failed schema validation", errs...)
			return
		}
		bodyMap, _ := body.(map[string]interface{})

		value, fault, err := client.Call(r.Context(), op, bodyMap, CredentialsFromRequest(r))
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if fault != nil {
			writeFault(w, fault)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

// BuildMux wires a POST /api/{OperationName} REST handler for every
// operation in def onto a fresh *http.ServeMux, all sharing one Client for
// def's primary binding endpoint. This is the single place REST routing is
// assembled, so the generated server (cmd's "generate" output) and gateway
// tests exercise identical code.
func BuildMux(def *model.Definition) (*http.ServeMux, error) {
	b := def.PrimaryBinding()
	if b == nil {
		return nil, fmt.Errorf("WSDL %q has no usable SOAP binding", def.Name)
	}
	if b.EndpointURL == "" {
		return nil, fmt.Errorf("binding %q has no service address", b.Name)
	}
	client := NewClient(b.EndpointURL)

	mux := http.NewServeMux()
	for _, op := range def.Operations {
		op := op
		var inType *model.Type
		if op.Input != nil {
			inType = op.Input.Type
		}
		inSchema := schema.FromType(inType)
		mux.HandleFunc("/api/"+op.Name, Handler(op, client, inSchema))
	}
	return mux, nil
}
