// Package wizard runs the interactive setup flow that the install scripts
// launch after dropping the binary in place. The wizard collects API
// credentials, ensures an SSH key, registers it with ServerPilot, assigns
// it to all sysusers, and patches every detected MCP-client config.
//
// All side-effecting steps are routed through the Deps struct so tests
// (and the --unattended path) can substitute fakes.
package wizard

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/februality/serverpilot-mcp/internal/clients"
	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/creds"
	"github.com/februality/serverpilot-mcp/internal/mcpserver"
	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

// Options configure a wizard run.
type Options struct {
	Out          io.Writer
	Prompter     Prompter
	Unattended   bool   // when true, skip prompts and use defaults
	BinaryPath   string // path to install into MCP-client configs
	SkipSSH      bool   // skip SSH key generation/registration/assignment
	SkipClients  bool   // skip patching any MCP-client configs
	OnlyClients  []string // limit which client patchers to use ("" = all)
	SSHKeyPath   string   // override config default
	SSHKeyName   string   // override config default
}

// Run executes the 11-step setup flow. Returns the first fatal error or nil.
func Run(opts Options) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Prompter == nil {
		opts.Prompter = NewPrompter()
	}
	if opts.BinaryPath == "" {
		bp, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}
		opts.BinaryPath = bp
	}
	if opts.SSHKeyPath == "" {
		p, err := config.ExpandHome(config.DefaultSSHKeyPath)
		if err != nil {
			return err
		}
		opts.SSHKeyPath = p
	}
	if opts.SSHKeyName == "" {
		opts.SSHKeyName = config.DefaultSSHKeyName
	}
	w := opts.Out

	// Step 1: banner
	printBanner(w)

	// Step 2-4: credentials + verify
	clientID, apiKey, err := collectCredentials(opts)
	if err != nil {
		return err
	}
	cache := spapi.NewTTLCache(60)
	apiClient := spapi.NewClient(clientID, apiKey)
	servers := spapi.NewServersAPI(apiClient, cache)
	apps := spapi.NewAppsAPI(apiClient, cache)
	sysusers := spapi.NewSysUsersAPI(apiClient, cache)
	sshkeys := spapi.NewSSHKeysAPI(apiClient)

	srvList, err := servers.List()
	if err != nil {
		return fmt.Errorf("verify credentials (GET /servers): %w", err)
	}
	appList, _ := apps.List()
	fmt.Fprintf(w, "  ✓ Verified — %d servers, %d apps\n\n", len(srvList), len(appList))

	// Step 5: store creds
	if err := storeCreds(w, clientID, apiKey); err != nil {
		return err
	}

	// Steps 6-8: SSH bootstrap
	if !opts.SkipSSH {
		if err := bootstrapSSH(w, opts, sshkeys, sysusers); err != nil {
			return err
		}
	}

	// Steps 9-10: detect + patch clients
	patched := []string{}
	if !opts.SkipClients {
		patched, err = patchClients(w, opts)
		if err != nil {
			return err
		}
	}

	// Step 11: summary
	printSummary(w, opts, patched)
	return nil
}

func collectCredentials(opts Options) (string, string, error) {
	if opts.Unattended {
		cid := os.Getenv("SERVERPILOT_CLIENT_ID")
		key := os.Getenv("SERVERPILOT_API_KEY")
		if cid == "" || key == "" {
			return "", "", fmt.Errorf("--unattended requires SERVERPILOT_CLIENT_ID and SERVERPILOT_API_KEY env vars")
		}
		return cid, key, nil
	}

	cid, err := opts.Prompter.Text(
		"ServerPilot Client ID",
		"cid_xxxxxxxxxxxx",
		nonEmpty("Client ID"),
	)
	if err != nil {
		return "", "", err
	}
	key, err := opts.Prompter.Password(
		"ServerPilot API Key",
		nonEmpty("API Key"),
	)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(cid), strings.TrimSpace(key), nil
}

func nonEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s cannot be empty", label)
		}
		return nil
	}
}

func storeCreds(w io.Writer, clientID, apiKey string) error {
	store, err := creds.Open()
	if err != nil {
		return fmt.Errorf("open credentials store: %w", err)
	}
	if err := store.Set(creds.KeyClientID, clientID); err != nil {
		return fmt.Errorf("store client ID: %w", err)
	}
	if err := store.Set(creds.KeyAPIKey, apiKey); err != nil {
		return fmt.Errorf("store API key: %w", err)
	}
	switch store.Backend() {
	case creds.SourceKeyring:
		fmt.Fprintln(w, "  ✓ Stored credentials in OS keychain")
	case creds.SourceFile:
		if fs, ok := store.(*creds.FileStore); ok {
			fmt.Fprintf(w, "  ✓ Stored credentials at %s (mode 0600)\n", fs.Path())
		} else {
			fmt.Fprintln(w, "  ✓ Stored credentials in file fallback")
		}
	}
	fmt.Fprintln(w)
	return nil
}

func bootstrapSSH(w io.Writer, opts Options, sshkeys *spapi.SSHKeysAPI, sysusers *spapi.SysUsersAPI) error {
	// Generate / load SSH key.
	pair, err := mcsh.EnsureKeyPair(opts.SSHKeyPath, opts.SSHKeyName)
	if err != nil {
		return fmt.Errorf("ensure SSH key: %w", err)
	}
	fmt.Fprintf(w, "  ✓ SSH key at %s\n", opts.SSHKeyPath)

	// Register with ServerPilot if missing.
	spKey, err := sshkeys.FindByName(opts.SSHKeyName)
	if err != nil {
		return fmt.Errorf("look up SSH key on ServerPilot: %w", err)
	}
	if spKey == nil {
		spKey, err = sshkeys.Create(opts.SSHKeyName, pair.PublicKey)
		if err != nil {
			return fmt.Errorf("register SSH key: %w", err)
		}
		fmt.Fprintf(w, "  ✓ Registered key %q with ServerPilot (%s)\n", opts.SSHKeyName, spKey.ID)
	} else {
		fmt.Fprintf(w, "  ✓ Key %q already registered with ServerPilot (%s)\n", opts.SSHKeyName, spKey.ID)
	}

	// Assign to all sysusers (skip already-assigned).
	users, err := sysusers.List()
	if err != nil {
		return fmt.Errorf("list sysusers: %w", err)
	}
	assign := true
	if !opts.Unattended {
		var promptErr error
		assign, promptErr = opts.Prompter.Confirm(
			fmt.Sprintf("Assign SSH key to all %d system users?", len(users)),
			true,
		)
		if promptErr != nil {
			return promptErr
		}
	}
	if !assign {
		fmt.Fprintln(w, "  · Skipping sysuser assignment (you can re-run with sp_ssh_setup)")
		return nil
	}
	var added, skipped, failed int
	total := len(users)
	for i, u := range users {
		// In-place progress line: \r returns to col 0, \033[K clears to EOL
		// so a shorter sysuser name doesn't leave residue from a longer one.
		fmt.Fprintf(w, "\r  Assigning SSH key… [%d/%d] %s\033[K", i+1, total, u.Name)

		userKeys, err := sshkeys.ListForSysUser(u.ID)
		if err != nil {
			failed++
			continue
		}
		has := false
		for _, k := range userKeys {
			if k.ID == spKey.ID {
				has = true
				break
			}
		}
		if has {
			skipped++
			continue
		}
		if err := sshkeys.AddToSysUser(spKey.ID, u.ID); err != nil {
			failed++
			continue
		}
		added++
	}
	// Clear the progress line, then write the summary on its own line.
	fmt.Fprint(w, "\r\033[K")
	fmt.Fprintf(w, "  ✓ SSH key assignments: %d added, %d already had it, %d failed\n\n",
		added, skipped, failed)
	return nil
}

func patchClients(w io.Writer, opts Options) ([]string, error) {
	all := clients.All()

	// Filter to OnlyClients if specified.
	if len(opts.OnlyClients) > 0 {
		want := map[string]bool{}
		for _, id := range opts.OnlyClients {
			want[id] = true
		}
		filtered := all[:0]
		for _, p := range all {
			if want[p.ID()] {
				filtered = append(filtered, p)
			}
		}
		all = filtered
	}

	// Detect each.
	type detectInfo struct {
		patcher   clients.Patcher
		installed bool
		path      string
	}
	infos := make([]detectInfo, 0, len(all))
	for _, p := range all {
		installed, path, _ := p.Detect()
		infos = append(infos, detectInfo{p, installed, path})
	}

	// Build options + defaults.
	prompts := make([]SelectOption, 0, len(infos))
	defaults := []string{}
	for _, i := range infos {
		label := i.patcher.DisplayName()
		if !i.installed {
			label += " (not detected)"
		}
		prompts = append(prompts, SelectOption{Label: label, Value: i.patcher.ID()})
		if i.installed {
			defaults = append(defaults, i.patcher.ID())
		}
	}

	var chosen []string
	var err error
	if opts.Unattended {
		chosen = defaults
	} else {
		chosen, err = opts.Prompter.MultiSelect(
			"Configure which MCP clients?",
			prompts,
			defaults,
		)
		if err != nil {
			return nil, err
		}
	}

	want := map[string]bool{}
	for _, id := range chosen {
		want[id] = true
	}
	patched := []string{}
	for _, i := range infos {
		if !want[i.patcher.ID()] {
			continue
		}
		changed, _, err := i.patcher.Patch(opts.BinaryPath, false)
		if err != nil {
			fmt.Fprintf(w, "  ✗ %s: %s\n", i.patcher.DisplayName(), err)
			continue
		}
		if changed {
			fmt.Fprintf(w, "  ✓ %s — patched %s\n", i.patcher.DisplayName(), i.path)
		} else {
			fmt.Fprintf(w, "  · %s — already configured\n", i.patcher.DisplayName())
		}
		patched = append(patched, i.patcher.ID())
	}
	fmt.Fprintln(w)
	return patched, nil
}

func printSummary(w io.Writer, opts Options, patched []string) {
	fmt.Fprintln(w, "  Setup complete.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Binary:     %s\n", opts.BinaryPath)
	fmt.Fprintf(w, "  SSH key:    %s\n", opts.SSHKeyPath)
	fmt.Fprintf(w, "  MCP server: %s v%s\n", mcpserver.ServerName, mcpserver.ServerVersion)
	if len(patched) > 0 {
		fmt.Fprintf(w, "  Configured: %s\n", strings.Join(patched, ", "))
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Restart your MCP client to load the server.")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Verify with:  serverpilot-mcp doctor")
	fmt.Fprintln(w)
}
