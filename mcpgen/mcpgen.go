// Package mcpgen exposes a WSDL's operations as MCP tools, backed by the
// exact same internal model and gateway.Client REST handlers use — SOAP
// calling logic lives once, in internal/gateway.
//
// Scope for v1: the JSON-RPC methods "initialize", "tools/list",
// "tools/call", and "ping"; no resources, prompts, sampling, roots, or
// subscriptions. Two transports: stdio (newline-delimited JSON-RPC, for
// local agent use) and a synchronous single-response-per-POST HTTP
// transport (for remote agents) — not the full Streamable HTTP transport's
// optional SSE server-push, which is out of scope for v1.
package mcpgen

import (
	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/schema"
)

const protocolVersion = "2024-11-05"

// Tool is one MCP tool definition derived from a SOAP operation.
type Tool struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	InputSchema *schema.JSONSchema   `json:"inputSchema"`
	op          model.Operation
}

// GenerateTools builds one MCP Tool per operation in def.
func GenerateTools(def *model.Definition) []Tool {
	tools := make([]Tool, 0, len(def.Operations))
	for _, op := range def.Operations {
		desc := op.Documentation
		if desc == "" {
			desc = "Calls the " + op.Name + " SOAP operation."
		}
		var inType *model.Type
		if op.Input != nil {
			inType = op.Input.Type
		}
		tools = append(tools, Tool{
			Name:        op.Name,
			Description: desc,
			InputSchema: schema.FromType(inType),
			op:          op,
		})
	}
	return tools
}
