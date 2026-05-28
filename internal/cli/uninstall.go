package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/clients"
	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/creds"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

func NewUninstall() *cobra.Command {
	var (
		account     string
		allAccounts bool
		clientIDs   []string
		all         bool
		removeCreds bool
		removeKey   bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the serverpilot entry from MCP-client configs",
		Long: "Removes the serverpilot entry. Without --account, only the unnamed " +
			"default entry is touched — named accounts (serverpilot-<name>) survive. " +
			"Use --account <name> to target one named account, or --all-accounts to " +
			"sweep every serverpilot* entry from each targeted client.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateAccount(account); err != nil {
				return err
			}
			if allAccounts && account != "" {
				return fmt.Errorf("--account and --all-accounts are mutually exclusive")
			}
			if !all && len(clientIDs) == 0 && !removeCreds && !removeKey {
				return fmt.Errorf("must specify --all, --client, --remove-creds, or --remove-key")
			}

			// Resolve the set of patcher *instances* (one per client) we want
			// to operate on. When --all-accounts is set we expand each instance
			// into per-discovered-account instances below.
			var targets []clients.Patcher
			if all {
				targets = clients.All(account)
			} else {
				for _, id := range clientIDs {
					p := clients.ByID(id, account)
					if p == nil {
						return fmt.Errorf("unknown client %q", id)
					}
					targets = append(targets, p)
				}
			}

			for _, p := range targets {
				if allAccounts {
					accts, err := p.Entries()
					if err != nil {
						fmt.Printf("  ✗ %s: %s\n", p.DisplayName(), err)
						continue
					}
					if len(accts) == 0 {
						fmt.Printf("  · %s — no entries found\n", p.DisplayName())
						continue
					}
					for _, acct := range accts {
						unp := clients.ByID(p.ID(), acct)
						changed, err := unp.Unpatch()
						if err != nil {
							fmt.Printf("  ✗ %s [%s]: %s\n", p.DisplayName(), accountLabel(acct), err)
							continue
						}
						if changed {
							fmt.Printf("  ✓ %s [%s] — removed entry\n", p.DisplayName(), accountLabel(acct))
						}
					}
					continue
				}
				changed, err := p.Unpatch()
				if err != nil {
					fmt.Printf("  ✗ %s: %s\n", p.DisplayName(), err)
					continue
				}
				if changed {
					fmt.Printf("  ✓ %s — removed entry\n", p.DisplayName())
				} else {
					fmt.Printf("  · %s — no entry found\n", p.DisplayName())
				}
			}

			if removeCreds {
				store, err := creds.Open()
				if err != nil {
					return err
				}
				_ = store.DeleteFor(account, creds.KeyClientID)
				_ = store.DeleteFor(account, creds.KeyAPIKey)
				fmt.Printf("  ✓ Removed stored credentials for %s\n", accountLabel(account))
			}

			if removeKey {
				keyPath, _, _ := config.AccountDefaults(account)
				path, err := config.ExpandHome(keyPath)
				if err != nil {
					return err
				}
				if mcsh.KeyPairExists(path) {
					_ = os.Remove(path)
					_ = os.Remove(path + ".pub")
					fmt.Printf("  ✓ Removed SSH key files at %s{,.pub}\n", path)
				} else {
					fmt.Printf("  · No SSH key files at %s\n", path)
				}
				fmt.Println("  Note: SSH key remains registered with ServerPilot. To remove it server-side,")
				fmt.Println("        use the sp_ssh_remove tool with delete_key=true.")
			}
			fmt.Println()
			fmt.Println(strings.TrimSpace("Uninstall complete."))
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "named ServerPilot account to unpatch; omit for the unnamed default")
	cmd.Flags().BoolVar(&allAccounts, "all-accounts", false, "for each targeted client, unpatch every discovered serverpilot* entry")
	cmd.Flags().StringSliceVar(&clientIDs, "client", nil, "client ID to unpatch (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "unpatch every MCP client we know about")
	cmd.Flags().BoolVar(&removeCreds, "remove-creds", false, "delete stored ServerPilot credentials (scoped to --account)")
	cmd.Flags().BoolVar(&removeKey, "remove-key", false, "delete local SSH key files (scoped to --account)")
	return cmd
}

// accountLabel renders an account name for human output: "default" for the
// unnamed legacy account, the name itself otherwise.
func accountLabel(account string) string {
	if account == "" {
		return "default"
	}
	return account
}
