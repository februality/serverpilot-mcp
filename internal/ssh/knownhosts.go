package ssh

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyVerifier implements TOFU (trust-on-first-use) host-key verification
// against a managed known_hosts file. First contact pins the key; subsequent
// connections require an exact match. A mismatch fails loud and never
// auto-trusts.
type HostKeyVerifier struct {
	path     string
	insecure bool
	mu       sync.Mutex
	warnOnce sync.Once
}

// NewHostKeyVerifier constructs a verifier writing to `path`. If `insecure`
// is true, host-key verification is disabled and a single WARN is logged the
// first time the callback runs (intended for testing only).
func NewHostKeyVerifier(path string, insecure bool) *HostKeyVerifier {
	return &HostKeyVerifier{path: path, insecure: insecure}
}

// Callback returns the xssh.HostKeyCallback to install in ClientConfig.
func (v *HostKeyVerifier) Callback() xssh.HostKeyCallback {
	if v.insecure {
		return func(hostname string, remote net.Addr, key xssh.PublicKey) error {
			v.warnOnce.Do(func() {
				slog.Warn("SSH host-key verification disabled (SP_INSECURE_DISABLE_HOST_KEY_CHECK=1)")
			})
			return nil
		}
	}
	return v.verify
}

func (v *HostKeyVerifier) verify(hostname string, remote net.Addr, key xssh.PublicKey) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := ensureFile(v.path); err != nil {
		return fmt.Errorf("known_hosts: %w", err)
	}

	cb, err := knownhosts.New(v.path)
	if err != nil {
		return fmt.Errorf("load known_hosts %s: %w", v.path, err)
	}

	err = cb(hostname, remote, key)
	if err == nil {
		return nil
	}

	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return err
	}

	if len(keyErr.Want) > 0 {
		return fmt.Errorf(
			"SSH host-key mismatch for %s: the server presented a key that does not match the pinned entry in %s. "+
				"This may indicate a man-in-the-middle attack, or that the server was reinstalled. "+
				"If you trust the change, remove the entry for this host from %s and reconnect.",
			hostname, v.path, v.path,
		)
	}

	// Empty Want → host unknown. TOFU: pin it.
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	if err := appendLine(v.path, line); err != nil {
		return fmt.Errorf("pin host key for %s: %w", hostname, err)
	}
	slog.Info("pinned SSH host key", "host", hostname, "fingerprint", xssh.FingerprintSHA256(key), "path", v.path)
	return nil
}

func ensureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return f.Close()
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
