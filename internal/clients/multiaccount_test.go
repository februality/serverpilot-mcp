package clients

import (
	"os"
	"sort"
	"testing"

	"github.com/tidwall/gjson"
)

// TestPatcher_NamedAccountCoexistsWithLegacy: a named patcher writes
// "serverpilot-acme" alongside an existing unnamed "serverpilot" entry
// without disturbing it.
func TestPatcher_NamedAccountCoexistsWithLegacy(t *testing.T) {
	withTempHome(t)

	legacy := ByID("claude-code", "")
	if _, _, err := legacy.Patch(binPath, nil, false); err != nil {
		t.Fatalf("legacy patch: %v", err)
	}
	_, path, _ := legacy.Detect()
	beforeLegacy, _ := os.ReadFile(path)

	acme := ByID("claude-code", "acme")
	if _, _, err := acme.Patch(binPath, map[string]string{"SP_ACCOUNT": "acme"}, false); err != nil {
		t.Fatalf("acme patch: %v", err)
	}

	after, _ := os.ReadFile(path)
	if !gjson.GetBytes(after, "mcpServers.serverpilot").Exists() {
		t.Fatal("legacy entry lost after acme patch")
	}
	if !gjson.GetBytes(after, "mcpServers.serverpilot-acme").Exists() {
		t.Fatal("acme entry not added")
	}
	// Legacy entry's bytes should be intact.
	legacyBefore := gjson.GetBytes(beforeLegacy, "mcpServers.serverpilot").String()
	legacyAfter := gjson.GetBytes(after, "mcpServers.serverpilot").String()
	if legacyBefore != legacyAfter {
		t.Errorf("legacy entry mutated:\nbefore: %s\nafter:  %s", legacyBefore, legacyAfter)
	}
	if got := gjson.GetBytes(after, "mcpServers.serverpilot-acme.env.SP_ACCOUNT").String(); got != "acme" {
		t.Errorf("SP_ACCOUNT env = %q, want \"acme\"", got)
	}
}

// TestPatcher_UnpatchAccountLeavesLegacy: unpatching the named account
// removes only its entry; the legacy entry survives.
func TestPatcher_UnpatchAccountLeavesLegacy(t *testing.T) {
	withTempHome(t)

	legacy := ByID("claude-code", "")
	if _, _, err := legacy.Patch(binPath, nil, false); err != nil {
		t.Fatal(err)
	}
	acme := ByID("claude-code", "acme")
	if _, _, err := acme.Patch(binPath, nil, false); err != nil {
		t.Fatal(err)
	}

	changed, err := acme.Unpatch()
	if err != nil || !changed {
		t.Fatalf("unpatch: changed=%v err=%v", changed, err)
	}
	_, path, _ := legacy.Detect()
	b, _ := os.ReadFile(path)
	if gjson.GetBytes(b, "mcpServers.serverpilot-acme").Exists() {
		t.Error("acme entry not removed")
	}
	if !gjson.GetBytes(b, "mcpServers.serverpilot").Exists() {
		t.Error("legacy entry collateral-damaged")
	}
}

// TestPatcher_EntriesDiscoversAll: Entries() reports every serverpilot*
// entry in the file regardless of which account the patcher is bound to.
func TestPatcher_EntriesDiscoversAll(t *testing.T) {
	withTempHome(t)

	for _, acct := range []string{"", "acme", "globex"} {
		p := ByID("claude-code", acct)
		if _, _, err := p.Patch(binPath, nil, false); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ByID("claude-code", "").Entries()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	want := []string{"", "acme", "globex"}
	if len(entries) != len(want) {
		t.Fatalf("Entries() = %v, want %v", entries, want)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("Entries()[%d] = %q, want %q", i, entries[i], want[i])
		}
	}
}

// TestPatcher_EntriesIgnoresUnrelated: only serverpilot[*] keys count;
// arbitrary other MCP server entries don't pollute the result.
func TestPatcher_EntriesIgnoresUnrelated(t *testing.T) {
	withTempHome(t)

	p := ByID("claude-code", "")
	if _, _, err := p.Patch(binPath, nil, false); err != nil {
		t.Fatal(err)
	}
	_, path, _ := p.Detect()
	// Inject an unrelated entry.
	existing, _ := os.ReadFile(path)
	merged := []byte(`{"mcpServers":{"serverpilot":` +
		gjson.GetBytes(existing, "mcpServers.serverpilot").Raw +
		`,"github":{"command":"/usr/bin/gh","args":["serve"]}}}`)
	if err := os.WriteFile(path, merged, 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := p.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "" {
		t.Errorf("Entries() = %v, want only [\"\"] (github should not match)", entries)
	}
}

// TestPatcher_TOMLEntries verifies the TOML enumerator works the same way.
func TestPatcher_TOMLEntries(t *testing.T) {
	withTempHome(t)
	for _, acct := range []string{"", "staging"} {
		p := ByID("codex", acct)
		if _, _, err := p.Patch(binPath, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ByID("codex", "").Entries()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	if len(entries) != 2 || entries[0] != "" || entries[1] != "staging" {
		t.Errorf("codex Entries() = %v, want [\"\" \"staging\"]", entries)
	}
}

func TestEntryName(t *testing.T) {
	if got := EntryName(""); got != "serverpilot" {
		t.Errorf("EntryName(\"\") = %q, want \"serverpilot\"", got)
	}
	if got := EntryName("acme"); got != "serverpilot-acme" {
		t.Errorf("EntryName(\"acme\") = %q, want \"serverpilot-acme\"", got)
	}
}

func TestParseEntryName(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		ok    bool
	}{
		{"serverpilot", "", true},
		{"serverpilot-acme", "acme", true},
		{"serverpilot-multi-word", "multi-word", true},
		{"github", "", false},
		{"serverpilot-", "", false},
		{"serverpilot-UPPER", "", false},
	}
	for _, c := range cases {
		got, ok := parseEntryName(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseEntryName(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
