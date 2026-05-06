package cli

import (
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/mcpserver"
)

func NewServe() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP stdio server (invoked by MCP clients)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServe()
		},
	}
}

func runServe() error {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNoCredentials) {
			return fmt.Errorf("no ServerPilot credentials found.\n\nRun:\n  serverpilot-mcp setup\n\nor set SERVERPILOT_CLIENT_ID and SERVERPILOT_API_KEY")
		}
		return fmt.Errorf("load config: %w", err)
	}
	deps := mcpserver.Build(cfg)
	srv := mcpserver.New(deps)
	return server.ServeStdio(srv)
}
