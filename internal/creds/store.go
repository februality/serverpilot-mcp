// Package creds resolves and persists ServerPilot API credentials.
//
// Resolution order (used by ResolveAPICredentials):
//
//  1. Environment variables (SERVERPILOT_CLIENT_ID, SERVERPILOT_API_KEY) —
//     always win, useful for CI / ephemeral containers.
//  2. OS keychain (macOS Keychain, Windows Credential Manager, Linux
//     Secret Service) via github.com/99designs/keyring.
//  3. File fallback at ~/.config/serverpilot-mcp/credentials.json (mode 0600)
//     for headless Linux / WSL / Docker where no keychain daemon is running.
package creds

import (
	"errors"
	"fmt"
	"os"
)

const (
	ServiceName = "serverpilot-mcp"
	KeyClientID = "client_id"
	KeyAPIKey   = "api_key"
)

// Source tags where a resolved credential came from. Used in `status` /
// `doctor` output so the user can see exactly which layer is providing it.
type Source string

const (
	SourceEnv      Source = "env"
	SourceKeyring  Source = "keyring"
	SourceFile     Source = "file"
	SourceNotFound Source = "not_found"
)

// Store is the persistence backend for credentials. Either an OS keychain
// (preferred) or a permission-restricted JSON file.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	Backend() Source // SourceKeyring or SourceFile
}

// ErrNotFound is returned by Store.Get when the requested key is absent.
var ErrNotFound = errors.New("credential not found")

// Open returns the best available Store for this environment. Tries the OS
// keychain first; falls back to FileStore on systems without one.
func Open() (Store, error) {
	if ks, err := openKeyring(); err == nil {
		return ks, nil
	}
	return OpenFile()
}

// Resolved is the output of ResolveAPICredentials.
type Resolved struct {
	ClientID string
	APIKey   string
	Source   Source
}

// ResolveAPICredentials resolves both credentials in resolution order. If
// the env vars are set they win regardless of any stored values; otherwise
// the function opens the best available Store.
func ResolveAPICredentials() (*Resolved, error) {
	cid := os.Getenv("SERVERPILOT_CLIENT_ID")
	key := os.Getenv("SERVERPILOT_API_KEY")
	if cid != "" && key != "" {
		return &Resolved{ClientID: cid, APIKey: key, Source: SourceEnv}, nil
	}
	store, err := Open()
	if err != nil {
		return nil, fmt.Errorf("open credentials store: %w", err)
	}
	if cid == "" {
		v, err := store.Get(KeyClientID)
		if err != nil {
			return &Resolved{Source: SourceNotFound}, nil
		}
		cid = v
	}
	if key == "" {
		v, err := store.Get(KeyAPIKey)
		if err != nil {
			return &Resolved{Source: SourceNotFound}, nil
		}
		key = v
	}
	if cid == "" || key == "" {
		return &Resolved{Source: SourceNotFound}, nil
	}
	return &Resolved{ClientID: cid, APIKey: key, Source: store.Backend()}, nil
}
