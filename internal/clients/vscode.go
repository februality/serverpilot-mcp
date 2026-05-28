package clients

import "path/filepath"

// VS Code's MCP support uses ~/.vscode/mcp.json with a `servers` (NOT
// `mcpServers`) top-level key. Each server entry requires a `type` field
// alongside command/args.
type vscode struct{ p jsonPatcher }

func newVSCode(account string) Patcher {
	return vscode{p: jsonPatcher{
		configPath: homePath(".vscode", "mcp.json"),
		jsonPath:   "servers." + EntryName(account),
	}}
}

func (v vscode) ID() string          { return "vscode" }
func (v vscode) DisplayName() string { return "VS Code" }

func (v vscode) Detect() (bool, string, error) {
	parent := filepath.Dir(v.p.configPath)
	return fileExists(parent) || fileExists(v.p.configPath), v.p.configPath, nil
}

func (v vscode) Patch(binaryPath string, env map[string]string, dryRun bool) (bool, string, error) {
	return v.p.patch(newVSCodeEntry(binaryPath, env), dryRun)
}

func (v vscode) Unpatch() (bool, error)                 { return v.p.unpatch() }
func (v vscode) CurrentEnv() (map[string]string, error) { return v.p.currentEnv() }
func (v vscode) Entries() ([]string, error)             { return v.p.entries() }
