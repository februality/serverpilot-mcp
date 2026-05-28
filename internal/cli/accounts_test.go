package cli

import (
	"os"
	"runtime"
	"testing"

	"github.com/februality/serverpilot-mcp/internal/clients"
)

// withTempHome redirects HOME so patcher constructors compute paths under
// the temp dir. Mirror of clients/patcher_test.go's helper.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
		t.Setenv("APPDATA", dir+"\\AppData\\Roaming")
	}
	t.Setenv("XDG_CONFIG_HOME", dir+"/.config")
	return dir
}

func TestListAccounts_DiscoversFromClientConfigs(t *testing.T) {
	withTempHome(t)
	// Force the file-fallback creds store under the temp HOME, with no entries.
	// This means the only accounts we discover come from client configs.

	// Patch claude-code with legacy and one named entry.
	if _, _, err := clients.ByID("claude-code", "").Patch("/usr/local/bin/serverpilot-mcp", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := clients.ByID("claude-code", "acme").Patch("/usr/local/bin/serverpilot-mcp", map[string]string{"SP_ACCOUNT": "acme"}, false); err != nil {
		t.Fatal(err)
	}

	rows := listAccounts()
	want := map[string]bool{"": false, "acme": false}
	for _, r := range rows {
		if _, ok := want[r.Name]; ok {
			want[r.Name] = true
		}
		// HasCredentials may be false since we didn't write creds, but the
		// entry-discovery side should still surface the account.
		if len(r.ClientEntries) == 0 {
			t.Errorf("account %q has no client entries (rows: %+v)", r.Name, rows)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("account %q not discovered (got rows: %+v)", name, rows)
		}
	}
}

func TestAccountLabel(t *testing.T) {
	if got := accountLabel(""); got != "default" {
		t.Errorf("accountLabel(\"\") = %q, want \"default\"", got)
	}
	if got := accountLabel("acme"); got != "acme" {
		t.Errorf("accountLabel(\"acme\") = %q, want \"acme\"", got)
	}
}

// Ensure pristine state doesn't blow up listAccounts.
func TestListAccounts_EmptyState(t *testing.T) {
	withTempHome(t)
	rows := listAccounts()
	_ = rows // empty or only legacy if a keychain happens to have data; just exercise it
	// No assertion: this test is about not panicking.
	if _, err := os.Stat(os.Getenv("HOME")); err != nil {
		t.Fatal(err)
	}
}
