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
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/februality/serverpilot-mcp/internal/clients"
	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/creds"
	"github.com/februality/serverpilot-mcp/internal/mcpserver"
	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

// Options configure a wizard run.
type Options struct {
	Out         io.Writer
	Prompter    Prompter
	Account     string   // "" = unnamed/legacy account; otherwise a named account slug
	Unattended  bool     // when true, skip prompts and use defaults
	BinaryPath  string   // path to install into MCP-client configs
	SkipSSH     bool     // skip SSH key generation/registration/assignment
	SkipClients bool     // skip patching any MCP-client configs
	OnlyClients []string // limit which client patchers to use ("" = all)
	SSHKeyPath  string   // override config default
	SSHKeyName  string   // override config default
	// ReadOnly bakes SP_READ_ONLY=1 into the patched MCP-client config so
	// the MCP server starts up with the six write tools hidden. When false
	// (the default) no env block is written, preserving byte-identical
	// output for existing installs.
	ReadOnly bool
}

// Run executes the setup flow. Returns the first fatal error or nil.
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
	defKeyPath, defKeyName, _ := config.AccountDefaults(opts.Account)
	if opts.SSHKeyPath == "" {
		p, err := config.ExpandHome(defKeyPath)
		if err != nil {
			return err
		}
		opts.SSHKeyPath = p
	}
	if opts.SSHKeyName == "" {
		opts.SSHKeyName = defKeyName
	}
	w := opts.Out

	// Step 1: banner
	printBanner(w)
	if opts.Account != "" {
		fmt.Fprintf(w, "  Setting up account %q — credentials, SSH key, and client entry suffixed -%s.\n\n", opts.Account, opts.Account)
	}

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
	if err := storeCreds(w, opts.Account, clientID, apiKey); err != nil {
		return err
	}

	// Steps 6-8: SSH bootstrap
	if !opts.SkipSSH {
		if err := bootstrapSSH(w, opts, sshkeys, sysusers); err != nil {
			return err
		}
	}

	// Step 9: read-only mode opt-in (the env var is baked into the client
	// configs in step 10, so it has to be decided before patching).
	if !opts.SkipClients && !opts.Unattended {
		readOnly, promptErr := opts.Prompter.Confirm(
			"Run the MCP server in read-only mode? Hides every write tool "+
				"(site_exec, site_write_file, sp_update_*, sp_ssh_setup/remove) "+
				"so the AI tool can read but cannot change anything.",
			opts.ReadOnly,
		)
		if promptErr != nil {
			return promptErr
		}
		opts.ReadOnly = readOnly
	}
	if opts.ReadOnly {
		fmt.Fprintln(w, "  ✓ Read-only mode: ON")
		fmt.Fprintln(w)
	}

	// Step 10: detect + patch clients
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

func storeCreds(w io.Writer, account, clientID, apiKey string) error {
	store, err := creds.Open()
	if err != nil {
		return fmt.Errorf("open credentials store: %w", err)
	}
	if err := store.SetFor(account, creds.KeyClientID, clientID); err != nil {
		return fmt.Errorf("store client ID: %w", err)
	}
	if err := store.SetFor(account, creds.KeyAPIKey, apiKey); err != nil {
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

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// keyProgress renders a 3-line spinner+bar block in-place while sysuser
// SSH-key assignments run serially. The spinner is animated by a goroutine
// so a slow API call to a single user still shows liveness.
type keyProgress struct {
	w     io.Writer
	bar   progress.Model
	total int

	mu    sync.Mutex
	cur   int
	frame int
	drawn bool

	stop chan struct{}
	done chan struct{}
}

func newKeyProgress(w io.Writer, total int) *keyProgress {
	return &keyProgress{
		w:     w,
		bar:   progress.New(progress.WithDefaultGradient()),
		total: total,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (p *keyProgress) start() {
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		defer close(p.done)
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				p.mu.Lock()
				p.frame = (p.frame + 1) % len(spinnerFrames)
				p.draw()
				p.mu.Unlock()
			}
		}
	}()
}

func (p *keyProgress) advance() {
	p.mu.Lock()
	p.cur++
	p.draw()
	p.mu.Unlock()
}

func (p *keyProgress) draw() {
	if p.drawn {
		// \033[2F: cursor up 2 lines, column 0. \033[J: clear to end of screen.
		fmt.Fprint(p.w, "\033[2F\033[J")
	}
	pct := 0.0
	if p.total > 0 {
		pct = float64(p.cur) / float64(p.total)
	}
	fmt.Fprintf(p.w, "%s Registering SSH keys...\n\n  %s\n",
		spinnerFrames[p.frame], p.bar.ViewAs(pct))
	p.drawn = true
}

func (p *keyProgress) finish(msg string) {
	close(p.stop)
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.drawn {
		fmt.Fprint(p.w, "\033[2F\033[J")
	}
	fmt.Fprintln(p.w, msg)
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
	prog := newKeyProgress(w, len(users))
	prog.start()
	anyFailed := false
	for _, u := range users {
		userKeys, err := sshkeys.ListForSysUser(u.ID)
		if err != nil {
			anyFailed = true
			prog.advance()
			continue
		}
		has := false
		for _, k := range userKeys {
			if k.ID == spKey.ID {
				has = true
				break
			}
		}
		if !has {
			if err := sshkeys.AddToSysUser(spKey.ID, u.ID); err != nil {
				anyFailed = true
			}
		}
		prog.advance()
	}
	if anyFailed {
		prog.finish("  ⚠ SSH keys registered (some failed)")
	} else {
		prog.finish("  ✓ SSH keys registered")
	}
	return nil
}

// patchEnv builds the env block written into each client's MCP server
// entry. Returns nil (omitted on the wire) when neither flag is set so
// existing single-account installs produce byte-identical output.
func patchEnv(opts Options) map[string]string {
	env := map[string]string{}
	if opts.ReadOnly {
		env["SP_READ_ONLY"] = "1"
	}
	if opts.Account != "" {
		env["SP_ACCOUNT"] = opts.Account
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func patchClients(w io.Writer, opts Options) ([]string, error) {
	all := clients.All(opts.Account)

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
		changed, _, err := i.patcher.Patch(opts.BinaryPath, patchEnv(opts), false)
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
	if opts.Account != "" {
		fmt.Fprintf(w, "  Account:    %s\n", opts.Account)
		fmt.Fprintf(w, "  Entry:      %s\n", clients.EntryName(opts.Account))
	}
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
