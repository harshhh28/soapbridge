// Command soapbridge generates a REST/MCP/OpenAPI gateway for a SOAP web
// service from its WSDL, and runs previously generated gateways.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// cliVersion is this binary's own release version, e.g. "v0.1.0" — set at
// build time via "-ldflags -X main.cliVersion=vX.Y.Z" (GoReleaser does
// this automatically for release builds). The default, "dev", means this
// binary was built from source outside a tagged release: "generate" can
// then only depend on soapbridge via a local checkout (--soapbridge-dir),
// not a published version — see generate.go.
var cliVersion = "dev"

func main() {
	root := &cobra.Command{
		Use:     "soapbridge",
		Short:   "Generate a REST/MCP/OpenAPI gateway from a WSDL SOAP service",
		Version: cliVersion,
	}
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newMockCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
