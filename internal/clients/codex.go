package clients

import "path/filepath"

// Codex CLI uses ~/.codex/config.toml with [mcp_servers.<name>] tables.
type codex struct{ p tomlPatcher }

func newCodex() Patcher {
	return codex{p: tomlPatcher{
		configPath: homePath(".codex", "config.toml"),
		tablePath:  []string{"mcp_servers", ServerKey},
	}}
}

func (c codex) ID() string          { return "codex" }
func (c codex) DisplayName() string { return "Codex CLI" }

func (c codex) Detect() (bool, string, error) {
	parent := filepath.Dir(c.p.configPath)
	return fileExists(parent) || fileExists(c.p.configPath), c.p.configPath, nil
}

func (c codex) Patch(binaryPath string, dryRun bool) (bool, string, error) {
	return c.p.patch(newTOMLEntry(binaryPath), dryRun)
}

func (c codex) Unpatch() (bool, error) { return c.p.unpatch() }
