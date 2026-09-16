package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/schema"
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

func findOp(t *testing.T, def *model.Definition, name string) model.Operation {
	t.Helper()
	for _, o := range def.Operations {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("operation %q not found", name)
	return model.Operation{}
}

// TestBuildEnvelope_Calculator checks the exact envelope shape against the
// real dneonline Calculator WSDL, including that unprefixed child elements
// inherit the default xmlns set on the operation root (matching how real
// SOAP servers emit responses, verified live in TestLive_Calculator below).
func TestBuildEnvelope_Calculator(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")

	env, err := BuildEnvelope(op, map[string]interface{}{"intA": float64(4), "intB": float64(5)}, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(env)
	for _, want := range []string{
		`<Add xmlns="http://tempuri.org/">`,
		`<intA>4</intA>`,
		`<intB>5</intB>`,
		`</Add>`,
		`<soap:Body>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("envelope missing %q; got:\n%s", want, s)
		}
	}
}

// TestBuildEnvelope_MissingRequired ensures a missing required field is
// rejected before any XML is sent, rather than silently omitted.
func TestBuildEnvelope_MissingRequired(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")
	_, err := BuildEnvelope(op, map[string]interface{}{"intA": float64(4)}, Credentials{})
	if err == nil {
		t.Fatal("expected an error for missing required field intB")
	}
}

// TestBuildEnvelope_WSSecurity checks the UsernameToken header is emitted
// when credentials are supplied.
func TestBuildEnvelope_WSSecurity(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")
	env, err := BuildEnvelope(op, map[string]interface{}{"intA": float64(1), "intB": float64(2)}, Credentials{Username: "alice", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(env)
	for _, want := range []string{"<wsse:UsernameToken>", "<wsse:Username>alice</wsse:Username>", "<wsse:Password", "s3cret"} {
		if !strings.Contains(s, want) {
			t.Errorf("envelope missing %q", want)
		}
	}
}

// TestPeekBodyElement checks the body-element dispatch key package
// mockserver relies on, including against a real envelope this package
// itself builds (BuildEnvelope's output must always be something
// PeekBodyElement can read back).
func TestPeekBodyElement(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")
	env, err := BuildEnvelope(op, map[string]interface{}{"intA": float64(1), "intB": float64(2)}, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	elem, err := PeekBodyElement(env)
	if err != nil {
		t.Fatal(err)
	}
	if elem.Local != "Add" || elem.Space != "http://tempuri.org/" {
		t.Errorf("got %+v", elem)
	}

	if _, err := PeekBodyElement([]byte(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body></soap:Body></soap:Envelope>`)); err == nil {
		t.Error("expected an error for an empty soap:Body")
	}
}

// TestParseResponse_Fault feeds a canned SOAP 1.1 fault body (shape
// verified against the SOAP 1.1 spec) through ParseResponse and checks
// classification.
func TestParseResponse_Fault(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")
	body := []byte(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <soap:Fault>
      <faultcode>soap:Client</faultcode>
      <faultstring>intA is required</faultstring>
      <detail>bad request detail</detail>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`)
	_, fault, err := ParseResponse(body, op)
	if err != nil {
		t.Fatal(err)
	}
	if fault == nil {
		t.Fatal("expected a fault")
	}
	if fault.HTTPStatus() != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want 400", fault.HTTPStatus())
	}
	if fault.Message != "intA is required" {
		t.Errorf("Message = %q", fault.Message)
	}
}

// TestGateway_MockServer round-trips through a real net/http server (not
// just ParseResponse) so the Client's HTTP plumbing (SOAPAction header,
// content type, status handling) is also exercised, including a server
// fault mapping to 502.
func TestGateway_MockServer(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("SOAPAction"); got != `"http://tempuri.org/Add"` {
			t.Errorf("SOAPAction header = %q", got)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "github.com/harshhh28/soapbridge/") {
			t.Errorf("User-Agent = %q, want a soapbridge/ identifier (some endpoints block Go's default UA)", ua)
		}
		if r.URL.Path == "/fault" {
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:Server</faultcode><faultstring>boom</faultstring></soap:Fault></soap:Body></soap:Envelope>`))
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><AddResponse xmlns="http://tempuri.org/"><AddResult>7</AddResult></AddResponse></soap:Body></soap:Envelope>`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	value, fault, err := client.Call(context.Background(), op, map[string]interface{}{"intA": float64(3), "intB": float64(4)}, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %+v", fault)
	}
	obj, ok := value.(map[string]interface{})
	if !ok || obj["AddResult"] != int64(7) {
		t.Errorf("value = %#v, want AddResult=7", value)
	}

	client2 := NewClient(srv.URL + "/fault")
	_, fault2, err := client2.Call(context.Background(), op, map[string]interface{}{"intA": float64(3), "intB": float64(4)}, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if fault2 == nil || fault2.HTTPStatus() != http.StatusBadGateway {
		t.Errorf("expected a server fault mapping to 502, got %+v", fault2)
	}
}

// TestHandler_HTTP drives gateway.Handler end to end through an
// httptest.Server standing in for the real SOAP endpoint, verifying the
// REST JSON in/out contract including validation-failure and
// method-not-allowed behavior.
func TestHandler_HTTP(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")

	soap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><AddResponse xmlns="http://tempuri.org/"><AddResult>9</AddResult></AddResponse></soap:Body></soap:Envelope>`))
	}))
	defer soap.Close()

	client := NewClient(soap.URL)
	inSchema := schema.FromType(op.Input.Type)
	rest := httptest.NewServer(Handler(op, client, inSchema))
	defer rest.Close()

	resp, err := http.Post(rest.URL, "application/json", strings.NewReader(`{"intA": 4, "intB": 5}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["AddResult"] != float64(9) {
		t.Errorf("body = %v", out)
	}

	// Missing required field -> 400 from schema validation, no SOAP call.
	resp2, err := http.Post(rest.URL, "application/json", strings.NewReader(`{"intA": 4}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp2.StatusCode)
	}

	resp3, err := http.Get(rest.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp3.StatusCode)
	}
}

// TestLive_Calculator and TestLive_NumberConversion hit real, public demo
// SOAP services over the network. They're skipped automatically if the
// service isn't reachable (e.g. offline CI) rather than failing the suite.
func TestLive_Calculator(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	op := findOp(t, def, "Add")
	b := def.PrimaryBinding()
	client := &Client{HTTPClient: &http.Client{Timeout: 10 * time.Second}, Endpoint: b.EndpointURL}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, fault, err := client.Call(ctx, op, map[string]interface{}{"intA": float64(4), "intB": float64(5)}, Credentials{})
	if err != nil {
		t.Skipf("live service unreachable, skipping: %v", err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %+v", fault)
	}
	obj := value.(map[string]interface{})
	if obj["AddResult"] != int64(9) {
		t.Errorf("4+5 via live SOAP service = %v, want 9", obj["AddResult"])
	}
}

func TestLive_NumberConversion(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/numberconversion.wsdl")
	op := findOp(t, def, "NumberToWords")
	b := def.PrimaryBinding()
	client := &Client{HTTPClient: &http.Client{Timeout: 10 * time.Second}, Endpoint: b.EndpointURL}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, fault, err := client.Call(ctx, op, map[string]interface{}{"ubiNum": float64(42)}, Credentials{})
	if err != nil {
		t.Skipf("live service unreachable, skipping: %v", err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %+v", fault)
	}
	obj := value.(map[string]interface{})
	words, _ := obj["NumberToWordsResult"].(string)
	if !strings.Contains(strings.ToLower(words), "forty two") {
		t.Errorf("NumberToWords(42) = %q, want it to contain \"forty two\"", words)
	}
}
