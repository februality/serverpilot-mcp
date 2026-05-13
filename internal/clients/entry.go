package clients

// stdioEntry is the canonical "command + args" map inserted into JSON
// configs. Most MCP clients use the {command, args} schema; VS Code adds a
// "type" field. Codex uses the same shape but in TOML.
//
// Env is omitted on the wire when nil/empty so installs without read-only
// mode produce byte-identical output to the pre-env-support format.
type stdioEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func newStdioEntry(binaryPath string, env map[string]string) stdioEntry {
	return stdioEntry{Command: binaryPath, Args: []string{"serve"}, Env: env}
}

// vscodeEntry is VS Code's variant: a `type` field is required.
type vscodeEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func newVSCodeEntry(binaryPath string, env map[string]string) vscodeEntry {
	return vscodeEntry{Type: "stdio", Command: binaryPath, Args: []string{"serve"}, Env: env}
}

// newTOMLEntry returns the Codex TOML shape. go-toml emits tables from a
// map[string]any, so we use that. Env is only included when non-empty.
func newTOMLEntry(binaryPath string, env map[string]string) map[string]any {
	out := map[string]any{
		"command": binaryPath,
		"args":    []string{"serve"},
	}
	if len(env) > 0 {
		envTable := make(map[string]any, len(env))
		for k, v := range env {
			envTable[k] = v
		}
		out["env"] = envTable
	}
	return out
}

// fileExists is small enough to live alongside the patchers.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := osStat(path)
	return err == nil
}
