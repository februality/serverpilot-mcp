package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// currentEnv reads the entry's "env" object at jsonPath+".env". Returns nil
// when the file, entry, or env block is absent. Refuses on malformed JSON.
func (p jsonPatcher) currentEnv() (map[string]string, error) {
	original, err := os.ReadFile(p.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(original)) == 0 {
		return nil, nil
	}
	if !gjson.ValidBytes(original) {
		return nil, fmt.Errorf("%w: %s", ErrMalformedConfig, p.configPath)
	}
	envNode := gjson.GetBytes(original, p.jsonPath+".env")
	if !envNode.Exists() || !envNode.IsObject() {
		return nil, nil
	}
	out := map[string]string{}
	envNode.ForEach(func(k, v gjson.Result) bool {
		out[k.String()] = v.String()
		return true
	})
	return out, nil
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

// entries enumerates all "serverpilot"-style entries under the parent of
// jsonPath (i.e. "mcpServers" or "servers"). Returns the account suffixes
// (LegacyAccount "" for the bare "serverpilot" entry).
func (p jsonPatcher) entries() ([]string, error) {
	parent, _ := parentPath(p.jsonPath)
	b, err := os.ReadFile(p.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	if !gjson.ValidBytes(b) {
		return nil, fmt.Errorf("%w: %s", ErrMalformedConfig, p.configPath)
	}
	node := gjson.GetBytes(b, parent)
	if !node.Exists() || !node.IsObject() {
		return nil, nil
	}
	out := []string{}
	node.ForEach(func(k, _ gjson.Result) bool {
		if acct, ok := parseEntryName(k.String()); ok {
			out = append(out, acct)
		}
		return true
	})
	sortAccounts(out)
	return out, nil
}

// parentPath returns everything before the last "." in a sjson dot-path.
// "mcpServers.serverpilot" → "mcpServers". Paths without a "." return "".
func parentPath(jsonPath string) (string, string) {
	i := strings.LastIndex(jsonPath, ".")
	if i < 0 {
		return "", jsonPath
	}
	return jsonPath[:i], jsonPath[i+1:]
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
