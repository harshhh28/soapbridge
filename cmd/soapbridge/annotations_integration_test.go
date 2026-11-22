package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a strings.Builder safe for the child process's output
// goroutine to write while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

// noteLine returns the single "NOTE: N of M MCP tools ..." line from generate
// output, or "" if there is none.
func noteLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "NOTE:") {
			return l
		}
	}
	return ""
}

func runGenerate(t *testing.T, repoRoot, wsdlPath, outDir string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "run", "./cmd/soapbridge", "generate",
		"--wsdl", wsdlPath, "--out", outDir, "--soapbridge-dir", repoRoot, "--mcp", "--openapi")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// startGateway builds and runs the generated module, returning its base URL.
func startGateway(t *testing.T, outDir string) (baseURL string, output func() string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gw")
	b := exec.Command("go", "build", "-o", bin, ".")
	b.Dir = outDir
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("build generated server: %v\n%s", err, out)
	}
	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = outDir
	cmd.Env = append(os.Environ(), "ADDR="+addr)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	sb := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = sb, sb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	if !waitForHealthy("http://"+addr+"/healthz", 20*time.Second) {
		t.Fatalf("gateway never healthy:\n%s", sb.String())
	}
	return "http://" + addr, sb.String
}

func listTools(t *testing.T, baseURL string) map[string]map[string]interface{} {
	t.Helper()
	resp, err := http.Post(baseURL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
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
		t.Fatalf("%v: %s", err, data)
	}
	out := map[string]map[string]interface{}{}
	for _, tl := range body.Result.Tools {
		out[tl.Name] = tl.Annotations
	}
	return out
}

// TestAnnotationOverrides_EndToEnd drives the real CLI: generate (warns about
// unannotated ops) -> add overrides to config.yaml -> regenerate (overrides
// survive, warning shrinks) -> build and run -> tools/list reflects overrides.
func TestAnnotationOverrides_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	repoRoot, _ := filepath.Abs("../..")
	wsdlPath := filepath.Join(repoRoot, "testdata", "wsdl", "countryinfo.wsdl")
	outDir := filepath.Join(t.TempDir(), "gw")

	// 1. First generate: noun-style lookups are reported, config has the hint.
	out, err := runGenerate(t, repoRoot, wsdlPath, outDir)
	if err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	note := noteLine(out)
	if note == "" || !strings.Contains(note, "CountryName") || !strings.Contains(note, "CurrencyName") {
		t.Errorf("generate output should list unannotated noun-style ops:\n%s", out)
	}
	if strings.Contains(note, "ListOf") {
		t.Errorf("List* ops are guessed read-only and must not be reported: %s", note)
	}
	cfgPath := filepath.Join(outDir, "config.yaml")
	cfg, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfg), "# annotations:") {
		t.Errorf("fresh config.yaml should carry the commented hint:\n%s", cfg)
	}

	// 2. Add overrides: promote two nouns, demote a List* the heuristic got
	//    "wrong" (pretend it mutates), include a typo'd name.
	edited := string(cfg) + "\nannotations:\n  readOnly: [CountryName, CurrencyName, CountryISOCode, NoSuchOp]\n  mutating: [ListOfContinentsByName]\n"
	if err := os.WriteFile(cfgPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Regenerate into the same dir: overrides must survive, warning shrinks.
	out, err = runGenerate(t, repoRoot, wsdlPath, outDir)
	if err != nil {
		t.Fatalf("regenerate: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"NoSuchOp"`) {
		t.Errorf("typo'd override should warn at generate time:\n%s", out)
	}
	if note := noteLine(out); strings.Contains(note, "CountryName") || strings.Contains(note, "CurrencyName") || strings.Contains(note, "CountryISOCode") {
		t.Errorf("overridden ops should no longer be reported: %s", note)
	}
	if note := noteLine(out); !strings.Contains(note, "CapitalCity") {
		t.Errorf("ops without an override must still be reported: %q", note)
	}
	cfg2, _ := os.ReadFile(cfgPath)
	for _, want := range []string{"CountryName", "CountryISOCode", "ListOfContinentsByName", "NoSuchOp"} {
		if !strings.Contains(string(cfg2), want) {
			t.Errorf("regenerate wiped override %q from config.yaml:\n%s", want, cfg2)
		}
	}

	// 4. Run it and read the real wire output.
	base, logs := startGateway(t, outDir)
	tools := listTools(t, base)
	if a := tools["CountryName"]; a["readOnlyHint"] != true || a["idempotentHint"] != true {
		t.Errorf("CountryName override not applied: %v", a)
	}
	if a := tools["ListOfContinentsByName"]; a["readOnlyHint"] != false || a["destructiveHint"] != false {
		t.Errorf("mutating demotion of a heuristic read not applied: %v", a)
	}
	if a := tools["ListOfContinentsByCode"]; a["readOnlyHint"] != true {
		t.Errorf("untouched List* should keep heuristic read-only: %v", a)
	}
	if !strings.Contains(logs(), `"NoSuchOp"`) {
		t.Errorf("gateway should log a warning for the unknown override name:\n%s", logs())
	}
}

func TestAnnotationOverrides_BadConfigFailsLoudly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	repoRoot, _ := filepath.Abs("../..")
	wsdlPath := filepath.Join(repoRoot, "testdata", "wsdl", "calculator.wsdl")

	for name, section := range map[string]string{
		"typo key": "annotations:\n  readonly: [Add]\n",
		"conflict": "annotations:\n  readOnly: [Add]\n  destructive: [Add]\n",
	} {
		t.Run(name, func(t *testing.T) {
			outDir := filepath.Join(t.TempDir(), "gw")
			if out, err := runGenerate(t, repoRoot, wsdlPath, outDir); err != nil {
				t.Fatalf("initial generate: %v\n%s", err, out)
			}
			cfgPath := filepath.Join(outDir, "config.yaml")
			cfg, _ := os.ReadFile(cfgPath)
			if err := os.WriteFile(cfgPath, append(cfg, []byte("\n"+section)...), 0o644); err != nil {
				t.Fatal(err)
			}
			// generate must refuse...
			if out, err := runGenerate(t, repoRoot, wsdlPath, outDir); err == nil {
				t.Errorf("generate should fail on bad annotations, got success:\n%s", out)
			}
			// ...and so must a gateway built from the bad config.
			bin := filepath.Join(t.TempDir(), "gw")
			b := exec.Command("go", "build", "-o", bin, ".")
			b.Dir = outDir
			if out, err := b.CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, out)
			}
			run := exec.Command(bin)
			run.Dir = outDir
			run.Env = append(os.Environ(), "ADDR=127.0.0.1:"+strconv.Itoa(freePort(t)))
			out, err := run.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "config.yaml") {
				t.Errorf("gateway should exit non-zero naming config.yaml; err=%v out=%s", err, out)
			}
		})
	}
}

// Regression: a gateway generated with --mcp=false must still compile.
func TestGenerateWithoutMCPBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	repoRoot, _ := filepath.Abs("../..")
	outDir := filepath.Join(t.TempDir(), "gw")
	cmd := exec.Command("go", "run", "./cmd/soapbridge", "generate",
		"--wsdl", filepath.Join(repoRoot, "testdata", "wsdl", "calculator.wsdl"),
		"--out", outDir, "--soapbridge-dir", repoRoot, "--mcp=false")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate --mcp=false: %v\n%s", err, out)
	}
}
