package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harshhh28/soapbridge/internal/genserver"
	"github.com/harshhh28/soapbridge/wsdl"
)

func newGenerateCmd() *cobra.Command {
	var (
		wsdlSource    string
		outDir        string
		enableMCP     bool
		enableOpenAPI bool
		modulePath    string
		serviceName   string
		addr          string
		soapbridgeDir string
		endpoint      string
		runAfter      bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a runnable REST/MCP/OpenAPI gateway from a WSDL",
		Example: "  soapbridge generate --wsdl ./service.wsdl --out ./gateway --mcp --openapi\n" +
			"  soapbridge generate --wsdl https://example.com/Service.svc?WSDL --out ./gateway --run",
		RunE: func(cmd *cobra.Command, args []string) error {
			printf := func(format string, a ...any) { _, _ = fmt.Fprintf(cmd.OutOrStdout(), format, a...) }

			if wsdlSource == "" {
				return fmt.Errorf("--wsdl is required")
			}

			printf("Loading WSDL from %s...\n", wsdlSource)
			res, err := wsdl.Load(wsdlSource)
			if err != nil {
				return fmt.Errorf("loading WSDL: %w", err)
			}
			for _, w := range res.Warnings {
				printf("  WARNING: %s\n", w)
			}
			def := res.Definition
			if len(def.Operations) == 0 {
				return fmt.Errorf("WSDL parsed successfully but declares no operations soapbridge could resolve")
			}

			name := serviceName
			if name == "" {
				name = def.Name
			}
			if name == "" {
				name = strings.TrimSuffix(filepath.Base(wsdlSource), filepath.Ext(wsdlSource))
			}

			// The generated module needs to depend on soapbridge itself
			// somehow: a local checkout (dev workflow: --soapbridge-dir,
			// or auto-detected by walking up from cwd) if one is
			// available, otherwise the CLI's own pinned release version,
			// resolved from the module proxy like any other dependency —
			// which is what makes this work for someone who only has an
			// installed binary (go install / Homebrew), no checkout.
			pinnedVersion := ""
			if soapbridgeDir == "" {
				soapbridgeDir, err = findSoapbridgeModuleDir()
				if err != nil {
					if cliVersion == "dev" {
						return fmt.Errorf(
							"%w\n\nthis soapbridge build has no release version embedded (a \"dev\" build),\n"+
								"so it can't fall back to a published dependency. Pass --soapbridge-dir\n"+
								"pointing at a soapbridge checkout (the directory containing its go.mod)", err)
					}
					pinnedVersion = cliVersion
					soapbridgeDir = ""
				}
			}
			if soapbridgeDir != "" {
				soapbridgeDir, err = filepath.Abs(soapbridgeDir)
				if err != nil {
					return err
				}
				printf("Building the generated module against the local soapbridge checkout at %s\n", soapbridgeDir)
			} else {
				printf("Building the generated module against soapbridge %s (via the Go module proxy)\n", pinnedVersion)
			}

			outAbs, err := filepath.Abs(outDir)
			if err != nil {
				return err
			}
			if err := checkOutDirSafe(outAbs); err != nil {
				return err
			}

			wsdlBytes, err := wsdl.Fetch(wsdlSource)
			if err != nil {
				return fmt.Errorf("re-reading WSDL source to embed it: %w", err)
			}

			if endpoint != "" {
				b := def.PrimaryBinding()
				if b == nil {
					return fmt.Errorf("cannot override endpoint: WSDL has no usable SOAP binding")
				}
				marker := []byte(`location="` + b.EndpointURL + `"`)
				replacement := []byte(`location="` + endpoint + `"`)
				replaced := bytes.Replace(wsdlBytes, marker, replacement, 1)
				if bytes.Equal(replaced, wsdlBytes) {
					return fmt.Errorf("could not find %s in the WSDL to replace with --endpoint", marker)
				}
				wsdlBytes = replaced
				printf("Overriding SOAP endpoint: %s -> %s\n", b.EndpointURL, endpoint)
			}

			opts := genserver.Options{
				OutDir:         outAbs,
				ModulePath:     modulePath,
				SoapbridgeDir:  soapbridgeDir,
				PinnedVersion:  pinnedVersion,
				ServiceName:    name,
				WSDLSource:     wsdlBytes,
				WSDLOrigin:     wsdlSource,
				OperationCount: len(def.Operations),
				EnableMCP:      enableMCP,
				EnableOpenAPI:  enableOpenAPI,
				Addr:           addr,
			}
			if err := genserver.Scaffold(opts); err != nil {
				return fmt.Errorf("scaffolding generated server: %w", err)
			}

			printf("Generated %s (%d operations) into %s\n", name, len(def.Operations), outAbs)
			for _, op := range def.Operations {
				printf("  POST /api/%s\n", op.Name)
			}

			tidyCmd := exec.Command("go", "mod", "tidy")
			tidyCmd.Dir = outAbs
			if out, err := tidyCmd.CombinedOutput(); err != nil {
				printf("\n`go mod tidy` failed:\n%s\n", out)
				return fmt.Errorf("go mod tidy failed in %s: %w", outAbs, err)
			}

			buildCmd := exec.Command("go", "build", "-o", os.DevNull, "./...")
			buildCmd.Dir = outAbs
			if out, err := buildCmd.CombinedOutput(); err != nil {
				printf("\nGenerated module does not build yet:\n%s\n", out)
				return fmt.Errorf("go build failed in %s: %w", outAbs, err)
			}
			printf("Generated module builds cleanly.\n")

			if runAfter {
				printf("\nStarting the generated server (go run .) in %s ...\n", outAbs)
				runCmd := exec.Command("go", "run", ".")
				runCmd.Dir = outAbs
				runCmd.Stdout = cmd.OutOrStdout()
				runCmd.Stderr = cmd.ErrOrStderr()
				runCmd.Stdin = cmd.InOrStdin()
				return runCmd.Run()
			}
			printf("\nRun it with:\n  cd %s && go run .\nor:\n  soapbridge serve --config %s\n", outAbs, filepath.Join(outAbs, "config.yaml"))
			return nil
		},
	}

	cmd.Flags().StringVar(&wsdlSource, "wsdl", "", "path or URL to the WSDL document (required)")
	cmd.Flags().StringVar(&outDir, "out", "./gateway", "output directory for the generated Go module")
	cmd.Flags().BoolVar(&enableMCP, "mcp", true, "generate an MCP tool server alongside the REST API")
	cmd.Flags().BoolVar(&enableOpenAPI, "openapi", true, "generate an OpenAPI 3.0 document alongside the REST API")
	cmd.Flags().StringVar(&modulePath, "module", "gateway", "Go module path for the generated go.mod")
	cmd.Flags().StringVar(&serviceName, "name", "", "service name (defaults to the WSDL's own service name)")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "default REST listen address for the generated server")
	cmd.Flags().StringVar(&soapbridgeDir, "soapbridge-dir", "", "path to a local soapbridge checkout to build against, for developing soapbridge itself (auto-detected; a released binary falls back to its own version from the module proxy if omitted and no checkout is found)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "override the WSDL's declared SOAP endpoint (e.g. to point at a local 'soapbridge mock' server instead of what the WSDL says)")
	cmd.Flags().BoolVar(&runAfter, "run", false, "build and immediately run the generated server (fast demo loop)")

	return cmd
}

// checkOutDirSafe refuses to scaffold into a directory that already
// contains Go source not belonging to a prior soapbridge-generated output.
// Without this, --out defaulting to "./gateway" silently clobbers any
// existing "gateway" package a caller happens to have lying around (this
// module's own gateway/ package included, if generate is ever run from a
// soapbridge checkout without an explicit --out).
func checkOutDirSafe(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh directory: nothing to clobber
		}
		return err
	}

	generatedBefore := false
	var foreignGoFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "config.yaml" || name == "service.wsdl" {
			generatedBefore = true
		}
		if strings.HasSuffix(name, ".go") && name != "main.go" {
			foreignGoFiles = append(foreignGoFiles, name)
		}
	}
	if generatedBefore || len(foreignGoFiles) == 0 {
		return nil // empty, or looks like a previous soapbridge-generated output: safe to overwrite
	}
	return fmt.Errorf(
		"%s already contains Go source that doesn't look like a previous soapbridge-generated output (%s) — refusing to overwrite it; pass a different --out",
		dir, strings.Join(foreignGoFiles, ", "))
}

// findSoapbridgeModuleDir walks up from the current working directory
// looking for the soapbridge module's own go.mod, covering the common
// development-time case of running `go run ./cmd/soapbridge generate ...`
// from within (or below) a soapbridge checkout.
func findSoapbridgeModuleDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		modPath := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(modPath); err == nil {
			if strings.HasPrefix(strings.TrimSpace(string(data)), "module "+genserver.SoapbridgeModulePath) {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find a soapbridge checkout by walking up from %s", dir)
		}
		dir = parent
	}
}
