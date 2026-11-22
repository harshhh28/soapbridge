package mcpgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/harshhh28/soapbridge/schema"
)

// SchemaMode controls how much of each tool's input schema tools/list
// advertises.
//
// MCP requires every tool's inputSchema to be self-contained, so a named type
// reachable from many operations is repeated in every one of them. On a
// large enterprise WSDL that multiplies badly: one real 289-operation WSDL
// has 654 distinct types (0.7 MB) but a 21 MB tools/list, far beyond what any
// MCP host can load. Compact mode advertises only each tool's top-level
// fields, summarizes nested objects, and serves the full schema on demand
// through a describe tool. Validation and tools/call always use the full
// schema, so nothing is lost, only deferred.
type SchemaMode string

const (
	// SchemaAuto (the default) uses compact schemas only when the full
	// tools/list would exceed autoCompactBytes. Small WSDLs are unaffected.
	SchemaAuto SchemaMode = "auto"
	// SchemaFull always advertises complete self-contained schemas.
	SchemaFull SchemaMode = "full"
	// SchemaCompact always advertises compact schemas.
	SchemaCompact SchemaMode = "compact"
)

// autoCompactBytes is the full tools/list size above which SchemaAuto switches
// to compact schemas. A variable so tests can lower it.
var autoCompactBytes = 1 << 20 // 1 MiB (roughly 250k tokens; already too big for most hosts)

// AutoCompactThreshold is the full tools/list size (bytes) above which
// SchemaAuto switches to compact schemas.
func AutoCompactThreshold() int { return autoCompactBytes }

// describeToolName is the tool that returns a full input schema in compact
// mode. uniqueDescribeName avoids colliding with a WSDL operation of that name.
const describeToolName = "describe_operation"

// ParseSchemaMode reads the top-level "toolSchemas:" key from a gateway
// config.yaml. A missing key means SchemaAuto; an unknown value is an error.
func ParseSchemaMode(configYAML []byte) (SchemaMode, error) {
	var top struct {
		ToolSchemas string `yaml:"toolSchemas"`
	}
	if err := yaml.Unmarshal(configYAML, &top); err != nil {
		return "", fmt.Errorf("parsing config: %w", err)
	}
	switch m := SchemaMode(strings.TrimSpace(top.ToolSchemas)); m {
	case "":
		return SchemaAuto, nil
	case SchemaAuto, SchemaFull, SchemaCompact:
		return m, nil
	default:
		return "", fmt.Errorf("toolSchemas: %q is not valid (use auto, full or compact)", top.ToolSchemas)
	}
}

// ListSize returns the byte size tools/list would have with full schemas.
func ListSize(tools []Tool) int {
	n := 0
	for _, t := range tools {
		b, _ := json.Marshal(toolEntry(t, t.InputSchema, t.Description))
		n += len(b)
	}
	return n
}

// toolEntry is one tools/list element.
func toolEntry(t Tool, input *schema.JSONSchema, description string) map[string]interface{} {
	entry := map[string]interface{}{
		"name":        t.Name,
		"description": description,
		"inputSchema": input,
	}
	if t.Annotations != nil {
		entry["annotations"] = t.Annotations
	}
	return entry
}

// CompactSchema returns a small version of full: the root's own fields keep
// their scalar types, but every nested object (a $ref, an inline object, or
// an array of them) is replaced by a one-line placeholder, and $defs is
// dropped. The bool reports whether anything was summarized; if not, the
// compact schema carries no less information than full.
func CompactSchema(full *schema.JSONSchema) (*schema.JSONSchema, bool) {
	if full == nil {
		return nil, false
	}
	out := &schema.JSONSchema{
		Type:        full.Type,
		Description: full.Description,
		Required:    full.Required,
		Format:      full.Format,
		Nullable:    full.Nullable,
	}
	changed := false
	if len(full.Properties) > 0 {
		out.Properties = make(map[string]*schema.JSONSchema, len(full.Properties))
		for name, p := range full.Properties {
			cp, c := compactProperty(p)
			out.Properties[name] = cp
			changed = changed || c
		}
	}
	// Anything the root defines below the top level (items of a root array,
	// $defs) is dropped; a root with no properties but an Items/Ref counts as
	// summarized.
	if len(full.Properties) == 0 && (full.Items != nil || full.Ref != "") {
		changed = true
		out.Type = "object"
	}
	return out, changed
}

const nestedNote = "Nested object; call " + describeToolName + " for its fields."

func compactProperty(p *schema.JSONSchema) (*schema.JSONSchema, bool) {
	if p == nil {
		return p, false
	}
	switch {
	case p.Ref != "" || p.Type == "object" && len(p.Properties) > 0:
		return &schema.JSONSchema{Type: "object", Description: joinDesc(p.Description, nestedNote), Nullable: p.Nullable}, true
	case p.Type == "array" && p.Items != nil && (p.Items.Ref != "" || p.Items.Type == "object" && len(p.Items.Properties) > 0 || p.Items.Type == "array"):
		return &schema.JSONSchema{
			Type:        "array",
			Items:       &schema.JSONSchema{Type: "object"},
			Description: joinDesc(p.Description, "Array of nested objects; call "+describeToolName+" for their fields."),
			Nullable:    p.Nullable,
		}, true
	}
	return p, false // scalar, enum, array of scalars, or an untyped "any"
}

func joinDesc(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

// describeResult renders a tool's full schema as the describe tool's output.
func describeResult(t *Tool) (interface{}, *rpcError) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t.InputSchema); err != nil {
		return toolError("encoding schema: " + err.Error()), nil
	}
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": strings.TrimSpace(buf.String())}},
		"isError": false,
	}, nil
}
