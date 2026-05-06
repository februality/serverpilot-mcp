package clients

import (
	"os"
	"runtime"
	"testing"

	"github.com/tidwall/gjson"
)

// withTempHome redirects HOME (and USERPROFILE on Windows) to a temp dir
// so patcher constructors compute paths under it.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
		t.Setenv("APPDATA", dir+"\\AppData\\Roaming")
	}
	t.Setenv("XDG_CONFIG_HOME", dir+"/.config")
	return dir
}

// TestPatchers_SmokeRoundTrip exercises every patcher: patch → re-patch
// (no-op) → unpatch → file no longer contains our entry.
func TestPatchers_SmokeRoundTrip(t *testing.T) {
	for _, p := range All() {
		t.Run(p.ID(), func(t *testing.T) {
			withTempHome(t)
			// Re-construct after HOME was overridden.
			p := ByID(p.ID())

			changed, _, err := p.Patch(binPath, false)
			if err != nil {
				t.Fatalf("Patch err: %v", err)
			}
			if !changed {
				t.Fatal("expected first patch to change")
			}
			// Idempotent: second patch is a no-op.
			changed, _, err = p.Patch(binPath, false)
			if err != nil {
				t.Fatalf("re-Patch err: %v", err)
			}
			if changed {
				t.Error("expected re-patch to be no-op")
			}
			// Unpatch removes our entry.
			changed, err = p.Unpatch()
			if err != nil {
				t.Fatalf("Unpatch err: %v", err)
			}
			if !changed {
				t.Error("expected unpatch to change")
			}
		})
	}
}

func TestVSCode_UsesServersKey(t *testing.T) {
	withTempHome(t)
	p := ByID("vscode")
	if _, _, err := p.Patch(binPath, false); err != nil {
		t.Fatal(err)
	}
	_, path, _ := p.Detect()
	b, _ := os.ReadFile(path)
	if !gjson.GetBytes(b, "servers.serverpilot.type").Exists() {
		t.Error("VS Code entry should be under `servers` with a `type` field")
	}
	if gjson.GetBytes(b, "servers.serverpilot.type").String() != "stdio" {
		t.Error("VS Code entry should have type=stdio")
	}
	if gjson.GetBytes(b, "mcpServers").Exists() {
		t.Error("VS Code entry should NOT use `mcpServers`")
	}
}

func TestRegistry_IDsAndByID(t *testing.T) {
	want := []string{"claude-code", "claude-desktop", "cursor", "windsurf", "vscode", "codex"}
	got := IDs()
	if len(got) != len(want) {
		t.Fatalf("IDs() = %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("IDs()[%d] = %q, want %q", i, got[i], id)
		}
		if ByID(id) == nil {
			t.Errorf("ByID(%q) returned nil", id)
		}
	}
	if ByID("nonexistent") != nil {
		t.Error("ByID('nonexistent') should return nil")
	}
}
