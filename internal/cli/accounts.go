package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/clients"
	"github.com/februality/serverpilot-mcp/internal/creds"
)

// NewAccounts returns the `accounts` parent command (list, remove).
func NewAccounts() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "List or remove configured ServerPilot accounts",
		Long: "Inspect or manage the named ServerPilot accounts registered with " +
			"setup --account. The unnamed/legacy account is shown as \"default\".",
	}
	cmd.AddCommand(newAccountsList())
	cmd.AddCommand(newAccountsRemove())
	return cmd
}

type accountRow struct {
	Name           string   `json:"name"`
	HasCredentials bool     `json:"hasCredentials"`
	ClientEntries  []string `json:"clientEntries"`
}

func newAccountsList() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured accounts",
		RunE: func(_ *cobra.Command, _ []string) error {
			rows := listAccounts()
			if jsonOut {
				b, err := json.MarshalIndent(rows, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}
			if len(rows) == 0 {
				fmt.Println("No accounts configured. Run: serverpilot-mcp setup")
				return nil
			}
			fmt.Printf("%-20s  %-12s  %s\n", "ACCOUNT", "CREDENTIALS", "CLIENT ENTRIES")
			for _, r := range rows {
				cred := "✗"
				if r.HasCredentials {
					cred = "✓"
				}
				entries := "(none)"
				if len(r.ClientEntries) > 0 {
					entries = joinSorted(r.ClientEntries)
				}
				fmt.Printf("%-20s  %-12s  %s\n", accountLabel(r.Name), cred, entries)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	return cmd
}

func newAccountsRemove() *cobra.Command {
	var (
		account string
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a named account (credentials + every client entry)",
		Long: "Deletes the named account's stored credentials and unpatches its " +
			"serverpilot-<name> entry from every MCP client. Refuses to operate " +
			"on the unnamed/legacy account — use `uninstall --remove-creds` for that.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if account == "" {
				return fmt.Errorf("--account is required")
			}
			if err := validateAccount(account); err != nil {
				return err
			}
			if !yes {
				return fmt.Errorf("destructive: pass --yes to confirm removing account %q", account)
			}
			store, err := creds.Open()
			if err != nil {
				return err
			}
			_ = store.DeleteFor(account, creds.KeyClientID)
			_ = store.DeleteFor(account, creds.KeyAPIKey)
			fmt.Printf("  ✓ Removed credentials for %s\n", account)
			for _, p := range clients.All(account) {
				changed, err := p.Unpatch()
				if err != nil {
					fmt.Printf("  ✗ %s: %s\n", p.DisplayName(), err)
					continue
				}
				if changed {
					fmt.Printf("  ✓ %s — removed entry\n", p.DisplayName())
				}
			}
			fmt.Println("\nNote: the SSH key files on disk and the public key registered with")
			fmt.Println("ServerPilot are NOT removed automatically.")
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "named account to remove (required; cannot be empty)")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the destructive operation")
	return cmd
}

// listAccounts returns one row per discovered account, unioning the
// credentials store with every client config's entries.
func listAccounts() []accountRow {
	rows := map[string]*accountRow{}
	credAccts, _ := creds.ListAccounts()
	for _, a := range credAccts {
		rows[a] = &accountRow{Name: a, HasCredentials: true}
	}
	for _, p := range clients.All("") {
		entries, err := p.Entries()
		if err != nil {
			continue
		}
		for _, e := range entries {
			row, ok := rows[e]
			if !ok {
				row = &accountRow{Name: e}
				rows[e] = row
			}
			row.ClientEntries = append(row.ClientEntries, p.ID())
		}
	}
	out := make([]accountRow, 0, len(rows))
	for _, r := range rows {
		sort.Strings(r.ClientEntries)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func joinSorted(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
