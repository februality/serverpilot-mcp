package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// FileStore persists credentials as JSON at mode 0600 in the user's config
// directory. Used as a fallback when the OS keychain isn't available.
type FileStore struct {
	path string
}

// fileEnvelope is the on-disk JSON shape. Field declaration order is
// alphabetical so a legacy-only install round-trips byte-identical to the
// pre-multi-account format (which used a flat map<string,string> with Go's
// sorted-key map marshaling).
type fileEnvelope struct {
	Accounts map[string]map[string]string `json:"accounts,omitempty"`
	APIKey   string                       `json:"api_key,omitempty"`
	ClientID string                       `json:"client_id,omitempty"`
}

func (e *fileEnvelope) empty() bool {
	return e.ClientID == "" && e.APIKey == "" && len(e.Accounts) == 0
}

func (e *fileEnvelope) get(account, attr string) (string, bool) {
	if account == LegacyAccount {
		v := e.legacyField(attr)
		return v, v != ""
	}
	if e.Accounts == nil {
		return "", false
	}
	m, ok := e.Accounts[account]
	if !ok {
		return "", false
	}
	v, ok := m[attr]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func (e *fileEnvelope) legacyField(attr string) string {
	switch attr {
	case KeyClientID:
		return e.ClientID
	case KeyAPIKey:
		return e.APIKey
	}
	return ""
}

func (e *fileEnvelope) set(account, attr, value string) {
	if account == LegacyAccount {
		switch attr {
		case KeyClientID:
			e.ClientID = value
		case KeyAPIKey:
			e.APIKey = value
		}
		return
	}
	if e.Accounts == nil {
		e.Accounts = map[string]map[string]string{}
	}
	if e.Accounts[account] == nil {
		e.Accounts[account] = map[string]string{}
	}
	e.Accounts[account][attr] = value
}

func (e *fileEnvelope) delete(account, attr string) bool {
	if account == LegacyAccount {
		switch attr {
		case KeyClientID:
			if e.ClientID == "" {
				return false
			}
			e.ClientID = ""
			return true
		case KeyAPIKey:
			if e.APIKey == "" {
				return false
			}
			e.APIKey = ""
			return true
		}
		return false
	}
	if e.Accounts == nil {
		return false
	}
	m, ok := e.Accounts[account]
	if !ok {
		return false
	}
	if _, ok := m[attr]; !ok {
		return false
	}
	delete(m, attr)
	if len(m) == 0 {
		delete(e.Accounts, account)
	}
	if len(e.Accounts) == 0 {
		e.Accounts = nil
	}
	return true
}

// DefaultFilePath returns the canonical credentials-file path for this OS.
// Honors XDG_CONFIG_HOME on Unix.
func DefaultFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ServiceName, "credentials.json"), nil
}

// OpenFile returns a FileStore at the default path.
func OpenFile() (Store, error) {
	p, err := DefaultFilePath()
	if err != nil {
		return nil, err
	}
	return &FileStore{path: p}, nil
}

func (f *FileStore) load() (*fileEnvelope, error) {
	env := &fileEnvelope{}
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return env, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return env, nil
	}
	if err := json.Unmarshal(b, env); err != nil {
		return nil, fmt.Errorf("parse credentials file %s: %w", f.path, err)
	}
	return env, nil
}

func (f *FileStore) save(env *fileEnvelope) error {
	if env.empty() {
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *FileStore) Get(key string) (string, error) {
	return f.GetFor(LegacyAccount, key)
}

func (f *FileStore) Set(key, value string) error {
	return f.SetFor(LegacyAccount, key, value)
}

func (f *FileStore) Delete(key string) error {
	return f.DeleteFor(LegacyAccount, key)
}

func (f *FileStore) GetFor(account, attr string) (string, error) {
	env, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := env.get(account, attr)
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *FileStore) SetFor(account, attr, value string) error {
	env, err := f.load()
	if err != nil {
		return err
	}
	env.set(account, attr, value)
	return f.save(env)
}

func (f *FileStore) DeleteFor(account, attr string) error {
	env, err := f.load()
	if err != nil {
		return err
	}
	if !env.delete(account, attr) {
		return nil
	}
	return f.save(env)
}

func (f *FileStore) Accounts() ([]string, error) {
	env, err := f.load()
	if err != nil {
		return nil, err
	}
	out := []string{}
	if env.ClientID != "" || env.APIKey != "" {
		out = append(out, LegacyAccount)
	}
	for name, m := range env.Accounts {
		if len(m) == 0 {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (f *FileStore) Backend() Source { return SourceFile }

// Path returns the on-disk path for diagnostics.
func (f *FileStore) Path() string { return f.path }

// Ensure the embedded errors.Is contract holds for callers that want to
// detect "no creds yet" without depending on the package's sentinel.
var _ = errors.Is
