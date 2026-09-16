package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestGenerateAndServe is the end-to-end integration test: it runs the
// actual `soapbridge generate` CLI against a fixture WSDL (with its
// service address rewritten to a local mock SOAP server, so the test is
// deterministic and doesn't depend on network access — live-network
// coverage against the real public Calculator service already lives in
// gateway.TestLive_Calculator and mcpgen.TestMCPOverStdio), builds the
// resulting standalone Go module, runs it as a subprocess, and verifies it
// answers a real REST request correctly end to end.
func TestGenerateAndServe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><AddResponse xmlns="http://tempuri.org/"><AddResult>19</AddResult></AddResponse></soap:Body></soap:Envelope>`))
	}))
	defer mock.Close()

	origWSDL, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "wsdl", "calculator.wsdl"))
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.ReplaceAll(string(origWSDL), "http://www.dneonline.com/calculator.asmx", mock.URL)

	workDir := t.TempDir()
	wsdlPath := filepath.Join(workDir, "calculator.wsdl")
	if err := os.WriteFile(wsdlPath, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(workDir, "gateway")

	genCmd := exec.Command("go", "run", "./cmd/soapbridge", "generate",
		"--wsdl", wsdlPath,
		"--out", outDir,
		"--soapbridge-dir", repoRoot,
		"--mcp", "--openapi",
	)
	genCmd.Dir = repoRoot
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("soapbridge generate failed: %v\n%s", err, out)
	}

	for _, f := range []string{"main.go", "go.mod", "service.wsdl", "config.yaml"} {
		if _, err := os.Stat(filepath.Join(outDir, f)); err != nil {
			t.Errorf("expected generated file %s: %v", f, err)
		}
	}

	// Build a real binary rather than using `go run .`: `go run` spawns
	// the compiled program as a *child of the go tool*, so killing the
	// "go run" process (what CommandContext's cancellation does) leaves
	// that grandchild running and holding the inherited stdout pipe open
	// forever — cmd.Wait() then never returns. Running the compiled
	// binary directly makes it the direct child, so cancellation works.
	serverBin := filepath.Join(workDir, "gateway-server")
	buildCmd := exec.Command("go", "build", "-o", serverBin, ".")
	buildCmd.Dir = outDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("building generated server: %v\n%s", err, out)
	}

	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runCmd := exec.CommandContext(ctx, serverBin)
	runCmd.Dir = outDir
	runCmd.Env = append(os.Environ(), "ADDR="+addr)
	runCmd.Cancel = func() error { return runCmd.Process.Kill() }
	var serverOutput strings.Builder
	runCmd.Stdout = &serverOutput
	runCmd.Stderr = &serverOutput
	if err := runCmd.Start(); err != nil {
		t.Fatalf("starting generated server: %v", err)
	}
	defer func() {
		cancel()
		_ = runCmd.Wait()
	}()

	baseURL := "http://" + addr
	if !waitForHealthy(baseURL+"/healthz", 20*time.Second) {
		t.Fatalf("generated server never became healthy; output:\n%s", serverOutput.String())
	}

	resp, err := http.Post(baseURL+"/api/Add", "application/json", strings.NewReader(`{"intA": 4, "intB": 5}`))
	if err != nil {
		t.Fatalf("POST /api/Add: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "19") {
		t.Errorf("body = %s, want it to contain the mock's AddResult (19)", body)
	}

	openapiResp, err := http.Get(baseURL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openapiResp.Body.Close() }()
	if openapiResp.StatusCode != http.StatusOK {
		t.Errorf("GET /openapi.json status = %d", openapiResp.StatusCode)
	}

	mcpResp, err := http.Post(baseURL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcpResp.Body.Close() }()
	mcpBody, _ := io.ReadAll(mcpResp.Body)
	if !strings.Contains(string(mcpBody), `"name":"Add"`) {
		t.Errorf("POST /mcp tools/list body = %s, want it to list the Add tool", mcpBody)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForHealthy(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
