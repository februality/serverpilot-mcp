package creds

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAccountKey(t *testing.T) {
	cases := []struct {
		account, attr, want string
	}{
		{"", KeyClientID, "client_id"},
		{"", KeyAPIKey, "api_key"},
		{"acme", KeyClientID, "acme:client_id"},
		{"globex", KeyAPIKey, "globex:api_key"},
	}
	for _, c := range cases {
		got := accountKey(c.account, c.attr)
		if got != c.want {
			t.Errorf("accountKey(%q, %q) = %q, want %q", c.account, c.attr, got, c.want)
		}
	}
}

func TestFileStore_AccountNamespacing(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{path: filepath.Join(dir, "credentials.json")}

	// Legacy and named credentials coexist.
	if err := s.SetFor(LegacyAccount, KeyClientID, "legacy_cid"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFor(LegacyAccount, KeyAPIKey, "legacy_key"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFor("acme", KeyClientID, "acme_cid"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFor("acme", KeyAPIKey, "acme_key"); err != nil {
		t.Fatal(err)
	}

	if v, err := s.GetFor(LegacyAccount, KeyClientID); err != nil || v != "legacy_cid" {
		t.Errorf("GetFor legacy: %q err=%v", v, err)
	}
	if v, err := s.GetFor("acme", KeyAPIKey); err != nil || v != "acme_key" {
		t.Errorf("GetFor acme: %q err=%v", v, err)
	}
	// Legacy callers (Get/Set/Delete) still see the legacy slot.
	if v, _ := s.Get(KeyClientID); v != "legacy_cid" {
		t.Errorf("Get legacy via old API: %q", v)
	}

	// Accounts enumeration.
	accts, err := s.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accts) != 2 || accts[0] != "" || accts[1] != "acme" {
		t.Errorf("Accounts() = %v, want [\"\" \"acme\"]", accts)
	}

	// Delete just the legacy slot — acme survives.
	if err := s.DeleteFor(LegacyAccount, KeyClientID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFor(LegacyAccount, KeyAPIKey); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFor(LegacyAccount, KeyClientID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete legacy: %v", err)
	}
	if v, _ := s.GetFor("acme", KeyClientID); v != "acme_cid" {
		t.Errorf("acme lost after legacy delete: %q", v)
	}

	// Delete acme — file is removed.
	if err := s.DeleteFor("acme", KeyClientID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFor("acme", KeyAPIKey); err != nil {
		t.Fatal(err)
	}
	accts, _ = s.Accounts()
	if len(accts) != 0 {
		t.Errorf("Accounts() after all deletes: %v", accts)
	}
}

func TestFileStore_LegacyOnlyStaysFlatShape(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{path: filepath.Join(dir, "credentials.json")}

	if err := s.Set(KeyClientID, "cid"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(KeyAPIKey, "key"); err != nil {
		t.Fatal(err)
	}
	// Re-load — envelope should not have an Accounts map.
	env, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if env.ClientID != "cid" || env.APIKey != "key" {
		t.Errorf("legacy fields: %+v", env)
	}
	if env.Accounts != nil {
		t.Errorf("legacy-only file should not produce Accounts map: %v", env.Accounts)
	}
}

func TestResolveAPICredentialsFor_EnvOverridesPerAccount(t *testing.T) {
	t.Setenv("SERVERPILOT_CLIENT_ID", "env_cid")
	t.Setenv("SERVERPILOT_API_KEY", "env_key")
	// Even when asking about a named account, env wins.
	r, err := ResolveAPICredentialsFor("acme")
	if err != nil {
		t.Fatal(err)
	}
	if r.ClientID != "env_cid" || r.APIKey != "env_key" || r.Source != SourceEnv {
		t.Errorf("got %+v", r)
	}
}
