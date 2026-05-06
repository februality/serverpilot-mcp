// Package ssh provides Ed25519 key generation in the OpenSSH on-disk format
// plus a connection pool and SFTP/exec operations.
//
// Key format details mirror src/ssh/keygen.ts byte-for-byte: the private key
// is encoded as openssh-key-v1 with cipher="none" and KDF="none", padded to an
// 8-byte boundary with the standard 1,2,3,... pad bytes. The public key is the
// usual `ssh-ed25519 BASE64 COMMENT` line.
package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const opensshMagic = "openssh-key-v1\x00"

type KeyPair struct {
	PublicKey  string // OpenSSH single-line: "ssh-ed25519 BASE64 COMMENT"
	PrivateKey string // PEM-encoded openssh-key-v1 blob
}

// GenerateKeyPair creates a new Ed25519 keypair and serializes it to the
// formats used by ServerPilot (public key line for the API; openssh-key-v1
// PEM for SSH clients). Random source is used for both the key seed and the
// 4-byte "check" value in the private blob.
func GenerateKeyPair(comment string) (*KeyPair, error) {
	return generateKeyPairFromRand(comment, rand.Reader)
}

func generateKeyPairFromRand(comment string, r io.Reader) (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	pubBytes := []byte(pub)
	publicLine := fmt.Sprintf("ssh-ed25519 %s %s",
		base64.StdEncoding.EncodeToString(buildPubBlob(pubBytes)),
		comment,
	)

	check := make([]byte, 4)
	if _, err := io.ReadFull(r, check); err != nil {
		return nil, fmt.Errorf("read check bytes: %w", err)
	}

	privatePEM := encodeOpenSSHPrivateKey(priv, pubBytes, comment, check)
	return &KeyPair{PublicKey: publicLine, PrivateKey: privatePEM}, nil
}

func buildPubBlob(pubBytes []byte) []byte {
	var b []byte
	b = append(b, sshString([]byte("ssh-ed25519"))...)
	b = append(b, sshString(pubBytes)...)
	return b
}

func encodeOpenSSHPrivateKey(priv ed25519.PrivateKey, pubBytes []byte, comment string, check []byte) string {
	pubBlob := buildPubBlob(pubBytes)

	// Ed25519 "private blob" = seed (32) || public (32) = 64 bytes.
	// ed25519.PrivateKey is already seed||public.
	ed25519PrivBlob := []byte(priv)

	var privSection []byte
	privSection = append(privSection, check...)
	privSection = append(privSection, check...)
	privSection = append(privSection, sshString([]byte("ssh-ed25519"))...)
	privSection = append(privSection, sshString(pubBytes)...)
	privSection = append(privSection, sshString(ed25519PrivBlob)...)
	privSection = append(privSection, sshString([]byte(comment))...)

	// Pad to 8-byte block boundary with bytes 1, 2, 3, ...
	const blockSize = 8
	padLen := blockSize - (len(privSection) % blockSize)
	if padLen < blockSize {
		for i := 1; i <= padLen; i++ {
			privSection = append(privSection, byte(i))
		}
	}

	var blob []byte
	blob = append(blob, []byte(opensshMagic)...)
	blob = append(blob, sshString([]byte("none"))...) // cipher
	blob = append(blob, sshString([]byte("none"))...) // kdf
	blob = append(blob, sshString(nil)...)            // kdf options
	blob = append(blob, u32(1)...)                    // number of keys
	blob = append(blob, sshString(pubBlob)...)
	blob = append(blob, sshString(privSection)...)

	b64 := base64.StdEncoding.EncodeToString(blob)
	var sb strings.Builder
	sb.WriteString("-----BEGIN OPENSSH PRIVATE KEY-----\n")
	for i := 0; i < len(b64); i += 70 {
		end := i + 70
		if end > len(b64) {
			end = len(b64)
		}
		sb.WriteString(b64[i:end])
		sb.WriteByte('\n')
	}
	sb.WriteString("-----END OPENSSH PRIVATE KEY-----\n")
	return sb.String()
}

func sshString(data []byte) []byte {
	out := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(out[:4], uint32(len(data)))
	copy(out[4:], data)
	return out
}

func u32(n uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, n)
	return b
}

// EnsureKeyPair loads the keypair from disk if both files exist, otherwise
// generates a new pair and writes it. Mirrors src/ssh/keygen.ts:ensureSSHKeyPair.
//
// File modes: parent dir 0700; private 0600; public 0644.
func EnsureKeyPair(keyPath, comment string) (*KeyPair, error) {
	pubPath := keyPath + ".pub"
	if fileExists(keyPath) && fileExists(pubPath) {
		priv, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		pub, err := os.ReadFile(pubPath)
		if err != nil {
			return nil, err
		}
		return &KeyPair{
			PrivateKey: string(priv),
			PublicKey:  strings.TrimRight(string(pub), "\n\r "),
		}, nil
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	pair, err := GenerateKeyPair(comment)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(pair.PrivateKey), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(pubPath, []byte(pair.PublicKey+"\n"), 0o644); err != nil {
		return nil, err
	}
	return pair, nil
}

// KeyPairExists returns true only if both private and public key files exist.
func KeyPairExists(keyPath string) bool {
	return fileExists(keyPath) && fileExists(keyPath+".pub")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
