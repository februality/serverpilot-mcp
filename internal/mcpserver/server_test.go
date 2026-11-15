package mcpserver

import (
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/config"
)

func TestNew_ToolRegistration(t *testing.T) {
	readOnlyTools := []string{
		"sp_list_servers",
		"sp_get_server",
		"sp_list_apps",
		"sp_get_app",
		"sp_list_databases",
		"sp_list_sysusers",
		"sp_get_action",
		"sp_ssh_status",
		"site_read_file",
		"site_list_files",
		"site_download_file",
	}
	writeTools := []string{
		"sp_update_app_runtime",
		"sp_update_app_domains",
		"sp_set_app_ssl",
		"sp_remove_app_ssl",
		"sp_create_app",
		"sp_update_db_password",
		"sp_create_database",
		"sp_delete_database",
		"sp_ssh_setup",
		"sp_ssh_remove",
		"site_exec",
		"site_write_file",
		"site_edit_file",
		"site_upload_file",
	}

	t.Run("default mode registers all 25 tools", func(t *testing.T) {
		cfg := &config.Config{ClientID: "cid", APIKey: "key"}
		s := New(Build(cfg))
		got := registeredToolNames(s.ListTools())
		want := append([]string{}, readOnlyTools...)
		want = append(want, writeTools...)
		sort.Strings(want)
		if !equalStringSets(got, want) {
			t.Errorf("default tools = %v\nwant       = %v", got, want)
		}
	})

	t.Run("read-only mode hides all fourteen write tools", func(t *testing.T) {
		cfg := &config.Config{ClientID: "cid", APIKey: "key", ReadOnly: true}
		s := New(Build(cfg))
		got := registeredToolNames(s.ListTools())
		want := append([]string{}, readOnlyTools...)
		sort.Strings(want)
		if !equalStringSets(got, want) {
			t.Errorf("read-only tools = %v\nwant            = %v", got, want)
		}
		for _, name := range writeTools {
			if containsString(got, name) {
				t.Errorf("write tool %q was registered in read-only mode", name)
			}
		}
	})
}

func registeredToolNames(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// mcp-go defaults every tool to destructiveHint=true / readOnlyHint=false.
// Lock in that every tool overrides that with its real behavior.
func TestToolAnnotations(t *testing.T) {
	readTools := map[string]bool{
		"sp_list_servers": true, "sp_get_server": true, "sp_list_apps": true, "sp_get_app": true,
		"sp_list_databases": true, "sp_list_sysusers": true, "sp_get_action": true, "sp_ssh_status": true,
		"site_read_file": true, "site_list_files": true,
	}
	nonDestructive := map[string]bool{
		"sp_create_app": true, "sp_create_database": true, "sp_ssh_setup": true,
	}
	mustBeDestructive := []string{
		"sp_delete_database", "sp_remove_app_ssl", "sp_update_app_domains", "sp_update_db_password",
		"sp_ssh_remove", "site_exec", "site_write_file", "site_edit_file", "site_upload_file", "site_download_file",
	}

	s := New(Build(&config.Config{ClientID: "cid", APIKey: "key", TransferDir: "/tmp/x"}))
	tools := s.ListTools()
	for name, st := range tools {
		a := st.Tool.Annotations
		if a.Title == "" {
			t.Errorf("%s: missing title annotation", name)
		}
		if a.ReadOnlyHint == nil || a.DestructiveHint == nil || a.IdempotentHint == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: annotation hints must all be set", name)
			continue
		}
		if *a.ReadOnlyHint != readTools[name] {
			t.Errorf("%s: readOnlyHint = %v, want %v", name, *a.ReadOnlyHint, readTools[name])
		}
		wantDestructive := !readTools[name] && !nonDestructive[name]
		if *a.DestructiveHint != wantDestructive {
			t.Errorf("%s: destructiveHint = %v, want %v", name, *a.DestructiveHint, wantDestructive)
		}
		if *a.OpenWorldHint != (name == "site_exec") {
			t.Errorf("%s: openWorldHint = %v", name, *a.OpenWorldHint)
		}
	}
	for _, name := range mustBeDestructive {
		if st, ok := tools[name]; !ok || !*st.Tool.Annotations.DestructiveHint {
			t.Errorf("%s must be registered and marked destructive", name)
		}
	}
}

func TestInstructions(t *testing.T) {
	full := Instructions(&config.Config{Account: "acme", TransferDir: "/home/u/Downloads/serverpilot-mcp"})
	for _, want := range []string{`account "acme"`, "sp_get_action with wait_seconds", "sp_ssh_setup", "apps/<app>/public", "log/<app>/", "Lists are paged", "site_edit_file", "/home/u/Downloads/serverpilot-mcp"} {
		if !strings.Contains(full, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
	if strings.Contains(full, "READ-ONLY") {
		t.Error("read-only note shown in default mode")
	}

	ro := Instructions(&config.Config{ReadOnly: true})
	if !strings.Contains(ro, "READ-ONLY") {
		t.Error("read-only note missing")
	}
	for _, absent := range []string{"wait_seconds", "site_edit_file", "account", "site_exec", "site_write_file"} {
		if strings.Contains(ro, absent) {
			t.Errorf("read-only instructions mention %q", absent)
		}
	}
}

func TestReadOnlyDownloadIsAdditiveWithoutOverwrite(t *testing.T) {
	s := New(Build(&config.Config{ClientID: "cid", APIKey: "key", ReadOnly: true, TransferDir: "/tmp/x"}))
	tool := s.ListTools()["site_download_file"].Tool
	if *tool.Annotations.DestructiveHint {
		t.Error("read-only site_download_file must not be destructive")
	}
	if _, ok := tool.InputSchema.Properties["overwrite"]; ok {
		t.Error("read-only site_download_file must not offer overwrite")
	}
}
