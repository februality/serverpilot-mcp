package creds

import (
	"errors"

	"github.com/99designs/keyring"
)

type keyringStore struct {
	r keyring.Keyring
}

// openKeyring opens an OS-native keychain. Restricts to backends that don't
// prompt for a passphrase: macOS Keychain, Windows Credential Manager,
// Linux Secret Service. Returns an error on systems where none are present
// (headless Linux, Docker without a host bridge, WSL minimal).
func openKeyring() (Store, error) {
	r, err := keyring.Open(keyring.Config{
		ServiceName: ServiceName,
		AllowedBackends: []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.WinCredBackend,
			keyring.SecretServiceBackend,
		},
		KeychainTrustApplication:       true,
		KeychainAccessibleWhenUnlocked: true,
	})
	if err != nil {
		return nil, err
	}
	// Probe: a working backend should let us List() without error. This
	// surfaces "no D-Bus session" failures here instead of on first Set().
	if _, err := r.Keys(); err != nil {
		return nil, err
	}
	return &keyringStore{r: r}, nil
}

func (k *keyringStore) Get(key string) (string, error) {
	item, err := k.r.Get(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return string(item.Data), nil
}

func (k *keyringStore) Set(key, value string) error {
	return k.r.Set(keyring.Item{
		Key:         key,
		Data:        []byte(value),
		Label:       ServiceName + "/" + key,
		Description: "ServerPilot MCP credential",
	})
}

func (k *keyringStore) Delete(key string) error {
	err := k.r.Remove(key)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	return err
}

func (k *keyringStore) Backend() Source { return SourceKeyring }
