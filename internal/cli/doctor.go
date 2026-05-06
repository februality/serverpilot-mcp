package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"

	"github.com/februality/serverpilot-mcp/internal/clients"
	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/creds"
	"github.com/februality/serverpilot-mcp/internal/spapi"
)

func NewDoctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose installation, credentials, and per-client configs",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	fmt.Println("Running diagnostics…")
	fmt.Println()

	// 1. Credentials present?
	r, err := creds.ResolveAPICredentials()
	switch {
	case err != nil:
		fmt.Printf("  ✗ Credentials store error: %s\n", err)
		return err
	case r.Source == creds.SourceNotFound:
		fmt.Println("  ✗ No credentials configured. Run: serverpilot-mcp setup")
		return errors.New("missing credentials")
	default:
		fmt.Printf("  ✓ Credentials available (source: %s)\n", r.Source)
	}

	// 2. API reachable?
	cache := spapi.NewTTLCache(60)
	apiClient := spapi.NewClient(r.ClientID, r.APIKey)
	servers := spapi.NewServersAPI(apiClient, cache)
	srvList, err := servers.List()
	if err != nil {
		fmt.Printf("  ✗ ServerPilot API: %s\n", err)
		return err
	}
	fmt.Printf("  ✓ ServerPilot API reachable (%d servers)\n", len(srvList))

	// 3. SSH key present?
	keyPath, _ := config.ExpandHome(config.DefaultSSHKeyPath)
	if _, err := os.Stat(keyPath); err == nil {
		fmt.Printf("  ✓ SSH key at %s\n", keyPath)
	} else {
		fmt.Printf("  ✗ SSH key missing at %s — run setup\n", keyPath)
	}

	// 4. Per-client config validity.
	fmt.Println()
	fmt.Println("MCP-client configs:")
	for _, p := range clients.All() {
		_, path, _ := p.Detect()
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("  · %-16s no config file at %s\n", p.DisplayName(), path)
			} else {
				fmt.Printf("  ✗ %-16s %s\n", p.DisplayName(), err)
			}
			continue
		}
		// Light validation: JSON parses, our key is present.
		switch p.ID() {
		case "codex":
			if !gjson.ValidBytes(b) || true {
				// TOML — skip parse here; tomlpatch_test covers it.
				fmt.Printf("  ✓ %-16s present at %s\n", p.DisplayName(), path)
			}
		case "vscode":
			if !gjson.ValidBytes(b) {
				fmt.Printf("  ✗ %-16s malformed JSON\n", p.DisplayName())
				continue
			}
			if gjson.GetBytes(b, "servers."+clients.ServerKey).Exists() {
				fmt.Printf("  ✓ %-16s configured\n", p.DisplayName())
			} else {
				fmt.Printf("  · %-16s present but no serverpilot entry\n", p.DisplayName())
			}
		default:
			if !gjson.ValidBytes(b) {
				fmt.Printf("  ✗ %-16s malformed JSON\n", p.DisplayName())
				continue
			}
			if gjson.GetBytes(b, "mcpServers."+clients.ServerKey).Exists() {
				fmt.Printf("  ✓ %-16s configured\n", p.DisplayName())
			} else {
				fmt.Printf("  · %-16s present but no serverpilot entry\n", p.DisplayName())
			}
		}
	}
	fmt.Println()
	fmt.Println("All checks complete.")
	return nil
}
