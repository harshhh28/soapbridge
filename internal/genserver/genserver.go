// Package genserver scaffolds a standalone, runnable Go module implementing
// the REST/MCP/OpenAPI gateway for one parsed WSDL. The generated module
// depends on this repo's internal packages (gateway, mcpgen, openapigen,
// wsdl) via a go.mod replace directive rather than duplicating their logic,
// so a generated server always runs the exact same SOAP-calling code this
// repo's own tests exercise.
package genserver

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed templates/main.go.tmpl
var templatesFS embed.FS

// SoapbridgeModulePath is this project's own module path — fixed, since
// it's this repo's identity, not something a generate invocation varies.
// The generated main.go template imports gateway/mcpgen/openapigen/wsdl
// under this path directly; goModContent uses it to declare the
// dependency in the generated go.mod.
const SoapbridgeModulePath = "github.com/harshhh28/soapbridge"

// Options configures one generate run.
type Options struct {
	OutDir         string // destination directory (created if missing)
	ModulePath     string // Go module path for the generated go.mod, e.g. "gateway"
	GoVersion      string // e.g. "1.22"
	ServiceName    string
	ServiceVersion string
	WSDLSource     []byte // raw WSDL bytes, embedded verbatim as service.wsdl
	WSDLOrigin     string // the --wsdl value the user passed, recorded for humans/README
	OperationCount int
	EnableMCP      bool
	EnableOpenAPI  bool
	Addr           string

	// Exactly one of these determines how the generated go.mod depends on
	// soapbridge:
	//   - SoapbridgeDir set: a local "replace" to this absolute path (dev
	//     workflow — running generate from within a soapbridge checkout).
	//   - PinnedVersion set: a plain "require github.com/.../soapbridge
	//     vX.Y.Z", resolved from the module proxy like any other
	//     dependency — no local checkout needed. This is what makes
	//     "soapbridge generate" work for someone who only has an
	//     installed binary (e.g. via Homebrew or "go install"), once
	//     soapbridge itself has a tagged release at that version.
	SoapbridgeDir string
	PinnedVersion string
}

// Config is the shape of the generated config.yaml, read back by
// `soapbridge serve --config`.
type Config struct {
	Name    string `yaml:"name"`
	Addr    string `yaml:"addr"`
	MCP     bool   `yaml:"mcp"`
	OpenAPI bool   `yaml:"openapi"`
}

// Scaffold writes a complete Go module to opts.OutDir.
func Scaffold(opts Options) error {
	if opts.GoVersion == "" {
		opts.GoVersion = "1.22"
	}
	if opts.Addr == "" {
		opts.Addr = ":8080"
	}
	if opts.ServiceVersion == "" {
		opts.ServiceVersion = "0.1.0"
	}
	if opts.ModulePath == "" {
		opts.ModulePath = "gateway"
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	if err := writeFile(filepath.Join(opts.OutDir, "service.wsdl"), opts.WSDLSource); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(opts.OutDir, "go.mod"), []byte(goModContent(opts))); err != nil {
		return err
	}
	if err := writeMain(opts); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(opts.OutDir, "README.md"), []byte(readmeContent(opts))); err != nil {
		return err
	}

	cfg := Config{Name: opts.ServiceName, Addr: opts.Addr, MCP: opts.EnableMCP, OpenAPI: opts.EnableOpenAPI}
	cfgBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config.yaml: %w", err)
	}
	if err := writeFile(filepath.Join(opts.OutDir, "config.yaml"), cfgBytes); err != nil {
		return err
	}

	return nil
}

func writeMain(opts Options) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/main.go.tmpl")
	if err != nil {
		return fmt.Errorf("loading main.go template: %w", err)
	}
	f, err := os.Create(filepath.Join(opts.OutDir, "main.go"))
	if err != nil {
		return err
	}
	if err := tmpl.Execute(f, opts); err != nil {
		_ = f.Close()
		return fmt.Errorf("rendering main.go: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing main.go: %w", err)
	}
	return nil
}

func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func goModContent(opts Options) string {
	if opts.SoapbridgeDir != "" {
		return fmt.Sprintf(`module %s

go %s

require %s v0.0.0

replace %s => %s
`, opts.ModulePath, opts.GoVersion, SoapbridgeModulePath, SoapbridgeModulePath, opts.SoapbridgeDir)
	}

	version := opts.PinnedVersion
	if version == "" {
		version = "latest" // last resort; Scaffold's caller should always set one of the two
	}
	return fmt.Sprintf(`module %s

go %s

require %s %s
`, opts.ModulePath, opts.GoVersion, SoapbridgeModulePath, version)
}

func readmeContent(opts Options) string {
	mcp := "disabled"
	if opts.EnableMCP {
		mcp = "enabled (HTTP: POST /mcp, or run with -stdio for local agent use)"
	}
	openapi := "disabled"
	if opts.EnableOpenAPI {
		openapi = "enabled (GET /openapi.json)"
	}
	return fmt.Sprintf(`# %s gateway

Generated by soapbridge from %s. %d SOAP operation(s) are exposed as:

- REST: POST /api/{OperationName}  (JSON in, JSON out)
- MCP:  %s
- OpenAPI: %s

## Run

	go run .

Override the listen address with -addr or the ADDR environment variable
(default %s). Do not hand-edit the WSDL parsing/gateway wiring in main.go —
re-run "soapbridge generate" instead; everything below that line is yours.
`, opts.ServiceName, opts.WSDLOrigin, opts.OperationCount, mcp, openapi, opts.Addr)
}
