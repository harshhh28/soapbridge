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
	"strings"
	"unicode"

	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/schema"
)

const protocolVersion = "2024-11-05"

// Tool is one MCP tool definition derived from a SOAP operation.
type Tool struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema *schema.JSONSchema `json:"inputSchema"`
	Annotations *Annotations       `json:"annotations,omitempty"`
	op          model.Operation
}

// Annotations are MCP tool annotations (readOnlyHint etc.). WSDL has no
// standard "mutates state" field, so these are inferred from the operation
// name's leading verb. Pointers let a hint be omitted when unknown, in which
// case MCP hosts apply the spec's conservative defaults (not read-only,
// potentially destructive).
type Annotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

var readVerbs = map[string]bool{
	"get": true, "list": true, "query": true, "search": true, "find": true,
	"read": true, "fetch": true, "lookup": true, "check": true, "describe": true,
	"count": true, "validate": true, "is": true, "has": true, "show": true,
}

var destructiveVerbs = map[string]bool{
	"delete": true, "remove": true, "cancel": true, "destroy": true,
	"purge": true, "drop": true, "terminate": true, "revoke": true, "clear": true,
}

// firstWord returns the leading lower-cased word of a camelCase, PascalCase,
// snake_case or kebab-case identifier.
func firstWord(name string) string {
	rs := []rune(name)
	for i, r := range rs {
		if i > 0 && (unicode.IsUpper(r) || r == '_' || r == '-' || r == '.') {
			return strings.ToLower(string(rs[:i]))
		}
	}
	return strings.ToLower(name)
}

// InferAnnotations classifies an operation by name. Names matching a known
// read verb are marked read-only and idempotent; known destructive verbs are
// marked destructive; anything else gets no read/destructive hint so hosts
// fall back to cautious defaults. openWorldHint is always true because every
// tool calls an external SOAP service.
func InferAnnotations(opName string) *Annotations {
	t, f := true, false
	a := &Annotations{OpenWorldHint: &t}
	switch w := firstWord(opName); {
	case readVerbs[w]:
		a.ReadOnlyHint, a.IdempotentHint = &t, &t
	case destructiveVerbs[w]:
		a.ReadOnlyHint, a.DestructiveHint = &f, &t
	}
	return a
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
			Annotations: InferAnnotations(op.Name),
			op:          op,
		})
	}
	return tools
}
