package clients

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func newTOMLPatcherFor(t *testing.T) (tomlPatcher, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	return tomlPatcher{configPath: path, tablePath: []string{"mcp_servers", ServerKey}}, path
}

func TestTOMLPatch_FileNotExists(t *testing.T) {
	p, path := newTOMLPatcherFor(t)
	changed, _, err := p.patch(newTOMLEntry(binPath, nil), false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	b, _ := os.ReadFile(path)
	root := map[string]any{}
	if err := toml.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	srv := root["mcp_servers"].(map[string]any)["serverpilot"].(map[string]any)
	if srv["command"] != binPath {
		t.Errorf("command = %v", srv["command"])
	}
}

func TestTOMLPatch_PreservesOtherTables(t *testing.T) {
	p, path := newTOMLPatcherFor(t)
	existing := `model = "claude-sonnet-4-6"

[mcp_servers.github]
command = "/usr/bin/gh"
args = ["serve"]
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.patch(newTOMLEntry(binPath, nil), false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	root := map[string]any{}
	if err := toml.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	if root["model"] != "claude-sonnet-4-6" {
		t.Error("top-level key lost")
	}
	servers := root["mcp_servers"].(map[string]any)
	if _, ok := servers["github"]; !ok {
		t.Error("github server lost")
	}
	if _, ok := servers["serverpilot"]; !ok {
		t.Error("our entry not added")
	}
}

func TestTOMLPatch_RefusesMalformed(t *testing.T) {
	p, path := newTOMLPatcherFor(t)
	if err := os.WriteFile(path, []byte("[unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := p.patch(newTOMLEntry(binPath, nil), false)
	if err == nil || !errors.Is(err, ErrMalformedTOML) {
		t.Fatalf("expected ErrMalformedTOML, got %v", err)
	}
}

func TestTOMLPatch_WritesEnvSubTable(t *testing.T) {
	p, path := newTOMLPatcherFor(t)
	env := map[string]string{"SP_READ_ONLY": "1"}
	if _, _, err := p.patch(newTOMLEntry(binPath, env), false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	root := map[string]any{}
	if err := toml.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	srv := root["mcp_servers"].(map[string]any)["serverpilot"].(map[string]any)
	envTable, ok := srv["env"].(map[string]any)
	if !ok {
		t.Fatalf("env sub-table missing: %v", srv)
	}
	if envTable["SP_READ_ONLY"] != "1" {
		t.Errorf("SP_READ_ONLY = %v, want \"1\"", envTable["SP_READ_ONLY"])
	}
}

func TestTOMLPatch_NilEnvOmitsBlock(t *testing.T) {
	p, path := newTOMLPatcherFor(t)
	if _, _, err := p.patch(newTOMLEntry(binPath, nil), false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "env") {
		t.Errorf("nil env should not emit env table. got: %s", b)
	}
}

func TestTOMLPatch_CurrentEnv(t *testing.T) {
	p, _ := newTOMLPatcherFor(t)
	if _, _, err := p.patch(newTOMLEntry(binPath, map[string]string{"SP_READ_ONLY": "1"}), false); err != nil {
		t.Fatal(err)
	}
	env, err := p.currentEnv()
	if err != nil {
		t.Fatal(err)
	}
	if env["SP_READ_ONLY"] != "1" {
		t.Errorf("currentEnv = %v, want SP_READ_ONLY=1", env)
	}
}

func TestTOMLPatch_Unpatch(t *testing.T) {
	p, path := newTOMLPatcherFor(t)
	existing := `[mcp_servers.serverpilot]
command = "/x"
args = ["serve"]

[mcp_servers.other]
command = "/y"
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := p.unpatch()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), `[mcp_servers.serverpilot]`) {
		t.Error("our table not removed")
	}
	if !strings.Contains(string(b), `[mcp_servers.other]`) {
		t.Error("other table removed")
	}
}
