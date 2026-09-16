// Package openapigen emits an OpenAPI 3.0 document describing the REST API
// internal/gateway serves, derived from the same internal model and
// schema.FromType used by the gateway and MCP generators.
package openapigen

import (
	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/schema"
)

type Document struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Paths      map[string]PathItem `json:"paths"`
	Components *Components         `json:"components,omitempty"`
	Security   []map[string][]string `json:"security,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type PathItem struct {
	Post *Operation `json:"post,omitempty"`
}

type Operation struct {
	OperationID string               `json:"operationId"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty"`
	Responses   map[string]Response  `json:"responses"`
}

type RequestBody struct {
	Required bool                        `json:"required"`
	Content  map[string]MediaType        `json:"content"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema *schema.JSONSchema `json:"schema"`
}

type Components struct {
	Schemas         map[string]*schema.JSONSchema `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme     `json:"securitySchemes,omitempty"`
}

type SecurityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme"`
}

var errorSchema = &schema.JSONSchema{
	Type: "object",
	Properties: map[string]*schema.JSONSchema{
		"error":           {Type: "string"},
		"details":         {Type: "array", Items: &schema.JSONSchema{Type: "string"}},
		"soap_fault_code": {Type: "string"},
	},
	Required: []string{"error"},
}

// Generate builds the OpenAPI document for def: one POST /api/{operation}
// path per SOAP operation, request/response schemas derived from the same
// model the gateway and MCP generators use, and a basicAuth security
// scheme when the WSDL indicated WS-Security.
//
// Every operation's schemas are built through one shared schema.Builder
// (see its docs for why), so a named type reused across many operations —
// routine in a large enterprise WSDL — gets exactly one definition under
// components/schemas, referenced by "$ref" everywhere else, instead of a
// full copy inlined into every operation that touches it.
func Generate(def *model.Definition, serverTitle, version string) *Document {
	doc := &Document{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       serverTitle,
			Description: def.Documentation,
			Version:     version,
		},
		Paths: map[string]PathItem{},
	}

	b := schema.NewComponentsBuilder()
	for _, op := range def.Operations {
		var inType, outType *model.Type
		if op.Input != nil {
			inType = op.Input.Type
		}
		if op.Output != nil {
			outType = op.Output.Type
		}

		desc := op.Documentation
		o := &Operation{
			OperationID: op.Name,
			Summary:     op.Name,
			Description: desc,
			RequestBody: &RequestBody{
				Required: true,
				Content: map[string]MediaType{
					"application/json": {Schema: b.Schema(inType)},
				},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Successful response",
					Content: map[string]MediaType{
						"application/json": {Schema: b.Schema(outType)},
					},
				},
				"400": {
					Description: "Invalid request, or a client-side SOAP fault",
					Content:     map[string]MediaType{"application/json": {Schema: errorSchema}},
				},
				"502": {
					Description: "The upstream SOAP service errored or was unreachable",
					Content:     map[string]MediaType{"application/json": {Schema: errorSchema}},
				},
			},
		}
		doc.Paths["/api/"+op.Name] = PathItem{Post: o}
	}

	if defs := b.Defs(); len(defs) > 0 {
		doc.Components = &Components{Schemas: defs}
	}
	if def.WSSecurity {
		if doc.Components == nil {
			doc.Components = &Components{}
		}
		doc.Components.SecuritySchemes = map[string]SecurityScheme{
			"basicAuth": {Type: "http", Scheme: "basic"},
		}
		doc.Security = []map[string][]string{{"basicAuth": {}}}
	}

	return doc
}
