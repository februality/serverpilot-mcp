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
		clientIDs   []string
		all         bool
		removeCreds bool
		removeKey   bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the serverpilot entry from MCP-client configs",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !all && len(clientIDs) == 0 && !removeCreds && !removeKey {
				return fmt.Errorf("must specify --all, --client, --remove-creds, or --remove-key")
			}

			var targets []clients.Patcher
			if all {
				targets = clients.All()
			} else {
				for _, id := range clientIDs {
					p := clients.ByID(id)
					if p == nil {
						return fmt.Errorf("unknown client %q", id)
					}
					targets = append(targets, p)
				}
			}
			for _, p := range targets {
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
				_ = store.Delete(creds.KeyClientID)
				_ = store.Delete(creds.KeyAPIKey)
				fmt.Println("  ✓ Removed stored credentials")
			}

			if removeKey {
				path, err := config.ExpandHome(config.DefaultSSHKeyPath)
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
	cmd.Flags().StringSliceVar(&clientIDs, "client", nil, "client ID to unpatch (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "unpatch every MCP client we know about")
	cmd.Flags().BoolVar(&removeCreds, "remove-creds", false, "delete stored ServerPilot credentials")
	cmd.Flags().BoolVar(&removeKey, "remove-key", false, "delete local SSH key files")
	return cmd
}
