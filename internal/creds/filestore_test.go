package creds

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{path: filepath.Join(dir, "credentials.json")}

	if _, err := s.Get(KeyClientID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Set(KeyClientID, "cid_123"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(KeyAPIKey, "secret"); err != nil {
		t.Fatal(err)
	}

	if v, err := s.Get(KeyClientID); err != nil || v != "cid_123" {
		t.Errorf("Get cid: %q err=%v", v, err)
	}
	if v, err := s.Get(KeyAPIKey); err != nil || v != "secret" {
		t.Errorf("Get key: %q err=%v", v, err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %o, want 0600", info.Mode().Perm())
		}
	}

	if err := s.Delete(KeyClientID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(KeyClientID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	// Other key still present.
	if v, _ := s.Get(KeyAPIKey); v != "secret" {
		t.Error("api key was unexpectedly removed")
	}

	// Deleting last key removes the file.
	if err := s.Delete(KeyAPIKey); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Errorf("expected file removed when last key deleted, stat err=%v", err)
	}
}

func TestResolveAPICredentials_EnvOverride(t *testing.T) {
	t.Setenv("SERVERPILOT_CLIENT_ID", "env_cid")
	t.Setenv("SERVERPILOT_API_KEY", "env_key")
	r, err := ResolveAPICredentials()
	if err != nil {
		t.Fatal(err)
	}
	if r.ClientID != "env_cid" || r.APIKey != "env_key" || r.Source != SourceEnv {
		t.Errorf("got %+v", r)
	}
}
