package ssh

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDial_MissingKeyPointsToSetup(t *testing.T) {
	p := NewPool(filepath.Join(t.TempDir(), "nope"), 1000, NewHostKeyVerifier(filepath.Join(t.TempDir(), "kh"), true))
	defer p.Close()
	_, _, err := p.Get("127.0.0.1", "alice")
	if err == nil || !strings.Contains(err.Error(), "call sp_ssh_setup") {
		t.Fatalf("got %v", err)
	}
}

func TestDialHint_AuthFailure(t *testing.T) {
	h := dialHint(errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey]"), "alice")
	if !strings.Contains(h, `users: ["alice"]`) || !strings.Contains(h, "sp_ssh_setup") {
		t.Fatalf("got %q", h)
	}
}
