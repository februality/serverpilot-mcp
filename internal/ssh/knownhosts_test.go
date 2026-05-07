package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xssh "golang.org/x/crypto/ssh"
)

func makeHostKey(t *testing.T) xssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := xssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func fakeAddr(t *testing.T) net.Addr {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "203.0.113.1:22")
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestHostKeyVerifier_FirstTimePins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	v := NewHostKeyVerifier(path, false)

	key := makeHostKey(t)
	if err := v.Callback()("203.0.113.1:22", fakeAddr(t), key); err != nil {
		t.Fatalf("first call should succeed (TOFU pin): %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 {
		t.Fatal("expected known_hosts to be populated after pin")
	}
	if !strings.Contains(string(contents), key.Type()) {
		t.Errorf("known_hosts missing key type %q:\n%s", key.Type(), contents)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("known_hosts mode = %o, want 0600", mode)
	}
}

func TestHostKeyVerifier_AcceptsKnownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	v := NewHostKeyVerifier(path, false)
	key := makeHostKey(t)

	if err := v.Callback()("203.0.113.1:22", fakeAddr(t), key); err != nil {
		t.Fatalf("pin call: %v", err)
	}
	before, _ := os.ReadFile(path)

	if err := v.Callback()("203.0.113.1:22", fakeAddr(t), key); err != nil {
		t.Fatalf("second call should accept pinned key: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("known_hosts should be unchanged on match.\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestHostKeyVerifier_RejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	v := NewHostKeyVerifier(path, false)

	key1 := makeHostKey(t)
	key2 := makeHostKey(t)

	if err := v.Callback()("203.0.113.1:22", fakeAddr(t), key1); err != nil {
		t.Fatalf("pin call: %v", err)
	}
	err := v.Callback()("203.0.113.1:22", fakeAddr(t), key2)
	if err == nil {
		t.Fatal("expected mismatch error for different key on pinned host")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error message should describe mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error message should reference the known_hosts path %q: %v", path, err)
	}
}

func TestHostKeyVerifier_InsecureBypass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	v := NewHostKeyVerifier(path, true)
	key := makeHostKey(t)

	if err := v.Callback()("203.0.113.1:22", fakeAddr(t), key); err != nil {
		t.Fatalf("insecure mode should never fail: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("insecure mode should not write to known_hosts; got stat err: %v", err)
	}
}

func TestHostKeyVerifier_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "known_hosts")
	v := NewHostKeyVerifier(path, false)
	key := makeHostKey(t)

	if err := v.Callback()("203.0.113.1:22", fakeAddr(t), key); err != nil {
		t.Fatalf("verifier should create parent dirs: %v", err)
	}
}
