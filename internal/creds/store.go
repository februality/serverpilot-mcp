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
//
// Multi-account model: credentials for named accounts are stored under
// composite keys ("<account>:client_id", "<account>:api_key"). LegacyAccount
// is the sentinel for the original single-account install — its credentials
// remain at the bare "client_id" / "api_key" keys so existing installs are
// byte-identical post-upgrade.
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

	// LegacyAccount is the empty-string sentinel for the unnamed account
	// (pre-multi-account installs). All account-aware functions short-circuit
	// to the original storage shape when passed this value.
	LegacyAccount = ""
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

// accountKey returns the storage key for an (account, attr) pair. For
// LegacyAccount it returns the bare attribute so the stored bytes match
// pre-multi-account installs exactly.
func accountKey(account, attr string) string {
	if account == LegacyAccount {
		return attr
	}
	return account + ":" + attr
}

// Store is the persistence backend for credentials. Either an OS keychain
// (preferred) or a permission-restricted JSON file.
type Store interface {
	// Get/Set/Delete operate on the legacy unnamed-account slot. Kept for
	// backwards compatibility with single-account callers.
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	Backend() Source // SourceKeyring or SourceFile

	// GetFor/SetFor/DeleteFor are account-namespaced. Passing LegacyAccount
	// is equivalent to Get/Set/Delete.
	GetFor(account, attr string) (string, error)
	SetFor(account, attr, value string) error
	DeleteFor(account, attr string) error

	// Accounts returns every account with at least one stored attribute.
	// LegacyAccount ("") is included when the legacy slot has any data.
	Accounts() ([]string, error)
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

// ResolveAPICredentials resolves both credentials for the legacy unnamed
// account. Kept for callers that haven't been updated to multi-account yet.
func ResolveAPICredentials() (*Resolved, error) {
	return ResolveAPICredentialsFor(LegacyAccount)
}

// ResolveAPICredentialsFor resolves credentials for a specific account.
// Env vars still win — they apply to whichever account this process is
// running as (set via SP_ACCOUNT in the MCP-client config).
func ResolveAPICredentialsFor(account string) (*Resolved, error) {
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
		v, err := store.GetFor(account, KeyClientID)
		if err != nil {
			return &Resolved{Source: SourceNotFound}, nil
		}
		cid = v
	}
	if key == "" {
		v, err := store.GetFor(account, KeyAPIKey)
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

// ListAccounts returns every account name with stored credentials in the
// active store. LegacyAccount ("") is included when the legacy slot has
// any data. Returns a sorted slice (legacy first if present).
func ListAccounts() ([]string, error) {
	store, err := Open()
	if err != nil {
		return nil, err
	}
	return store.Accounts()
}
