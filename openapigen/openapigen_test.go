package openapigen

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/harshhh28/soapbridge/wsdl"
)

func TestGenerate_Calculator(t *testing.T) {
	data, err := os.ReadFile("../testdata/wsdl/calculator.wsdl")
	if err != nil {
		t.Fatal(err)
	}
	res, err := wsdl.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	doc := Generate(res.Definition, "Calculator", "0.1.0")
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("OpenAPI = %q", doc.OpenAPI)
	}
	path, ok := doc.Paths["/api/Add"]
	if !ok || path.Post == nil {
		t.Fatalf("missing POST /api/Add")
	}
	if path.Post.OperationID != "Add" {
		t.Errorf("OperationID = %q", path.Post.OperationID)
	}
	// Every operation's request/response schema is a $ref into the shared
	// components/schemas registry (not inlined) — see openapigen.Generate's
	// doc for why: a type reused across many operations would otherwise be
	// duplicated once per operation instead of defined a single time.
	reqSchema := path.Post.RequestBody.Content["application/json"].Schema
	if reqSchema.Ref == "" || !strings.HasPrefix(reqSchema.Ref, "#/components/schemas/") {
		t.Fatalf("request schema should be a #/components/schemas/ $ref, got %+v", reqSchema)
	}
	if doc.Components == nil {
		t.Fatalf("expected doc.Components to hold the shared schema definitions")
	}
	addDef := doc.Components.Schemas[strings.TrimPrefix(reqSchema.Ref, "#/components/schemas/")]
	if addDef == nil || addDef.Type != "object" || len(addDef.Required) != 2 {
		t.Errorf("Add's request definition = %+v", addDef)
	}
	if _, ok := path.Post.Responses["200"]; !ok {
		t.Errorf("missing 200 response")
	}
	if len(doc.Components.SecuritySchemes) != 0 {
		t.Errorf("expected no security scheme for a WSDL without WS-Security, got %v", doc.Components.SecuritySchemes)
	}

	// The document itself must be valid JSON end to end (this is what
	// gets written to disk / served at /openapi.json).
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("Document does not marshal to JSON: %v", err)
	}
}

func TestGenerate_WSSecurity(t *testing.T) {
	data, err := os.ReadFile("../testdata/wsdl/enumsample.wsdl")
	if err != nil {
		t.Fatal(err)
	}
	res, err := wsdl.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	res.Definition.WSSecurity = true // enumsample fixture has none; force the branch under test

	doc := Generate(res.Definition, "EnumSample", "0.1.0")
	if doc.Components == nil || doc.Components.SecuritySchemes["basicAuth"].Type != "http" {
		t.Errorf("expected a basicAuth security scheme, got %+v", doc.Components)
	}
}
