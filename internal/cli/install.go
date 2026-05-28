package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/clients"
)

func NewInstall() *cobra.Command {
	var (
		account    string
		clientIDs  []string
		all        bool
		binaryPath string
		dryRun     bool
		readOnly   bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Patch MCP-client configs to register the serverpilot server",
		Long: "Non-interactive client config patching. Use --all to patch every detected " +
			"client, or --client=ID one or more times. Use --account <name> to patch " +
			"under a named entry (serverpilot-<name>) instead of the unnamed default.\n\n" +
			"Available client IDs: " + strings.Join(clients.IDs(), ", "),
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateAccount(account); err != nil {
				return err
			}
			if !all && len(clientIDs) == 0 {
				return fmt.Errorf("must specify --all or one or more --client values")
			}
			if binaryPath == "" {
				bp, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve binary path: %w", err)
				}
				binaryPath = bp
			}

			var targets []clients.Patcher
			if all {
				for _, p := range clients.All(account) {
					installed, _, _ := p.Detect()
					if installed {
						targets = append(targets, p)
					}
				}
			} else {
				for _, id := range clientIDs {
					p := clients.ByID(id, account)
					if p == nil {
						return fmt.Errorf("unknown client %q (valid: %s)", id, strings.Join(clients.IDs(), ", "))
					}
					targets = append(targets, p)
				}
			}

			if len(targets) == 0 {
				fmt.Println("No MCP clients detected to install into.")
				return nil
			}

			env := map[string]string{}
			if readOnly {
				env["SP_READ_ONLY"] = "1"
			}
			if account != "" {
				env["SP_ACCOUNT"] = account
			}
			if len(env) == 0 {
				env = nil
			}
			for _, p := range targets {
				changed, diff, err := p.Patch(binaryPath, env, dryRun)
				if err != nil {
					fmt.Printf("  ✗ %s: %s\n", p.DisplayName(), err)
					continue
				}
				_, path, _ := p.Detect()
				switch {
				case dryRun && changed:
					fmt.Printf("  ~ %s — would patch %s\n%s", p.DisplayName(), path, diff)
				case dryRun:
					fmt.Printf("  · %s — no changes needed\n", p.DisplayName())
				case changed:
					fmt.Printf("  ✓ %s — patched %s\n", p.DisplayName(), path)
				default:
					fmt.Printf("  · %s — already configured\n", p.DisplayName())
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "named ServerPilot account (lowercase slug); omit for the unnamed default")
	cmd.Flags().StringSliceVar(&clientIDs, "client", nil, "client ID to patch (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "patch every detected MCP client")
	cmd.Flags().StringVar(&binaryPath, "binary-path", "", "absolute path to write into client configs (default: this binary)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print diffs without writing")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "bake SP_READ_ONLY=1 into client configs (hides write tools)")
	return cmd
}
