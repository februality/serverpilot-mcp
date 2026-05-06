package clients

import "path/filepath"

type windsurf struct{ p jsonPatcher }

func newWindsurf() Patcher {
	return windsurf{p: jsonPatcher{
		configPath: homePath(".codeium", "windsurf", "mcp_config.json"),
		jsonPath:   "mcpServers." + ServerKey,
	}}
}

func (w windsurf) ID() string          { return "windsurf" }
func (w windsurf) DisplayName() string { return "Windsurf" }

func (w windsurf) Detect() (bool, string, error) {
	parent := filepath.Dir(w.p.configPath)
	return fileExists(parent) || fileExists(w.p.configPath), w.p.configPath, nil
}

func (w windsurf) Patch(binaryPath string, dryRun bool) (bool, string, error) {
	return w.p.patch(newStdioEntry(binaryPath), dryRun)
}

func (w windsurf) Unpatch() (bool, error) { return w.p.unpatch() }
