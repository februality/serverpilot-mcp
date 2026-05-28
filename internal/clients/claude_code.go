package clients

type claudeCode struct{ p jsonPatcher }

func newClaudeCode(account string) Patcher {
	return claudeCode{p: jsonPatcher{
		configPath: homePath(".claude.json"),
		jsonPath:   "mcpServers." + EntryName(account),
	}}
}

func (c claudeCode) ID() string          { return "claude-code" }
func (c claudeCode) DisplayName() string { return "Claude Code" }

func (c claudeCode) Detect() (bool, string, error) {
	// Claude Code may be installed without ever creating ~/.claude.json
	// (first run creates it). We treat the path as detection: we can patch
	// it whether or not Claude Code has run yet — Claude Code reads it on
	// startup. So always "installed".
	return true, c.p.configPath, nil
}

func (c claudeCode) Patch(binaryPath string, env map[string]string, dryRun bool) (bool, string, error) {
	return c.p.patch(newStdioEntry(binaryPath, env), dryRun)
}

func (c claudeCode) Unpatch() (bool, error)                 { return c.p.unpatch() }
func (c claudeCode) CurrentEnv() (map[string]string, error) { return c.p.currentEnv() }
func (c claudeCode) Entries() ([]string, error)             { return c.p.entries() }
