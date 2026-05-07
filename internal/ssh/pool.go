package ssh

import (
	"fmt"
	"net"
	"os"
	"strconv"
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
}

// Pool reuses SSH connections per user@host with a 60-second idle timeout
// and a 30-second cleanup sweep. Host-key verification is delegated to a
// HostKeyVerifier (see knownhosts.go).
type Pool struct {
	keyPath   string
	timeoutMs int
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
		verifier:  verifier,
		entries:   make(map[string]*poolEntry),
		stopCh:    make(chan struct{}),
	}
	go p.cleanupLoop()
	return p
}

func poolKey(host, user string) string { return user + "@" + host }

// Get returns a pooled or freshly opened SSH client for user@host. The caller
// must NOT close the returned client; the pool owns it.
func (p *Pool) Get(host, user string) (*xssh.Client, error) {
	key := poolKey(host, user)
	p.mu.Lock()
	if e, ok := p.entries[key]; ok {
		e.lastUsed = time.Now()
		p.mu.Unlock()
		return e.client, nil
	}
	p.mu.Unlock()

	client, err := p.dial(host, user)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	// Race: another caller may have inserted while we were dialling. Use
	// theirs and close ours to avoid a leak.
	if e, ok := p.entries[key]; ok {
		p.mu.Unlock()
		_ = client.Close()
		return e.client, nil
	}
	p.entries[key] = &poolEntry{client: client, lastUsed: time.Now()}
	p.mu.Unlock()

	// Auto-remove on disconnect.
	go func() {
		_ = client.Wait()
		p.mu.Lock()
		if e, ok := p.entries[key]; ok && e.client == client {
			delete(p.entries, key)
		}
		p.mu.Unlock()
	}()

	return client, nil
}

func (p *Pool) dial(host, user string) (*xssh.Client, error) {
	keyBytes, err := os.ReadFile(p.keyPath)
	if err != nil {
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
	addr := net.JoinHostPort(host, strconv.Itoa(22))
	client, err := xssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("SSH connection to %s@%s failed: %w", user, host, err)
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
		if now.Sub(e.lastUsed) > idleTimeout {
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
