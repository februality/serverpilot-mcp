// Package clients patches MCP-client config files (Claude Code, Claude
// Desktop, Cursor, Windsurf, VS Code, Codex CLI) to add or remove the
// serverpilot MCP server entry. All writes are atomic (write-tmp + rename)
// and refuse to clobber malformed configs.
package clients

const ServerKey = "serverpilot"

// Patcher is the per-MCP-client interface. Each patcher owns one client's
// config file and knows its format/schema.
type Patcher interface {
	ID() string
	DisplayName() string
	// Detect reports whether the client is plausibly installed and returns
	// the path it would patch.
	Detect() (installed bool, configPath string, err error)
	// Patch inserts/updates the serverpilot entry. env is written under the
	// entry's "env" block (nil/empty for no env). Returns whether the file
	// changed and a unified-style diff. dryRun does not write.
	Patch(binaryPath string, env map[string]string, dryRun bool) (changed bool, diff string, err error)
	// Unpatch removes the serverpilot entry, leaving everything else alone.
	Unpatch() (changed bool, err error)
	// CurrentEnv reads the existing config and returns the env block on the
	// serverpilot entry, or nil if the file/entry doesn't exist. Used by
	// `status` to surface read-only mode without re-patching.
	CurrentEnv() (map[string]string, error)
}

// All returns the full registry of patchers in a stable order. The order
// determines presentation in the wizard's multi-select.
func All() []Patcher {
	return []Patcher{
		newClaudeCode(),
		newClaudeDesktop(),
		newCursor(),
		newWindsurf(),
		newVSCode(),
		newCodex(),
	}
}

// ByID returns a single patcher or nil. Used by `install --client X`.
func ByID(id string) Patcher {
	for _, p := range All() {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// IDs returns the list of valid --client values.
func IDs() []string {
	all := All()
	out := make([]string, len(all))
	for i, p := range all {
		out[i] = p.ID()
	}
	return out
}
