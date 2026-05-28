package cli

import (
	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/wizard"
)

func NewSetup() *cobra.Command {
	var (
		account     string
		unattended  bool
		skipSSH     bool
		skipClients bool
		onlyClients []string
		readOnly    bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Run the interactive setup wizard",
		Long: "Walks through credential entry, SSH key bootstrap, and MCP-client " +
			"configuration. Safe to re-run — every step is idempotent.\n\n" +
			"Use --account to set up a named account alongside the default. Each " +
			"named account gets its own client-config entry (serverpilot-<name>), " +
			"keychain credentials, and SSH key pair.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateAccount(account); err != nil {
				return err
			}
			return wizard.Run(wizard.Options{
				Account:     account,
				Unattended:  unattended,
				SkipSSH:     skipSSH,
				SkipClients: skipClients,
				OnlyClients: onlyClients,
				ReadOnly:    readOnly,
			})
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "named ServerPilot account (lowercase slug); omit for the unnamed default")
	cmd.Flags().BoolVar(&unattended, "unattended", false, "skip prompts (requires SERVERPILOT_CLIENT_ID/_API_KEY env vars)")
	cmd.Flags().BoolVar(&skipSSH, "skip-ssh", false, "skip SSH key generation/registration")
	cmd.Flags().BoolVar(&skipClients, "skip-clients", false, "skip MCP-client config patching")
	cmd.Flags().StringSliceVar(&onlyClients, "client", nil, "limit to specific client IDs (repeatable)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "bake SP_READ_ONLY=1 into client configs (hides write tools). In interactive mode, also pre-selects the read-only prompt.")
	return cmd
}
