package mockserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/harshhh28/soapbridge/gateway"
	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/wsdl"
)

func mustParse(t *testing.T, path string) *model.Definition {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := wsdl.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return res.Definition
}

// TestHandler_RoundTrip drives a real gateway.Client against the mock
// server (not a hand-built HTTP request) so it exercises the exact path a
// generated gateway would: BuildEnvelope -> HTTP POST -> mock dispatch by
// body element -> BuildResponseEnvelope -> ParseResponse.
func TestHandler_RoundTrip(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")

	srv := httptest.NewServer(Handler(def))
	defer srv.Close()

	var addOp model.Operation
	for _, op := range def.Operations {
		if op.Name == "Add" {
			addOp = op
		}
	}

	client := gateway.NewClient(srv.URL)
	value, fault, err := client.Call(context.Background(), addOp, map[string]interface{}{"intA": float64(4), "intB": float64(5)}, gateway.Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %+v", fault)
	}
	obj, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("value = %#v, want an object", value)
	}
	if _, ok := obj["AddResult"]; !ok {
		t.Errorf("expected an AddResult field in the mock response, got %v", obj)
	}
}

// TestHandler_UnknownOperation checks a request for an element the WSDL
// doesn't define comes back as a SOAP fault (HTTP 500 carrying
// <soap:Fault>), not a panic or a 200.
func TestHandler_UnknownOperation(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	srv := httptest.NewServer(Handler(def))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "text/xml", strings.NewReader(
		`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><NotAnOperation xmlns="http://tempuri.org/"/></soap:Body></soap:Envelope>`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "soap:Fault") {
		t.Errorf("body = %s, want a soap:Fault", body)
	}
}
