package mcpserver

import (
	"sort"
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
	}

	t.Run("default mode registers all 22 tools", func(t *testing.T) {
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

	t.Run("read-only mode hides all twelve write tools", func(t *testing.T) {
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
