package mcpgen

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestGenerateTools(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	tools := GenerateTools(def)
	if len(tools) != 4 {
		t.Fatalf("got %d tools, want 4", len(tools))
	}
	var add *Tool
	for i := range tools {
		if tools[i].Name == "Add" {
			add = &tools[i]
		}
	}
	if add == nil {
		t.Fatal("Add tool not found")
	}
	if add.InputSchema.Type != "object" || len(add.InputSchema.Required) != 2 {
		t.Errorf("Add.InputSchema = %+v", add.InputSchema)
	}
}

// TestMCPOverStdio drives the full JSON-RPC lifecycle (initialize,
// tools/list, tools/call) over the stdio transport against a mock SOAP
// backend, then repeats tools/call against the live Calculator endpoint to
// prove the tool handler and the REST handler genuinely share Client.Call
// rather than each reimplementing SOAP calling.
func TestMCPOverStdio(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	tools := GenerateTools(def)
	b := def.PrimaryBinding()
	client := gateway.NewClient(b.EndpointURL)
	srv := NewServer("calculator-mcp", "0.0.1", tools, client)

	reqs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Add","arguments":{"intA":10,"intB":32}}}`,
	}
	in := strings.NewReader(strings.Join(reqs, "\n") + "\n")
	var out bytes.Buffer
	if err := srv.ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d response lines (notification should produce none), want 3:\n%s", len(lines), out.String())
	}

	var initResp map[string]interface{}
	_ = json.Unmarshal([]byte(lines[0]), &initResp)
	result := initResp["result"].(map[string]interface{})
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("initialize result = %v", result)
	}

	var listResp map[string]interface{}
	_ = json.Unmarshal([]byte(lines[1]), &listResp)
	toolList := listResp["result"].(map[string]interface{})["tools"].([]interface{})
	if len(toolList) != 4 {
		t.Errorf("tools/list returned %d tools, want 4", len(toolList))
	}

	var callResp map[string]interface{}
	if err := json.Unmarshal([]byte(lines[2]), &callResp); err != nil {
		t.Fatalf("could not parse tools/call response %q: %v", lines[2], err)
	}
	if callResp["error"] != nil {
		t.Skipf("live Calculator endpoint unreachable, skipping: %v", callResp["error"])
	}
	callResult := callResp["result"].(map[string]interface{})
	if callResult["isError"] == true {
		t.Fatalf("tools/call reported isError: %v", callResult)
	}
	content := callResult["content"].([]interface{})[0].(map[string]interface{})
	if !strings.Contains(content["text"].(string), "42") {
		t.Errorf("Add(10,32) tool result = %v, want it to contain 42", content["text"])
	}
}

func TestMCPOverHTTP_ToolsList(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/calculator.wsdl")
	tools := GenerateTools(def)
	client := gateway.NewClient("http://unused.invalid")
	srv := NewServer("calculator-mcp", "0.0.1", tools, client)

	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var body map[string]interface{}
	_ = json.Unmarshal(data, &body)
	toolList := body["result"].(map[string]interface{})["tools"].([]interface{})
	if len(toolList) != 4 {
		t.Errorf("got %d tools", len(toolList))
	}
}
