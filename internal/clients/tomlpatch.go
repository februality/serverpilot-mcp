package clients

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml/v2"
)

// ErrMalformedTOML is returned when an existing TOML config file fails to parse.
var ErrMalformedTOML = errors.New("existing TOML config is malformed; refusing to patch")

// tomlPatcher patches a TOML config at a specific top-level table path.
//
// Caveat: pelletier/go-toml/v2 does not preserve comments or original
// formatting on round-trip. Documented in the README; users running setup
// will lose comments in ~/.codex/config.toml. Acceptable for v1.
type tomlPatcher struct {
	configPath string
	tablePath  []string // e.g. ["mcp_servers", "serverpilot"]
}

func (p tomlPatcher) patch(entry any, dryRun bool) (bool, string, error) {
	original, err := readOrEmpty(p.configPath)
	if err != nil {
		return false, "", err
	}

	root := map[string]any{}
	if len(bytes.TrimSpace(original)) > 0 {
		if err := toml.Unmarshal(original, &root); err != nil {
			return false, "", fmt.Errorf("%w: %s: %v", ErrMalformedTOML, p.configPath, err)
		}
	}

	setNested(root, p.tablePath, entry)

	updated, err := toml.Marshal(root)
	if err != nil {
		return false, "", err
	}

	if bytes.Equal(bytes.TrimSpace(original), bytes.TrimSpace(updated)) {
		return false, "", nil
	}
	if dryRun {
		return true, simpleDiff(original, updated), nil
	}
	if err := atomicWrite(p.configPath, updated, 0o600); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func (p tomlPatcher) unpatch() (bool, error) {
	original, err := os.ReadFile(p.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	root := map[string]any{}
	if len(bytes.TrimSpace(original)) > 0 {
		if err := toml.Unmarshal(original, &root); err != nil {
			return false, fmt.Errorf("%w: %s: %v", ErrMalformedTOML, p.configPath, err)
		}
	}
	if !deleteNested(root, p.tablePath) {
		return false, nil
	}
	updated, err := toml.Marshal(root)
	if err != nil {
		return false, err
	}
	return true, atomicWrite(p.configPath, updated, 0o600)
}

func setNested(root map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	cur := root
	for _, key := range path[:len(path)-1] {
		next, ok := cur[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[key] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = value
}

func deleteNested(root map[string]any, path []string) bool {
	if len(path) == 0 {
		return false
	}
	cur := root
	for _, key := range path[:len(path)-1] {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	last := path[len(path)-1]
	if _, ok := cur[last]; !ok {
		return false
	}
	delete(cur, last)
	return true
}
