package clients

import "path/filepath"

type claudeDesktop struct{ p jsonPatcher }

func newClaudeDesktop(account string) Patcher {
	return claudeDesktop{p: jsonPatcher{
		configPath: claudeDesktopConfigPath(),
		jsonPath:   "mcpServers." + EntryName(account),
	}}
}

func (c claudeDesktop) ID() string          { return "claude-desktop" }
func (c claudeDesktop) DisplayName() string { return "Claude Desktop" }

func (c claudeDesktop) Detect() (bool, string, error) {
	// Detection: parent dir exists (Claude Desktop's app-data dir is
	// created on first launch).
	parent := filepath.Dir(c.p.configPath)
	return fileExists(parent) || fileExists(c.p.configPath), c.p.configPath, nil
}

func (c claudeDesktop) Patch(binaryPath string, env map[string]string, dryRun bool) (bool, string, error) {
	return c.p.patch(newStdioEntry(binaryPath, env), dryRun)
}

func (c claudeDesktop) Unpatch() (bool, error)                 { return c.p.unpatch() }
func (c claudeDesktop) CurrentEnv() (map[string]string, error) { return c.p.currentEnv() }
func (c claudeDesktop) Entries() ([]string, error)             { return c.p.entries() }
