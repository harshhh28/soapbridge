package mcpgen

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harshhh28/soapbridge/gateway"
)

func toolsNamed(names ...string) []Tool {
	var ts []Tool
	for _, n := range names {
		ts = append(ts, Tool{Name: n, Annotations: InferAnnotations(n)})
	}
	return ts
}

func find(ts []Tool, name string) *Tool {
	for i := range ts {
		if ts[i].Name == name {
			return &ts[i]
		}
	}
	return nil
}

func TestParseOverrides(t *testing.T) {
	good := "name: x\naddr: :8080\nmcp: true\nannotations:\n  readOnly: [A, B]\n  mutating: [C]\n  destructive: [D]\n"
	o, err := ParseOverrides([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if len(o.ReadOnly) != 2 || len(o.Mutating) != 1 || len(o.Destructive) != 1 {
		t.Errorf("parsed %+v", o)
	}

	for name, in := range map[string]string{
		"no section":    "name: x\n",
		"empty":         "",
		"only comments": "# annotations:\n#   readOnly: [A]\n",
		"empty section": "annotations: {}\n",
	} {
		o, err := ParseOverrides([]byte(in))
		if err != nil || !o.IsZero() {
			t.Errorf("%s: got %+v, %v; want zero overrides, nil", name, o, err)
		}
	}

	// A typo must be an error, not silently ignored.
	if _, err := ParseOverrides([]byte("annotations:\n  readonly: [A]\n")); err == nil {
		t.Error("typo key 'readonly' should be rejected")
	}
	// Wrong value type.
	if _, err := ParseOverrides([]byte("annotations:\n  readOnly: A\n")); err == nil {
		t.Error("scalar instead of list should be rejected")
	}
	if _, err := ParseOverrides([]byte("annotations: [a")); err == nil {
		t.Error("malformed yaml should be rejected")
	}
}

func TestApplyOverrides(t *testing.T) {
	tools := toolsNamed("GetNextTicket", "CountryName", "DeleteUser", "CreateUser", "GetUser", "Untouched")
	warns, err := ApplyOverrides(tools, Overrides{
		ReadOnly:    []string{"CountryName"},
		Mutating:    []string{"GetNextTicket", "CreateUser"},
		Destructive: []string{"PurgeCache", "DeleteUser"}, // PurgeCache doesn't exist
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "PurgeCache") {
		t.Errorf("warnings = %v, want one about PurgeCache", warns)
	}

	// Promote: noun-style read.
	a := find(tools, "CountryName").Annotations
	if !eq(a.ReadOnlyHint, ptr(true)) || !eq(a.IdempotentHint, ptr(true)) || a.DestructiveHint != nil {
		t.Errorf("CountryName: %+v", a)
	}
	// Demote: a Get* that the heuristic called read-only.
	if find(tools, "GetNextTicket").Annotations.ReadOnlyHint == nil || !eq(find(tools, "GetNextTicket").Annotations.ReadOnlyHint, ptr(false)) {
		t.Error("GetNextTicket should be demoted to readOnlyHint=false")
	}
	if a := find(tools, "GetNextTicket").Annotations; !eq(a.DestructiveHint, ptr(false)) || a.IdempotentHint != nil {
		t.Errorf("mutating: destructive must be explicit false and idempotent cleared, got %+v", a)
	}
	if a := find(tools, "DeleteUser").Annotations; !eq(a.DestructiveHint, ptr(true)) || !eq(a.ReadOnlyHint, ptr(false)) {
		t.Errorf("DeleteUser: %+v", a)
	}
	// Untouched keeps its heuristic result; GetUser stays read-only.
	if find(tools, "Untouched").Annotations.ReadOnlyHint != nil {
		t.Error("Untouched should stay unannotated")
	}
	if !eq(find(tools, "GetUser").Annotations.ReadOnlyHint, ptr(true)) {
		t.Error("GetUser should keep its heuristic read-only hint")
	}
	for _, tl := range tools {
		if tl.Annotations.OpenWorldHint == nil || !*tl.Annotations.OpenWorldHint {
			t.Errorf("%s lost openWorldHint", tl.Name)
		}
	}
}

func TestApplyOverrides_Conflict(t *testing.T) {
	tools := toolsNamed("A")
	_, err := ApplyOverrides(tools, Overrides{ReadOnly: []string{"A"}, Destructive: []string{"A"}})
	if err == nil || !strings.Contains(err.Error(), `"A"`) {
		t.Errorf("want conflict error naming A, got %v", err)
	}
	// Conflict must leave tools untouched (validated before applying).
	if tools[0].Annotations.ReadOnlyHint != nil {
		t.Error("tools modified despite conflict error")
	}
	// Same name twice in the same list is harmless.
	if _, err := ApplyOverrides(toolsNamed("A"), Overrides{ReadOnly: []string{"A", "A"}}); err != nil {
		t.Errorf("duplicate within one list should be fine: %v", err)
	}
	// Blank entries are ignored.
	if w, err := ApplyOverrides(toolsNamed("A"), Overrides{ReadOnly: []string{"", "  "}}); err != nil || len(w) != 0 {
		t.Errorf("blank entries: %v %v", w, err)
	}
}

func TestApplyOverrides_CaseSensitive(t *testing.T) {
	tools := toolsNamed("CountryName")
	w, _ := ApplyOverrides(tools, Overrides{ReadOnly: []string{"countryname"}})
	if len(w) != 1 || tools[0].Annotations.ReadOnlyHint != nil {
		t.Errorf("wrong-case name should warn and not apply; warnings=%v", w)
	}
}

func TestUnannotated(t *testing.T) {
	tools := toolsNamed("GetA", "Zed", "DeleteB", "Alpha")
	got := Unannotated(tools)
	if strings.Join(got, ",") != "Alpha,Zed" {
		t.Errorf("Unannotated = %v", got)
	}
	_, _ = ApplyOverrides(tools, Overrides{Mutating: []string{"Zed"}, ReadOnly: []string{"Alpha"}})
	if got := Unannotated(tools); len(got) != 0 {
		t.Errorf("after overrides still unannotated: %v", got)
	}
}

// End to end through the real tools/list wire format.
func TestToolsListWireFormat_WithOverrides(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	tools := GenerateTools(def)
	if _, err := ApplyOverrides(tools, Overrides{ReadOnly: []string{"Add"}, Destructive: []string{"Divide"}}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer("calc", "0.0.1", tools, gateway.NewClient("http://unused.invalid"))
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var body struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Annotations map[string]interface{} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]interface{}{}
	for _, tl := range body.Result.Tools {
		got[tl.Name] = tl.Annotations
	}
	if got["Add"]["readOnlyHint"] != true || got["Add"]["idempotentHint"] != true {
		t.Errorf("Add: %v", got["Add"])
	}
	if got["Divide"]["destructiveHint"] != true || got["Divide"]["readOnlyHint"] != false {
		t.Errorf("Divide: %v", got["Divide"])
	}
	// false must appear on the wire, not be dropped by omitempty.
	if v, ok := got["Divide"]["readOnlyHint"]; !ok || v != false {
		t.Errorf("explicit readOnlyHint=false missing from wire output: %v", got["Divide"])
	}
	if _, has := got["Subtract"]["readOnlyHint"]; has {
		t.Errorf("Subtract should have no readOnlyHint: %v", got["Subtract"])
	}
}
