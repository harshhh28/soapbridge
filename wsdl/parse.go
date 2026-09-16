// Package wsdl parses a WSDL 1.1 document (with inline XSD schema) into the
// namespace-resolved internal representation defined by internal/model.
//
// Scope for v1 (see README for the full list): SOAP 1.1 bindings,
// document/literal and rpc/literal style, single-file WSDL (no
// xsd:import/xsd:include across files), no xsd:choice or
// xsd:substitutionGroup.
package wsdl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/harshhh28/soapbridge/model"
)

// Result is the outcome of parsing one WSDL document: the resolved model
// plus any non-fatal warnings about constructs that were skipped or
// approximated (unresolved type references, xsd:choice, xsd:import, ...).
type Result struct {
	Definition *model.Definition
	Warnings   []string
}

// Fetch reads the raw bytes of a WSDL document from either an http(s)://
// URL or a local file path, without parsing them. Exposed so callers that
// need the original bytes (e.g. the CLI embedding them in a generated
// module) don't have to re-derive them from the parsed model.
func Fetch(source string) ([]byte, error) {
	var data []byte
	var err error
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		data, err = fetchURL(source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, fmt.Errorf("loading WSDL from %q: %w", source, err)
	}
	return data, nil
}

// Load fetches a WSDL document from either an http(s):// URL or a local
// file path and parses it.
func Load(source string) (*Result, error) {
	data, err := Fetch(source)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func fetchURL(url string) ([]byte, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// Parse parses raw WSDL XML bytes into the resolved internal model.
func Parse(data []byte) (*Result, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false // tolerate real-world documents with minor entity/encoding quirks

	var raw rawDefinitions
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing WSDL XML: %w", err)
	}
	if raw.XMLName.Local != "definitions" {
		return nil, fmt.Errorf("root element is %q, not a WSDL <definitions> element", raw.XMLName.Local)
	}

	def, warnings, err := resolve(&raw)
	if err != nil {
		return nil, err
	}
	def.WSSecurity = detectWSSecurity(data)
	return &Result{Definition: def, Warnings: warnings}, nil
}

// wsSecuritySignatures are byte substrings whose presence anywhere in the
// raw WSDL strongly indicates a WS-Security UsernameToken policy is
// expected. Real-world WS-Security is attached via WS-Policy extensibility
// elements on bindings (shapes vary across .NET/Java/etc toolchains), so a
// raw scan is far more robust than trying to model WS-Policy itself, which
// is out of scope for v1.
var wsSecuritySignatures = [][]byte{
	[]byte("oasis-200401-wss-wssecurity-secext-1.0"), // the WS-Security secext namespace URI
	[]byte("UsernameToken"),
}

func detectWSSecurity(data []byte) bool {
	for _, sig := range wsSecuritySignatures {
		if bytes.Contains(data, sig) {
			return true
		}
	}
	return false
}
