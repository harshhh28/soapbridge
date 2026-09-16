package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/harshhh28/soapbridge/internal/genserver"
)

func newServeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a previously generated gateway",
		Example: "  soapbridge serve --config ./gateway/config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("reading config: %w", err)
			}
			var cfg genserver.Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return fmt.Errorf("parsing config: %w", err)
			}

			dir := filepath.Dir(configPath)
			if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
				return fmt.Errorf("%s doesn't look like a soapbridge-generated directory (no main.go next to config.yaml)", dir)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Serving %s from %s on %s (mcp=%v openapi=%v)\n", cfg.Name, dir, cfg.Addr, cfg.MCP, cfg.OpenAPI)

			runCmd := exec.Command("go", "run", ".")
			runCmd.Dir = dir
			runCmd.Env = append(os.Environ(), "ADDR="+cfg.Addr)
			runCmd.Stdout = cmd.OutOrStdout()
			runCmd.Stderr = cmd.ErrOrStderr()
			runCmd.Stdin = cmd.InOrStdin()
			return runCmd.Run()
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to a generated gateway's config.yaml (required)")
	return cmd
}
