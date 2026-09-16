package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/harshhh28/soapbridge/mockserver"
	"github.com/harshhh28/soapbridge/wsdl"
)

func newMockCmd() *cobra.Command {
	var (
		wsdlSource string
		addr       string
		path       string
	)

	cmd := &cobra.Command{
		Use:   "mock",
		Short: "Run a fake SOAP server for a WSDL, answering every operation with synthetic example data",
		Example: "  soapbridge mock --wsdl testdata/wsdl/calculator.wsdl --addr :9000\n" +
			"  soapbridge generate --wsdl testdata/wsdl/calculator.wsdl --out /tmp/calc-gw --endpoint http://localhost:9000/soap --run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if wsdlSource == "" {
				return fmt.Errorf("--wsdl is required")
			}

			res, err := wsdl.Load(wsdlSource)
			if err != nil {
				return fmt.Errorf("loading WSDL: %w", err)
			}
			for _, w := range res.Warnings {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  WARNING: %s\n", w)
			}
			def := res.Definition

			endpoint := "http://localhost" + addr + path
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Mocking %q: %d operations, answering with synthetic example data\n", def.Name, len(def.Operations))
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Listening on %s\n\n", endpoint)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Point a generated gateway at it with:\n  soapbridge generate --wsdl %s --out <dir> --endpoint %s --run\n\n", wsdlSource, endpoint)

			mux := http.NewServeMux()
			mux.Handle(path, mockserver.Handler(def))
			return http.ListenAndServe(addr, mux)
		},
	}

	cmd.Flags().StringVar(&wsdlSource, "wsdl", "", "path or URL to the WSDL document (required)")
	cmd.Flags().StringVar(&addr, "addr", ":9000", "listen address for the fake SOAP server")
	cmd.Flags().StringVar(&path, "path", "/soap", "HTTP path the fake SOAP server answers on")

	return cmd
}
