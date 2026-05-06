package cli

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/logging"
)

var logLevel string

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "serverpilot-mcp",
		Short: "ServerPilot MCP server + setup wizard",
		Long: "ServerPilot MCP — manage ServerPilot-hosted sites from any MCP client.\n\n" +
			"With no arguments, runs the MCP stdio server (the entry point invoked by\n" +
			"MCP clients). When launched interactively in a terminal with no arguments,\n" +
			"prints help instead so the wizard isn't started by accident.",
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			logging.Init(logLevel)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// No subcommand. Decide based on TTY.
			stdinIsTTY := isatty.IsTerminal(os.Stdin.Fd())
			stdoutIsTTY := isatty.IsTerminal(os.Stdout.Fd())
			if stdinIsTTY && stdoutIsTTY {
				return cmd.Help()
			}
			return runServe()
		},
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level: debug|info|warn|error")
	root.AddCommand(NewServe())
	root.AddCommand(NewVersion())
	root.AddCommand(NewSetup())
	root.AddCommand(NewInstall())
	root.AddCommand(NewUninstall())
	root.AddCommand(NewStatus())
	root.AddCommand(NewDoctor())
	return root
}

func Execute() error {
	return NewRoot().Execute()
}
