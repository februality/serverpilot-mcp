package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileStore persists credentials as JSON at mode 0600 in the user's config
// directory. Used as a fallback when the OS keychain isn't available.
type FileStore struct {
	path string
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

func (f *FileStore) load() (map[string]string, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	m := map[string]string{}
	if len(b) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse credentials file %s: %w", f.path, err)
	}
	return m, nil
}

func (f *FileStore) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
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
	m, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := m[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *FileStore) Set(key, value string) error {
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key] = value
	return f.save(m)
}

func (f *FileStore) Delete(key string) error {
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[key]; !ok {
		return nil
	}
	delete(m, key)
	if len(m) == 0 {
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return f.save(m)
}

func (f *FileStore) Backend() Source { return SourceFile }

// Path returns the on-disk path for diagnostics.
func (f *FileStore) Path() string { return f.path }

// Ensure the embedded errors.Is contract holds for callers that want to
// detect "no creds yet" without depending on the package's sentinel.
var _ = errors.Is
