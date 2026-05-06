package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xssh "golang.org/x/crypto/ssh"
)

// fixedReader returns the same byte over and over — gives a deterministic
// Ed25519 seed for golden testing.
type fixedReader byte

func (f fixedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(f)
	}
	return len(p), nil
}

func TestGenerateKeyPair_RoundtripWithCryptoSSH(t *testing.T) {
	pair, err := GenerateKeyPair("claude-mcp-serverpilot")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.ParsePrivateKey([]byte(pair.PrivateKey))
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v\n%s", err, pair.PrivateKey)
	}
	authPub, _, _, _, err := xssh.ParseAuthorizedKey([]byte(pair.PublicKey))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey failed: %v\n%s", err, pair.PublicKey)
	}
	if !bytes.Equal(signer.PublicKey().Marshal(), authPub.Marshal()) {
		t.Fatal("private key public component != published public key")
	}
	if signer.PublicKey().Type() != "ssh-ed25519" {
		t.Fatalf("type = %s", signer.PublicKey().Type())
	}
}

func TestGenerateKeyPair_DeterministicFromSeed(t *testing.T) {
	// Same random source → same bytes every time. Useful for verifying
	// that openssh-key-v1 encoding is stable.
	pair1, err := generateKeyPairFromRand("test-comment", fixedReader(0xAA))
	if err != nil {
		t.Fatal(err)
	}
	pair2, err := generateKeyPairFromRand("test-comment", fixedReader(0xAA))
	if err != nil {
		t.Fatal(err)
	}
	if pair1.PublicKey != pair2.PublicKey {
		t.Errorf("public keys differ:\n%s\n%s", pair1.PublicKey, pair2.PublicKey)
	}
	if pair1.PrivateKey != pair2.PrivateKey {
		t.Error("private keys differ")
	}
}

func TestGenerateKeyPair_PrivateKeyFormat(t *testing.T) {
	pair, err := GenerateKeyPair("c")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pair.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----\n") {
		t.Error("missing BEGIN header")
	}
	if !strings.HasSuffix(pair.PrivateKey, "-----END OPENSSH PRIVATE KEY-----\n") {
		t.Error("missing END header")
	}
	// All inner lines must be at most 70 chars.
	body := strings.TrimPrefix(pair.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	body = strings.TrimSuffix(body, "-----END OPENSSH PRIVATE KEY-----\n")
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if len(line) > 70 {
			t.Errorf("line exceeds 70 chars: %d", len(line))
		}
	}
}

func TestGenerateKeyPair_PublicKeyParses(t *testing.T) {
	pair, err := GenerateKeyPair("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(pair.PublicKey, " ", 3)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %q", len(parts), pair.PublicKey)
	}
	if parts[0] != "ssh-ed25519" || parts[2] != "alice@example.com" {
		t.Errorf("unexpected format: %q", pair.PublicKey)
	}
}

func TestEnsureKeyPair_GeneratesAndReuses(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "subdir", "id_ed25519")

	pair1, err := EnsureKeyPair(keyPath, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Files should exist with correct modes.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %o, want 0600", info.Mode().Perm())
	}
	pubInfo, err := os.Stat(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if pubInfo.Mode().Perm() != 0o644 {
		t.Errorf("public key mode = %o, want 0644", pubInfo.Mode().Perm())
	}

	pair2, err := EnsureKeyPair(keyPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	if pair1.PublicKey != pair2.PublicKey || pair1.PrivateKey != pair2.PrivateKey {
		t.Error("EnsureKeyPair regenerated existing keypair")
	}

	if !KeyPairExists(keyPath) {
		t.Error("KeyPairExists returned false")
	}
}

func TestEnsureKeyPair_RoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id")
	pair, err := EnsureKeyPair(keyPath, "rt-test")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.ParsePrivateKey([]byte(pair.PrivateKey))
	if err != nil {
		t.Fatalf("disk-written private key fails to parse: %v", err)
	}
	// Sign and verify a message to make sure the seed is correctly placed.
	msg := []byte("hello")
	sig, err := signer.Sign(rand.Reader, msg)
	if err != nil {
		t.Fatal(err)
	}
	pub := signer.PublicKey().(xssh.CryptoPublicKey).CryptoPublicKey().(ed25519.PublicKey)
	if !ed25519.Verify(pub, msg, sig.Blob) {
		t.Fatal("signature verification failed")
	}
}

// Sanity: the encoding logic doesn't use any io beyond our injected reader.
var _ io.Reader = fixedReader(0)
