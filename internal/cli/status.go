package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/februality/serverpilot-mcp/internal/clients"
	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/creds"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

const readOnlyEnvVar = "SP_READ_ONLY"

type statusOutput struct {
	Version     string         `json:"version"`
	Credentials credStatus     `json:"credentials"`
	SSHKey      sshKeyStatus   `json:"sshKey"`
	Clients     []clientStatus `json:"clients"`
	BinaryPath  string         `json:"binaryPath"`
	// ReadOnlyEnv reflects SP_READ_ONLY in the current shell — only useful
	// if the user happens to have it set when running `status`. The per-
	// client `readOnly` field below is the authoritative signal.
	ReadOnlyEnv bool `json:"readOnlyEnv"`
}

type credStatus struct {
	Configured bool         `json:"configured"`
	Source     creds.Source `json:"source"`
}

type sshKeyStatus struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Name   string `json:"name"`
}

type clientStatus struct {
	ID         string `json:"id"`
	Name       string `json:"displayName"`
	ConfigPath string `json:"configPath"`
	Detected   bool   `json:"detected"`
	// ReadOnly is true when the client's config has SP_READ_ONLY=1 in the
	// serverpilot entry's env block — i.e. the MCP server will start up
	// with the six write tools hidden.
	ReadOnly bool `json:"readOnly"`
}

func NewStatus() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what's configured and where",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := buildStatus()
			if jsonOut {
				b, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}
			printStatusHuman(out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	return cmd
}

func buildStatus() statusOutput {
	exe, _ := os.Executable()
	out := statusOutput{
		Version:     Version,
		BinaryPath:  exe,
		ReadOnlyEnv: config.IsTrueEnv(os.Getenv(readOnlyEnvVar)),
	}
	if r, err := creds.ResolveAPICredentials(); err == nil {
		out.Credentials = credStatus{
			Configured: r.Source != creds.SourceNotFound,
			Source:     r.Source,
		}
	}
	keyPath, _ := config.ExpandHome(config.DefaultSSHKeyPath)
	out.SSHKey = sshKeyStatus{
		Path:   keyPath,
		Name:   config.DefaultSSHKeyName,
		Exists: mcsh.KeyPairExists(keyPath),
	}
	for _, p := range clients.All() {
		detected, path, _ := p.Detect()
		cs := clientStatus{
			ID: p.ID(), Name: p.DisplayName(), ConfigPath: path, Detected: detected,
		}
		// Inspect env even when not "detected" — the wizard creates the
		// config on patch, so a client may have a serverpilot entry without
		// its install directory existing.
		if env, err := p.CurrentEnv(); err == nil {
			cs.ReadOnly = config.IsTrueEnv(env[readOnlyEnvVar])
		}
		out.Clients = append(out.Clients, cs)
	}
	return out
}

func printStatusHuman(s statusOutput) {
	fmt.Printf("serverpilot-mcp %s\n", s.Version)
	fmt.Printf("Binary:      %s\n\n", s.BinaryPath)

	fmt.Println("Credentials:")
	if s.Credentials.Configured {
		fmt.Printf("  ✓ Configured (source: %s)\n", s.Credentials.Source)
	} else {
		fmt.Println("  ✗ Not configured. Run: serverpilot-mcp setup")
	}
	fmt.Println()

	fmt.Println("SSH key:")
	if s.SSHKey.Exists {
		fmt.Printf("  ✓ %s\n", s.SSHKey.Path)
	} else {
		fmt.Printf("  ✗ Missing at %s\n", s.SSHKey.Path)
	}
	fmt.Println()

	fmt.Println("MCP clients:")
	for _, c := range s.Clients {
		mark := "·"
		if c.Detected {
			mark = "✓"
		}
		suffix := ""
		if c.ReadOnly {
			suffix = "  (read-only)"
		}
		fmt.Printf("  %s %-16s %s%s\n", mark, c.Name, c.ConfigPath, suffix)
	}
}
