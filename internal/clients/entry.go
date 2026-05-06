package clients

// stdioEntry is the canonical "command + args" map inserted into JSON
// configs. Most MCP clients use the {command, args} schema; VS Code adds a
// "type" field. Codex uses the same shape but in TOML.
type stdioEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func newStdioEntry(binaryPath string) stdioEntry {
	return stdioEntry{Command: binaryPath, Args: []string{"serve"}}
}

// vscodeEntry is VS Code's variant: a `type` field is required.
type vscodeEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func newVSCodeEntry(binaryPath string) vscodeEntry {
	return vscodeEntry{Type: "stdio", Command: binaryPath, Args: []string{"serve"}}
}

// tomlEntry is the Codex TOML shape — same fields, but go-toml emits TOML
// tables from a map[string]any, so we use that.
func newTOMLEntry(binaryPath string) map[string]any {
	return map[string]any{
		"command": binaryPath,
		"args":    []string{"serve"},
	}
}

// fileExists is small enough to live alongside the patchers.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := osStat(path)
	return err == nil
}
