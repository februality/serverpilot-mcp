package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

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

	accts := discoveredAccounts()
	if len(accts) == 0 {
		fmt.Println("  ✗ No accounts configured. Run: serverpilot-mcp setup")
		return errors.New("missing credentials")
	}

	var firstErr error
	for _, account := range accts {
		fmt.Printf("Account: %s\n", accountLabel(account))
		if err := doctorAccount(account); err != nil && firstErr == nil {
			firstErr = err
		}
		fmt.Println()
	}
	if firstErr == nil {
		fmt.Println("All checks complete.")
	}
	return firstErr
}

func doctorAccount(account string) error {
	r, err := creds.ResolveAPICredentialsFor(account)
	switch {
	case err != nil:
		fmt.Printf("  ✗ Credentials store error: %s\n", err)
		return err
	case r.Source == creds.SourceNotFound:
		cmd := "serverpilot-mcp setup"
		if account != "" {
			cmd = fmt.Sprintf("serverpilot-mcp setup --account %s", account)
		}
		fmt.Printf("  ✗ No credentials configured. Run: %s\n", cmd)
		return fmt.Errorf("missing credentials for %s", accountLabel(account))
	default:
		fmt.Printf("  ✓ Credentials available (source: %s)\n", r.Source)
	}

	cache := spapi.NewTTLCache(60)
	apiClient := spapi.NewClient(r.ClientID, r.APIKey)
	servers := spapi.NewServersAPI(apiClient, cache)
	srvList, err := servers.List()
	if err != nil {
		fmt.Printf("  ✗ ServerPilot API: %s\n", err)
		return err
	}
	fmt.Printf("  ✓ ServerPilot API reachable (%d servers)\n", len(srvList))

	keyPath, _, _ := config.AccountDefaults(account)
	expanded, _ := config.ExpandHome(keyPath)
	if _, err := os.Stat(expanded); err == nil {
		fmt.Printf("  ✓ SSH key at %s\n", expanded)
	} else {
		fmt.Printf("  ✗ SSH key missing at %s — run setup\n", expanded)
	}

	fmt.Println("  MCP-client configs:")
	for _, p := range clients.All(account) {
		_, path, _ := p.Detect()
		entries, err := p.Entries()
		if err != nil {
			fmt.Printf("    ✗ %-16s %s\n", p.DisplayName(), err)
			continue
		}
		hasEntry := false
		for _, e := range entries {
			if e == account {
				hasEntry = true
				break
			}
		}
		if !hasEntry {
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				fmt.Printf("    · %-16s no config file at %s\n", p.DisplayName(), path)
			} else {
				fmt.Printf("    · %-16s no entry for this account\n", p.DisplayName())
			}
			continue
		}
		roSuffix := ""
		if env, envErr := p.CurrentEnv(); envErr == nil && config.IsTrueEnv(env[readOnlyEnvVar]) {
			roSuffix = "  [read-only]"
		}
		fmt.Printf("    ✓ %-16s configured at %s%s\n", p.DisplayName(), path, roSuffix)
	}
	return nil
}
