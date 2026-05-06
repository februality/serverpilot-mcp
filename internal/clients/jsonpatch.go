package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ErrMalformedConfig is returned when an existing config file is not valid
// JSON. Patchers refuse to write rather than risk data loss.
var ErrMalformedConfig = errors.New("existing config is malformed; refusing to patch")

// jsonPatcher patches a JSON config at a specific dot-path with a struct value.
type jsonPatcher struct {
	configPath string
	jsonPath   string // sjson path, e.g. "mcpServers.serverpilot"
}

// patch reads, parses, sets, and atomically writes the config. Returns
// (changed, diff). Refuses on malformed JSON. Strips no other content.
func (p jsonPatcher) patch(entry any, dryRun bool) (bool, string, error) {
	original, err := readOrEmpty(p.configPath)
	if err != nil {
		return false, "", err
	}

	if len(bytes.TrimSpace(original)) > 0 && !gjson.ValidBytes(original) {
		return false, "", fmt.Errorf("%w: %s", ErrMalformedConfig, p.configPath)
	}

	var base []byte
	if len(bytes.TrimSpace(original)) == 0 {
		base = []byte("{}")
	} else {
		base = original
	}

	updated, err := sjson.SetBytes(base, p.jsonPath, entry)
	if err != nil {
		return false, "", fmt.Errorf("set %s: %w", p.jsonPath, err)
	}

	// Pretty-print so the file remains human-friendly.
	pretty, err := indentJSON(updated)
	if err != nil {
		return false, "", err
	}

	originalNorm := original
	if len(bytes.TrimSpace(originalNorm)) == 0 {
		originalNorm = nil
	} else {
		// Normalize through json.Indent so a no-op patch detects "unchanged"
		// even when the user file used different indentation.
		originalNorm, _ = indentJSON(originalNorm)
	}
	if bytes.Equal(originalNorm, pretty) {
		return false, "", nil
	}
	if dryRun {
		return true, simpleDiff(originalNorm, pretty), nil
	}
	if err := atomicWrite(p.configPath, pretty, 0o600); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// unpatch removes the serverpilot entry. Returns (changed, error). If the
// file or the path is absent, returns (false, nil).
func (p jsonPatcher) unpatch() (bool, error) {
	original, err := os.ReadFile(p.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !gjson.ValidBytes(original) {
		return false, fmt.Errorf("%w: %s", ErrMalformedConfig, p.configPath)
	}
	if !gjson.GetBytes(original, p.jsonPath).Exists() {
		return false, nil
	}
	updated, err := sjson.DeleteBytes(original, p.jsonPath)
	if err != nil {
		return false, err
	}
	pretty, err := indentJSON(updated)
	if err != nil {
		return false, err
	}
	if err := atomicWrite(p.configPath, pretty, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func indentJSON(b []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func readOrEmpty(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

// atomicWrite writes data to path via a sibling .tmp file + rename. Creates
// parent directories as needed.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// simpleDiff is a minimal "before/after" renderer for --dry-run output.
// We don't ship a full diff library — line-level "removed/added" is enough
// for a config patch.
func simpleDiff(before, after []byte) string {
	var b bytes.Buffer
	b.WriteString("--- before\n+++ after\n")
	if len(before) == 0 {
		b.WriteString("(file did not exist)\n")
	} else {
		for _, line := range bytes.Split(bytes.TrimRight(before, "\n"), []byte("\n")) {
			b.WriteString("- ")
			b.Write(line)
			b.WriteByte('\n')
		}
	}
	for _, line := range bytes.Split(bytes.TrimRight(after, "\n"), []byte("\n")) {
		b.WriteString("+ ")
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}
