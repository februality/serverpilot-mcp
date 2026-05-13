package clients

import "path/filepath"

type cursor struct{ p jsonPatcher }

func newCursor() Patcher {
	return cursor{p: jsonPatcher{
		configPath: homePath(".cursor", "mcp.json"),
		jsonPath:   "mcpServers." + ServerKey,
	}}
}

func (c cursor) ID() string          { return "cursor" }
func (c cursor) DisplayName() string { return "Cursor" }

func (c cursor) Detect() (bool, string, error) {
	parent := filepath.Dir(c.p.configPath)
	return fileExists(parent) || fileExists(c.p.configPath), c.p.configPath, nil
}

func (c cursor) Patch(binaryPath string, env map[string]string, dryRun bool) (bool, string, error) {
	return c.p.patch(newStdioEntry(binaryPath, env), dryRun)
}

func (c cursor) Unpatch() (bool, error)                 { return c.p.unpatch() }
func (c cursor) CurrentEnv() (map[string]string, error) { return c.p.currentEnv() }
