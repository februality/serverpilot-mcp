package ssh

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	xssh "golang.org/x/crypto/ssh"
)

const (
	idleTimeout     = 60 * time.Second
	cleanupInterval = 30 * time.Second
)

type poolEntry struct {
	client   *xssh.Client
	lastUsed time.Time
	// inUse counts operations currently holding the client. The idle reaper
	// never closes an entry with inUse > 0, so a command or transfer longer
	// than idleTimeout isn't cut off mid-run.
	inUse int
}

// Pool reuses SSH connections per user@host with a 60-second idle timeout
// and a 30-second cleanup sweep. Host-key verification is delegated to a
// HostKeyVerifier (see knownhosts.go).
type Pool struct {
	keyPath   string
	timeoutMs int
	port      int // 22; overridable in tests
	verifier  *HostKeyVerifier

	mu       sync.Mutex
	entries  map[string]*poolEntry
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewPool(keyPath string, timeoutMs int, verifier *HostKeyVerifier) *Pool {
	p := &Pool{
		keyPath:   keyPath,
		timeoutMs: timeoutMs,
		port:      22,
		verifier:  verifier,
		entries:   make(map[string]*poolEntry),
		stopCh:    make(chan struct{}),
	}
	go p.cleanupLoop()
	return p
}

func poolKey(host, user string) string { return user + "@" + host }

// Get returns a pooled or freshly opened SSH client for user@host and a
// release func that must be called (once) when the caller is done with it.
// The caller must NOT close the client; the pool owns it (see Evict).
func (p *Pool) Get(host, user string) (*xssh.Client, func(), error) {
	key := poolKey(host, user)
	p.mu.Lock()
	if e, ok := p.entries[key]; ok {
		e.inUse++
		e.lastUsed = time.Now()
		p.mu.Unlock()
		return e.client, p.releaser(e), nil
	}
	p.mu.Unlock()

	client, err := p.dial(host, user)
	if err != nil {
		return nil, nil, err
	}

	p.mu.Lock()
	if e, ok := p.entries[key]; ok {
		e.inUse++
		e.lastUsed = time.Now()
		p.mu.Unlock()
		_ = client.Close()
		return e.client, p.releaser(e), nil
	}
	e := &poolEntry{client: client, lastUsed: time.Now(), inUse: 1}
	p.entries[key] = e
	p.mu.Unlock()

	go func() {
		_ = client.Wait()
		p.mu.Lock()
		if cur, ok := p.entries[key]; ok && cur.client == client {
			delete(p.entries, key)
		}
		p.mu.Unlock()
	}()

	return client, p.releaser(e), nil
}

// Evict drops client from the pool (if it is still the pooled client for
// user@host) and closes it, which also unblocks anything waiting on it.
func (p *Pool) Evict(host, user string, client *xssh.Client) {
	key := poolKey(host, user)
	p.mu.Lock()
	if e, ok := p.entries[key]; ok && e.client == client {
		delete(p.entries, key)
	}
	p.mu.Unlock()
	_ = client.Close()
}

// openTimeout bounds opening a session/SFTP channel on an existing client.
func (p *Pool) openTimeout() time.Duration {
	if p.timeoutMs > 0 {
		return time.Duration(p.timeoutMs) * time.Millisecond
	}
	return 30 * time.Second
}

func (p *Pool) releaser(e *poolEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			e.inUse--
			e.lastUsed = time.Now()
			p.mu.Unlock()
		})
	}
}

func (p *Pool) dial(host, user string) (*xssh.Client, error) {
	keyBytes, err := os.ReadFile(p.keyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("SSH key not found at %s. SSH access has not been set up for this account yet: "+
				"call sp_ssh_setup first (it generates the key and assigns it to system users), then retry", p.keyPath)
		}
		return nil, fmt.Errorf("read SSH key %s: %w", p.keyPath, err)
	}
	signer, err := xssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse SSH key %s: %w", p.keyPath, err)
	}
	cfg := &xssh.ClientConfig{
		User:            user,
		Auth:            []xssh.AuthMethod{xssh.PublicKeys(signer)},
		HostKeyCallback: p.verifier.Callback(),
		Timeout:         time.Duration(p.timeoutMs) * time.Millisecond,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(p.port))
	client, err := xssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("SSH connection to %s@%s failed: %w%s", user, host, err, dialHint(err, user))
	}
	return client, nil
}

func (p *Pool) cleanupLoop() {
	t := time.NewTicker(cleanupInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-t.C:
			p.cleanup()
		}
	}
}

func (p *Pool) cleanup() {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, e := range p.entries {
		if e.inUse == 0 && now.Sub(e.lastUsed) > idleTimeout {
			_ = e.client.Close()
			delete(p.entries, k)
		}
	}
}

// Close terminates all pooled connections and stops the cleanup goroutine.
// Safe to call multiple times.
func (p *Pool) Close() {
	p.stopOnce.Do(func() { close(p.stopCh) })
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, e := range p.entries {
		_ = e.client.Close()
		delete(p.entries, k)
	}
}

// dialHint appends a recovery step to common SSH dial failures so the model
// knows what to call next instead of retrying blindly.
func dialHint(err error, user string) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"):
		return fmt.Sprintf(". The SSH key is not authorized for system user %q. "+
			"Call sp_ssh_status to check, then sp_ssh_setup (optionally with users: [%q]) to assign it. "+
			"A newly assigned key can take a short time to reach the server, so retry once after a moment", user, user)
	case strings.Contains(msg, "host-key mismatch"):
		return "" // the verifier's own message already explains what to do
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no route to host"):
		return ". The server is unreachable on port 22; check that it is online with sp_get_server"
	}
	return ""
}
