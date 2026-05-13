// Package config holds environment-variable defaults (SP_SSH_KEY_PATH,
// SP_CACHE_TTL_SECONDS, etc.) and the small helpers that resolve them
// alongside stored credentials.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/februality/serverpilot-mcp/internal/creds"
)

type Config struct {
	ClientID          string
	APIKey            string
	CredsSource       creds.Source
	SSHKeyPath        string
	SSHKeyName        string
	KnownHostsPath    string
	CacheTTLSeconds   int
	SSHTimeoutMs      int
	SiteExecTimeoutMs int
	InsecureHostKey   bool
	// ReadOnly hides every mutating tool from the MCP server's tools/list.
	// Driven by SP_READ_ONLY (1/true/yes). Off by default.
	ReadOnly bool
}

const (
	DefaultSSHKeyPath        = "~/.ssh/serverpilot-mcp"
	DefaultSSHKeyName        = "claude-mcp-serverpilot"
	DefaultKnownHostsPath    = "~/.ssh/serverpilot-mcp_known_hosts"
	DefaultCacheTTLSec       = 300
	DefaultSSHTimeout        = 30000
	DefaultSiteExecTimeoutMs = 120000
)

// ErrNoCredentials is returned by Load when neither environment vars nor
// the credentials store contain ServerPilot credentials. CLI callers can
// detect this and print a "run `serverpilot-mcp setup`" message.
var ErrNoCredentials = errors.New("no ServerPilot credentials configured")

// Load resolves all configuration. Credentials come from env > keychain >
// file (see internal/creds). Other settings come from env vars only.
func Load() (*Config, error) {
	resolved, err := creds.ResolveAPICredentials()
	if err != nil {
		return nil, err
	}
	if resolved.Source == creds.SourceNotFound {
		return nil, ErrNoCredentials
	}

	sshKeyPath := os.Getenv("SP_SSH_KEY_PATH")
	if sshKeyPath == "" {
		sshKeyPath = DefaultSSHKeyPath
	}
	expanded, err := ExpandHome(sshKeyPath)
	if err != nil {
		return nil, err
	}

	sshKeyName := os.Getenv("SP_SSH_KEY_NAME")
	if sshKeyName == "" {
		sshKeyName = DefaultSSHKeyName
	}

	knownHostsPath := os.Getenv("SP_KNOWN_HOSTS_PATH")
	if knownHostsPath == "" {
		knownHostsPath = DefaultKnownHostsPath
	}
	expandedKnownHosts, err := ExpandHome(knownHostsPath)
	if err != nil {
		return nil, err
	}

	return &Config{
		ClientID:          resolved.ClientID,
		APIKey:            resolved.APIKey,
		CredsSource:       resolved.Source,
		SSHKeyPath:        expanded,
		SSHKeyName:        sshKeyName,
		KnownHostsPath:    expandedKnownHosts,
		CacheTTLSeconds:   intEnv("SP_CACHE_TTL_SECONDS", DefaultCacheTTLSec),
		SSHTimeoutMs:      intEnv("SP_SSH_TIMEOUT_MS", DefaultSSHTimeout),
		SiteExecTimeoutMs: intEnv("SP_SITE_EXEC_TIMEOUT_MS", DefaultSiteExecTimeoutMs),
		InsecureHostKey:   os.Getenv("SP_INSECURE_DISABLE_HOST_KEY_CHECK") == "1",
		ReadOnly:          boolEnv("SP_READ_ONLY"),
	}, nil
}

// IsTrueEnv parses 1/true/yes/on (case-insensitive, trimmed) as true.
// Anything else, including unset and empty, is false. Shared with callers
// that need to interpret env-var-shaped values (e.g. status, doctor).
func IsTrueEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func boolEnv(name string) bool {
	return IsTrueEnv(os.Getenv(name))
}

func ExpandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

func intEnv(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
