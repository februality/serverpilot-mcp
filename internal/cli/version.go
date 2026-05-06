package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/mcpserver"
)

// Set via -ldflags by GoReleaser. Falls back to a dev string.
var (
	Version = mcpserver.ServerVersion
	Commit  = "unknown"
	Date    = "unknown"
)

func NewVersion() *cobra.Command {
	short := false
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			if short {
				fmt.Println(Version)
				return
			}
			fmt.Printf("serverpilot-mcp %s (commit %s, built %s)\n", Version, Commit, Date)
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the semver")
	return cmd
}
