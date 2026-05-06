package clients

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const binPath = "/usr/local/bin/serverpilot-mcp"

func newPatcherFor(t *testing.T) (jsonPatcher, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	return jsonPatcher{configPath: path, jsonPath: "mcpServers." + ServerKey}, path
}

func TestJSONPatch_FileNotExists(t *testing.T) {
	p, path := newPatcherFor(t)
	changed, _, err := p.patch(newStdioEntry(binPath), false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	b, _ := os.ReadFile(path)
	if !gjson.ValidBytes(b) {
		t.Fatalf("output not JSON: %s", b)
	}
	if got := gjson.GetBytes(b, "mcpServers.serverpilot.command").String(); got != binPath {
		t.Errorf("command = %q", got)
	}
}

func TestJSONPatch_PreservesOtherServers(t *testing.T) {
	p, path := newPatcherFor(t)
	existing := `{
  "mcpServers": {
    "github": {"command": "/usr/bin/gh-mcp", "args": ["serve"]},
    "other-tool": {"command": "/x"}
  },
  "telemetry": {"enabled": false}
}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _, err := p.patch(newStdioEntry(binPath), false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	b, _ := os.ReadFile(path)
	v := gjson.GetBytes(b, "mcpServers.github.command").String()
	if v != "/usr/bin/gh-mcp" {
		t.Errorf("github server lost: %q", v)
	}
	if !gjson.GetBytes(b, "telemetry.enabled").Exists() {
		t.Error("telemetry section lost")
	}
	if gjson.GetBytes(b, "mcpServers.serverpilot.command").String() != binPath {
		t.Error("our entry not added")
	}
}

func TestJSONPatch_NoChangeIfIdentical(t *testing.T) {
	p, path := newPatcherFor(t)
	// First write.
	if _, _, err := p.patch(newStdioEntry(binPath), false); err != nil {
		t.Fatal(err)
	}
	stat1, _ := os.Stat(path)
	// Second write — should be no-op.
	changed, _, err := p.patch(newStdioEntry(binPath), false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected no change on identical re-patch")
	}
	stat2, _ := os.Stat(path)
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Error("file rewritten despite no change")
	}
}

func TestJSONPatch_UpdatesStaleEntry(t *testing.T) {
	p, path := newPatcherFor(t)
	existing := `{"mcpServers":{"serverpilot":{"command":"/old/path","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _, err := p.patch(newStdioEntry(binPath), false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	b, _ := os.ReadFile(path)
	if got := gjson.GetBytes(b, "mcpServers.serverpilot.command").String(); got != binPath {
		t.Errorf("command not updated: %q", got)
	}
}

func TestJSONPatch_RefusesMalformed(t *testing.T) {
	p, path := newPatcherFor(t)
	if err := os.WriteFile(path, []byte("not json {{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := p.patch(newStdioEntry(binPath), false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMalformedConfig) {
		t.Errorf("wrong error: %v", err)
	}
	// File must be untouched.
	b, _ := os.ReadFile(path)
	if string(b) != "not json {{{" {
		t.Error("malformed file was modified")
	}
}

func TestJSONPatch_DryRunDoesNotWrite(t *testing.T) {
	p, path := newPatcherFor(t)
	changed, diff, err := p.patch(newStdioEntry(binPath), true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change in diff")
	}
	if !strings.Contains(diff, "+") {
		t.Errorf("expected diff with additions, got: %q", diff)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dry-run wrote file")
	}
}

func TestJSONPatch_Unpatch(t *testing.T) {
	p, path := newPatcherFor(t)
	if _, _, err := p.patch(newStdioEntry(binPath), false); err != nil {
		t.Fatal(err)
	}
	// Add another server so we can verify it survives.
	b, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["mcpServers"].(map[string]any)["other"] = map[string]any{"command": "/x"}
	out, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(path, out, 0o600)

	changed, err := p.unpatch()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	final, _ := os.ReadFile(path)
	if gjson.GetBytes(final, "mcpServers.serverpilot").Exists() {
		t.Error("our entry not removed")
	}
	if !gjson.GetBytes(final, "mcpServers.other").Exists() {
		t.Error("other server removed")
	}
}

func TestJSONPatch_UnpatchMissing(t *testing.T) {
	p, _ := newPatcherFor(t)
	changed, err := p.unpatch()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected no change for missing file")
	}
}
