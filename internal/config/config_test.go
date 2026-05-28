package config

import "testing"

func TestIsTrueEnv(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"  ", false},
		{"anything", false},

		{"1", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"  1  ", true},
	}
	for _, c := range cases {
		if got := IsTrueEnv(c.in); got != c.want {
			t.Errorf("IsTrueEnv(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLoad_ReadOnlyFromEnv(t *testing.T) {
	// Load() needs credentials; skip if not present in this environment.
	t.Setenv("SERVERPILOT_CLIENT_ID", "cid_test")
	t.Setenv("SERVERPILOT_API_KEY", "key_test")

	t.Setenv("SP_READ_ONLY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReadOnly {
		t.Error("ReadOnly should default to false when SP_READ_ONLY is unset")
	}

	t.Setenv("SP_READ_ONLY", "1")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ReadOnly {
		t.Error("ReadOnly should be true when SP_READ_ONLY=1")
	}

	t.Setenv("SP_READ_ONLY", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReadOnly {
		t.Error("ReadOnly should be false when SP_READ_ONLY=false")
	}
}

func TestAccountDefaults(t *testing.T) {
	keyPath, keyName, kh := AccountDefaults("")
	if keyPath != DefaultSSHKeyPath || keyName != DefaultSSHKeyName || kh != DefaultKnownHostsPath {
		t.Errorf("legacy: got (%q, %q, %q), want defaults", keyPath, keyName, kh)
	}

	keyPath, keyName, kh = AccountDefaults("acme")
	if keyPath != "~/.ssh/serverpilot-mcp-acme" {
		t.Errorf("named keyPath = %q", keyPath)
	}
	if keyName != "claude-mcp-serverpilot-acme" {
		t.Errorf("named keyName = %q", keyName)
	}
	if kh != "~/.ssh/serverpilot-mcp-acme_known_hosts" {
		t.Errorf("named knownHosts = %q", kh)
	}
}

func TestLoad_SPAccountShiftsSSHDefaults(t *testing.T) {
	t.Setenv("SERVERPILOT_CLIENT_ID", "cid_test")
	t.Setenv("SERVERPILOT_API_KEY", "key_test")
	t.Setenv("SP_ACCOUNT", "acme")
	// Ensure no overrides leak in.
	t.Setenv("SP_SSH_KEY_PATH", "")
	t.Setenv("SP_SSH_KEY_NAME", "")
	t.Setenv("SP_KNOWN_HOSTS_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Account != "acme" {
		t.Errorf("Account = %q, want \"acme\"", cfg.Account)
	}
	// Expanded paths must include the per-account suffix.
	if !contains(cfg.SSHKeyPath, "serverpilot-mcp-acme") {
		t.Errorf("SSHKeyPath = %q (missing -acme suffix)", cfg.SSHKeyPath)
	}
	if cfg.SSHKeyName != "claude-mcp-serverpilot-acme" {
		t.Errorf("SSHKeyName = %q", cfg.SSHKeyName)
	}
	if !contains(cfg.KnownHostsPath, "serverpilot-mcp-acme_known_hosts") {
		t.Errorf("KnownHostsPath = %q", cfg.KnownHostsPath)
	}
}

func TestLoad_SPAccountUnsetKeepsLegacyDefaults(t *testing.T) {
	t.Setenv("SERVERPILOT_CLIENT_ID", "cid_test")
	t.Setenv("SERVERPILOT_API_KEY", "key_test")
	t.Setenv("SP_ACCOUNT", "")
	t.Setenv("SP_SSH_KEY_PATH", "")
	t.Setenv("SP_SSH_KEY_NAME", "")
	t.Setenv("SP_KNOWN_HOSTS_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Account != "" {
		t.Errorf("Account = %q, want \"\"", cfg.Account)
	}
	if cfg.SSHKeyName != DefaultSSHKeyName {
		t.Errorf("SSHKeyName = %q, want %q", cfg.SSHKeyName, DefaultSSHKeyName)
	}
	// Sanity: no -acme leak.
	if contains(cfg.SSHKeyPath, "-acme") {
		t.Errorf("SSHKeyPath unexpectedly contains -acme: %q", cfg.SSHKeyPath)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
