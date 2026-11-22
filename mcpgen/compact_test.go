package mcpgen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/harshhh28/soapbridge/gateway"
	"github.com/harshhh28/soapbridge/schema"
)

// nestedTool has a scalar, an enum, an array of scalars, a $ref object, an
// array of $ref objects, and an inline object, plus $defs.
func nestedTool(name string) Tool {
	addr := &schema.JSONSchema{Type: "object", Properties: map[string]*schema.JSONSchema{
		"Street": {Type: "string"}, "Zip": {Type: "string"},
	}, Required: []string{"Street"}}
	return Tool{
		Name:        name,
		Description: "Does " + name + ".",
		InputSchema: &schema.JSONSchema{
			Type: "object",
			Defs: map[string]*schema.JSONSchema{"Address": addr},
			Properties: map[string]*schema.JSONSchema{
				"Id":      {Type: "integer"},
				"Kind":    {Type: "string", Enum: []string{"A", "B"}},
				"Tags":    {Type: "array", Items: &schema.JSONSchema{Type: "string"}},
				"Billing": {Ref: "#/$defs/Address", Description: "Where to bill."},
				"Ship":    {Type: "array", Items: &schema.JSONSchema{Ref: "#/$defs/Address"}},
				"Extra":   {Type: "object", Properties: map[string]*schema.JSONSchema{"X": {Type: "string"}}},
			},
			Required: []string{"Id", "Billing"},
		},
	}
}

func TestCompactSchema(t *testing.T) {
	full := nestedTool("T").InputSchema
	c, changed := CompactSchema(full)
	if !changed {
		t.Fatal("nested schema should report changed=true")
	}
	if c.Defs != nil {
		t.Error("$defs must be dropped")
	}
	if c.Type != "object" || len(c.Required) != 2 {
		t.Errorf("root type/required lost: %+v", c)
	}
	for _, scalar := range []string{"Id", "Kind", "Tags"} {
		if c.Properties[scalar] != full.Properties[scalar] {
			t.Errorf("%s (scalar/enum/scalar array) should be kept as is", scalar)
		}
	}
	if len(c.Properties["Kind"].Enum) != 2 {
		t.Error("enum values must be kept")
	}
	b := c.Properties["Billing"]
	if b.Ref != "" || b.Type != "object" || !strings.Contains(b.Description, "Where to bill.") || !strings.Contains(b.Description, describeToolName) {
		t.Errorf("Billing placeholder wrong: %+v", b)
	}
	if s := c.Properties["Ship"]; s.Type != "array" || s.Items == nil || s.Items.Ref != "" {
		t.Errorf("Ship placeholder wrong: %+v", s)
	}
	if e := c.Properties["Extra"]; e.Type != "object" || len(e.Properties) != 0 {
		t.Errorf("inline object should be summarized: %+v", e)
	}
	// The original must be untouched.
	if full.Properties["Billing"].Ref == "" || full.Defs == nil {
		t.Error("CompactSchema mutated its input")
	}

	flat := &schema.JSONSchema{Type: "object", Properties: map[string]*schema.JSONSchema{"A": {Type: "string"}}}
	if _, changed := CompactSchema(flat); changed {
		t.Error("a schema with only scalars should report changed=false")
	}
	if c, changed := CompactSchema(nil); c != nil || changed {
		t.Error("nil schema")
	}
}

func rpc(t *testing.T, s *Server, body string) map[string]interface{} {
	t.Helper()
	var req rpcRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	resp := s.Handle(context.Background(), req)
	b, _ := json.Marshal(resp)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func listed(t *testing.T, s *Server) map[string]map[string]interface{} {
	t.Helper()
	out := map[string]map[string]interface{}{}
	res := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)["result"].(map[string]interface{})
	for _, x := range res["tools"].([]interface{}) {
		m := x.(map[string]interface{})
		out[m["name"].(string)] = m
	}
	return out
}

func newTestServer(mode SchemaMode, tools ...Tool) *Server {
	s := NewServer("t", "0", tools, gateway.NewClient("http://unused.invalid"))
	s.SchemaMode = mode
	return s
}

func TestServer_SchemaModes(t *testing.T) {
	tools := []Tool{nestedTool("Alpha"), nestedTool("Beta")}

	full := listed(t, newTestServer(SchemaFull, tools...))
	if len(full) != 2 || full["describe_operation"] != nil {
		t.Errorf("full mode: want 2 tools and no describe tool, got %d", len(full))
	}
	if _, has := full["Alpha"]["inputSchema"].(map[string]interface{})["$defs"]; !has {
		t.Error("full mode must keep $defs")
	}

	// Auto below the threshold behaves like full.
	small := listed(t, newTestServer(SchemaAuto, tools...))
	if len(small) != 2 {
		t.Errorf("auto below threshold should be full, got %d tools", len(small))
	}

	// Auto above the threshold switches to compact.
	old := autoCompactBytes
	autoCompactBytes = 100
	defer func() { autoCompactBytes = old }()
	srv := newTestServer(SchemaAuto, tools...)
	if !srv.Compact() {
		t.Fatal("auto above threshold should be compact")
	}
	comp := listed(t, srv)
	if len(comp) != 3 || comp["describe_operation"] == nil {
		t.Fatalf("compact mode: want 2 tools + describe_operation, got %v", len(comp))
	}
	in := comp["Alpha"]["inputSchema"].(map[string]interface{})
	if _, has := in["$defs"]; has {
		t.Error("compact mode must not include $defs")
	}
	if !strings.Contains(comp["Alpha"]["description"].(string), "describe_operation") {
		t.Errorf("description should point at the describe tool: %v", comp["Alpha"]["description"])
	}
	if ann := comp["describe_operation"]["annotations"].(map[string]interface{}); ann["readOnlyHint"] != true {
		t.Errorf("describe tool should be read-only: %v", ann)
	}
	// Annotations on real tools survive compaction.
	tools[0].Annotations = InferAnnotations("GetThing")
	if a := listed(t, newTestServer(SchemaCompact, tools...))["Alpha"]["annotations"]; a == nil {
		t.Error("annotations lost in compact mode")
	}

	// Forcing compact works regardless of size; a flat tool gets no note.
	flat := Tool{Name: "Flat", Description: "Flat op.", InputSchema: &schema.JSONSchema{Type: "object", Properties: map[string]*schema.JSONSchema{"A": {Type: "string"}}}}
	autoCompactBytes = old
	c2 := listed(t, newTestServer(SchemaCompact, flat))
	if strings.Contains(c2["Flat"]["description"].(string), "describe_operation") {
		t.Error("a tool with nothing summarized should keep its description unchanged")
	}
}

func TestServer_DescribeTool(t *testing.T) {
	srv := newTestServer(SchemaCompact, nestedTool("Alpha"))
	res := rpc(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"describe_operation","arguments":{"name":"Alpha"}}}`)["result"].(map[string]interface{})
	if res["isError"] != false {
		t.Fatalf("describe errored: %v", res)
	}
	text := res["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	var got schema.JSONSchema
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("describe output is not a schema: %v\n%s", err, text)
	}
	if got.Defs["Address"] == nil || got.Properties["Billing"].Ref == "" {
		t.Errorf("describe must return the FULL schema with $defs: %s", text)
	}
	for _, args := range []string{`{}`, `{"name":"Nope"}`} {
		r := rpc(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"describe_operation","arguments":`+args+`}}`)["result"].(map[string]interface{})
		if r["isError"] != true {
			t.Errorf("describe with %s should be a tool error, got %v", args, r)
		}
	}
	// In full mode the describe tool does not exist.
	r := rpc(t, newTestServer(SchemaFull, nestedTool("Alpha")), `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"describe_operation","arguments":{"name":"Alpha"}}}`)
	if r["error"] == nil {
		t.Error("describe_operation should be an unknown tool in full mode")
	}
}

// Validation must keep using the full schema even when the listing is compact.
func TestServer_ValidationUsesFullSchema(t *testing.T) {
	srv := newTestServer(SchemaCompact, nestedTool("Alpha"))
	// Billing is required and nested; its own required field Street is only in $defs.
	r := rpc(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"Alpha","arguments":{"Id":1,"Billing":{"Zip":"1"}}}}`)["result"].(map[string]interface{})
	msg := r["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if r["isError"] != true || !strings.Contains(msg, "Street") {
		t.Errorf("nested required field must still be enforced from the full schema, got %v", msg)
	}
}

func TestDescribeNameCollision(t *testing.T) {
	srv := newTestServer(SchemaCompact, nestedTool("describe_operation"))
	got := listed(t, srv)
	if len(got) != 2 || got["describe_operation_"] == nil {
		t.Errorf("describe tool should be renamed to avoid the collision: %v", keys(got))
	}
}

func keys(m map[string]map[string]interface{}) []string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	return k
}

func TestParseSchemaMode(t *testing.T) {
	for in, want := range map[string]SchemaMode{
		"":                          SchemaAuto,
		"name: x\n":                 SchemaAuto,
		"toolSchemas: full\n":       SchemaFull,
		"toolSchemas: compact\n":    SchemaCompact,
		"toolSchemas: auto\n":       SchemaAuto,
		"toolSchemas: \" full \"\n": SchemaFull,
	} {
		got, err := ParseSchemaMode([]byte(in))
		if err != nil || got != want {
			t.Errorf("ParseSchemaMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"toolSchemas: tiny\n", "toolSchemas: [a]\n", "toolSchemas: [unclosed"} {
		if _, err := ParseSchemaMode([]byte(bad)); err == nil {
			t.Errorf("ParseSchemaMode(%q) should fail", bad)
		}
	}
}

func TestListSizeAndAbbreviate(t *testing.T) {
	if ListSize(nil) != 0 || ListSize([]Tool{nestedTool("A")}) <= 0 {
		t.Error("ListSize")
	}
	if got := Abbreviate([]string{"a", "b", "c"}, 10); len(got) != 3 {
		t.Errorf("under max should be unchanged: %v", got)
	}
	got := Abbreviate([]string{"a", "b", "c", "d", "e"}, 2)
	if strings.Join(got, ",") != "a,b,and 3 more" {
		t.Errorf("Abbreviate = %v", got)
	}
}
