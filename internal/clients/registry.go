// Package clients patches MCP-client config files (Claude Code, Claude
// Desktop, Cursor, Windsurf, VS Code, Codex CLI) to add or remove the
// serverpilot MCP server entry. All writes are atomic (write-tmp + rename)
// and refuse to clobber malformed configs.
//
// Multi-account: each patcher instance is bound to one account. The
// unnamed/legacy account ("") writes the entry as `serverpilot`; a named
// account "acme" writes `serverpilot-acme`. Both can coexist in the same
// config file because they target distinct keys.
package clients

import (
	"regexp"
	"sort"
)

// ServerKey is the entry name used for the legacy/unnamed account.
const ServerKey = "serverpilot"

// entryNameRe matches "serverpilot" or "serverpilot-<account>" where
// account follows the same regex enforced at the CLI layer.
var entryNameRe = regexp.MustCompile(`^serverpilot(?:-([a-z0-9][a-z0-9-]{0,30}))?$`)

// EntryName returns the config-file entry name for the given account.
// LegacyAccount ("") returns the bare ServerKey so existing single-account
// installs stay byte-identical.
func EntryName(account string) string {
	if account == "" {
		return ServerKey
	}
	return ServerKey + "-" + account
}

// parseEntryName returns (accountSuffix, ok) for a config entry name. The
// suffix is "" for the bare "serverpilot" entry and "<account>" for
// "serverpilot-<account>". Non-matching names return ok=false.
func parseEntryName(name string) (string, bool) {
	m := entryNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1], true
}

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
	// Entries returns the account suffixes of every "serverpilot" /
	// "serverpilot-<account>" entry currently present in this client's
	// config file. Legacy ("") sorts first when present. Used by
	// status/doctor/accounts to discover what's installed independent of
	// any specific account binding.
	Entries() ([]string, error)
}

// All returns the full registry of patchers bound to the given account, in
// a stable order. The order determines presentation in the wizard's
// multi-select. Pass "" for the legacy/unnamed account.
func All(account string) []Patcher {
	return []Patcher{
		newClaudeCode(account),
		newClaudeDesktop(account),
		newCursor(account),
		newWindsurf(account),
		newVSCode(account),
		newCodex(account),
	}
}

// ByID returns a single patcher or nil. Used by `install --client X`.
func ByID(id, account string) Patcher {
	for _, p := range All(account) {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// IDs returns the list of valid --client values.
func IDs() []string {
	all := All("")
	out := make([]string, len(all))
	for i, p := range all {
		out[i] = p.ID()
	}
	return out
}

// sortAccounts sorts the slice in-place with LegacyAccount ("") first.
// Used by Entries() implementations so callers see a stable order.
func sortAccounts(s []string) {
	sort.Strings(s) // "" sorts first naturally
}
