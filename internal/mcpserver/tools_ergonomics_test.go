package mcpserver

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func callRaw(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	var texts []string
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	return res, strings.Join(texts, "\n")
}

func TestJSONOutputIsCompact(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"srv_1","name":"web1"}]}`))
	})
	out := callText(t, handleListServers(d), nil)
	if strings.Contains(out, "\n") || strings.Contains(out, "  ") {
		t.Errorf("expected compact JSON, got %q", out)
	}
}

func TestGetAction_WaitsUntilDone(t *testing.T) {
	orig := actionPollInterval
	actionPollInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { actionPollInterval = orig })

	var calls atomic.Int32
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"data":{"id":"act_1","status":"open"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"act_1","status":"success"}}`))
	})
	out := callText(t, handleGetAction(d), map[string]any{"action_id": "act_1", "wait_seconds": float64(10)})
	if !strings.Contains(out, `"status":"success"`) {
		t.Errorf("got %s", out)
	}
	if calls.Load() != 3 {
		t.Errorf("API calls = %d, want 3", calls.Load())
	}
}

func TestGetAction_NoWaitByDefault(t *testing.T) {
	var calls atomic.Int32
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":{"id":"act_1","status":"open"}}`))
	})
	out := callText(t, handleGetAction(d), map[string]any{"action_id": "act_1"})
	if !strings.Contains(out, `"status":"open"`) || calls.Load() != 1 {
		t.Errorf("calls=%d out=%s", calls.Load(), out)
	}
}

func TestDeleteDatabase_AcceptsName(t *testing.T) {
	var deleted string
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dbs" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"db_1","name":"appdb","user":{"id":"u1","name":"appuser"},"appid":"a1","serverid":"s1"}]}`))
		case r.URL.Path == "/dbs/db_1" && r.Method == http.MethodDelete:
			deleted = "db_1"
			_, _ = w.Write([]byte(`{"actionid":"act_7","data":{}}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleDeleteDatabase(d), map[string]any{"database": "AppDB"})
	if deleted != "db_1" || !strings.Contains(out, "act_7") {
		t.Errorf("deleted=%q out=%s", deleted, out)
	}
}

func TestDeleteDatabase_AmbiguousNameRefuses(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Error("must not delete on an ambiguous name")
		}
		if r.URL.Path == "/dbs" {
			_, _ = w.Write([]byte(`{"data":[{"id":"db_1","name":"wp","serverid":"s1"},{"id":"db_2","name":"wp","serverid":"s2"}]}`))
			return
		}
		w.WriteHeader(404)
	})
	res, out := callRaw(t, handleDeleteDatabase(d), map[string]any{"database": "wp"})
	if !res.IsError || !strings.Contains(out, "db_1") || !strings.Contains(out, "db_2") {
		t.Errorf("expected ambiguity error listing both IDs, got %s", out)
	}
}

func TestUpdateAppDomains_ReportsPrevious(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"blog","domains":["a.com","www.a.com"]}]}`))
		case r.URL.Path == "/apps/app_1" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"actionid":"act_3","data":{}}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleUpdateAppDomains(d), map[string]any{"app": "blog", "domains": []any{"b.com"}})
	want := `Domains for "blog" updated to [b.com] (previously [a.com, www.a.com]). Action ID: act_3`
	if out != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

func TestGetApp_IncludesPaths(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"blog","sysuserid":"su_1","serverid":"s1","domains":["blog.example.com"]}]}`))
		case "/apps/app_1":
			_, _ = w.Write([]byte(`{"data":{"id":"app_1","name":"blog","sysuserid":"su_1","serverid":"s1"}}`))
		case "/sysusers/su_1":
			_, _ = w.Write([]byte(`{"data":{"id":"su_1","name":"alice","serverid":"s1"}}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleGetApp(d), map[string]any{"app": "https://Blog.Example.com/wp-admin/"})
	for _, want := range []string{`"public":"/srv/users/alice/apps/blog/public"`, `"logs":"/srv/users/alice/log/blog"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
}

func TestListApps_IncludesServerNameAndSysUser(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"blog","sysuserid":"su_1","serverid":"s1"}]}`))
		case "/servers":
			_, _ = w.Write([]byte(`{"data":[{"id":"s1","name":"web1"}]}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleListApps(d), nil)
	if !strings.Contains(out, `"server":"web1"`) || !strings.Contains(out, `"sysuserid":"su_1"`) {
		t.Errorf("got %s", out)
	}
}

func TestSiteExec_RejectsSecondsAsTimeout(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call: %s", r.URL.Path)
	})
	res, out := callRaw(t, handleSiteExec(d), map[string]any{"site": "x", "command": "true", "timeout": float64(300)})
	if !res.IsError || !strings.Contains(out, "milliseconds") || !strings.Contains(out, "300000") {
		t.Errorf("got %s", out)
	}
}

func TestSiteReadFile_RejectsTailWithOffset(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call: %s", r.URL.Path)
	})
	res, _ := callRaw(t, handleSiteReadFile(d), map[string]any{"site": "x", "path": "a", "tail": float64(10), "offset": float64(5)})
	if !res.IsError {
		t.Error("expected error")
	}
}

func TestSSHSetup_AmbiguousUserNameFailsBeforeAnyChange(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sysusers" {
			_, _ = w.Write([]byte(`{"data":[{"id":"su_1","name":"serverpilot","serverid":"s1"},{"id":"su_2","name":"serverpilot","serverid":"s2"}]}`))
			return
		}
		t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
	})
	d.Cfg.SSHKeyPath = t.TempDir() + "/key"
	res, out := callRaw(t, handleSSHSetup(d), map[string]any{"users": []any{"serverpilot"}})
	if !res.IsError || !strings.Contains(out, "su_1") || !strings.Contains(out, "su_2") {
		t.Errorf("got %s", out)
	}
}

func TestSSHSetup_SiteScopesToOwner(t *testing.T) {
	var assigned []string
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"blog","sysuserid":"su_1","domains":["blog.com"]}]}`))
		case r.URL.Path == "/sysusers/su_1":
			_, _ = w.Write([]byte(`{"data":{"id":"su_1","name":"alice","serverid":"s1"}}`))
		case r.URL.Path == "/sshkeys" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"key_1","name":"test-key"}]}`))
		case r.URL.Path == "/sysusers/su_1/sshkeys" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/sysusers/su_1/sshkeys" && r.Method == http.MethodPost:
			assigned = append(assigned, "su_1")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
		}
	})
	d.Cfg.SSHKeyPath = t.TempDir() + "/key"
	d.Cfg.SSHKeyName = "test-key"
	out := callText(t, handleSSHSetup(d), map[string]any{"site": "https://blog.com"})
	if len(assigned) != 1 || !strings.HasPrefix(out, "SSH Bootstrap Complete") {
		t.Errorf("assigned=%v out=%s", assigned, out)
	}
}

func TestSetAppSSL_RejectsAutoFalse(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
	})
	res, out := callRaw(t, handleSetAppSSL(d), map[string]any{"app": "x", "auto": false})
	if !res.IsError || !strings.Contains(out, "sp_remove_app_ssl") {
		t.Errorf("got %s", out)
	}
}

func usersFixture(t *testing.T, deleted *[]string) *Deps {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sshkeys" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"key_1","name":"k"}]}`))
		case r.URL.Path == "/sysusers":
			_, _ = w.Write([]byte(`{"data":[{"id":"su_1","name":"alice","serverid":"s1"},{"id":"su_2","name":"bob","serverid":"s1"},{"id":"su_3","name":"carol","serverid":"s2"}]}`))
		case strings.HasSuffix(r.URL.Path, "/sshkeys") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"key_1","name":"k"}]}`))
		case strings.Contains(r.URL.Path, "/sshkeys/") && r.Method == http.MethodDelete:
			*deleted = append(*deleted, strings.Split(r.URL.Path, "/")[2])
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	})
	d.Cfg.SSHKeyName = "k"
	return d
}

func TestSSHRemove_UsersArgNeverWidensToEveryone(t *testing.T) {
	for name, users := range map[string]any{
		"single string":       "alice",
		"json-encoded string": `["alice"]`,
	} {
		var deleted []string
		d := usersFixture(t, &deleted)
		res, out := callRaw(t, handleSSHRemove(d), map[string]any{"users": users})
		if res.IsError || len(deleted) != 1 || deleted[0] != "su_1" {
			t.Errorf("%s: deleted=%v out=%s", name, deleted, out)
		}
	}
	for name, users := range map[string]any{
		"empty array":      []any{},
		"non-string items": []any{float64(1)},
		"empty string":     "",
		"object":           map[string]any{"a": "b"},
	} {
		var deleted []string
		d := usersFixture(t, &deleted)
		res, _ := callRaw(t, handleSSHRemove(d), map[string]any{"users": users})
		if !res.IsError || len(deleted) != 0 {
			t.Errorf("%s: must be rejected without touching anyone; deleted=%v", name, deleted)
		}
	}
	// Omitting users (and site) is the only way to mean everyone.
	var deleted []string
	d := usersFixture(t, &deleted)
	if res, out := callRaw(t, handleSSHRemove(d), nil); res.IsError || len(deleted) != 3 {
		t.Errorf("omitted users: deleted=%v out=%s", deleted, out)
	}
}

func TestSSHRemove_ReportsPartialFailure(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sshkeys":
			_, _ = w.Write([]byte(`{"data":[{"id":"key_1","name":"k"}]}`))
		case r.URL.Path == "/sysusers":
			_, _ = w.Write([]byte(`{"data":[{"id":"su_1","name":"alice"},{"id":"su_2","name":"bob"}]}`))
		case strings.HasSuffix(r.URL.Path, "/sshkeys") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"key_1","name":"k"}]}`))
		case r.URL.Path == "/sysusers/su_1/sshkeys/key_1":
			w.WriteHeader(500)
		case r.URL.Path == "/sysusers/su_2/sshkeys/key_1":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	})
	d.Cfg.SSHKeyName = "k"
	res, out := callRaw(t, handleSSHRemove(d), nil)
	if res.IsError || !strings.Contains(out, "Removed from: bob") || !strings.Contains(out, "Failed: alice") {
		t.Errorf("got %s", out)
	}
}

func TestListDatabases_CombinesAppAndServer(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"a1","name":"blog"}]}`))
		case "/apps/a1":
			_, _ = w.Write([]byte(`{"data":{"id":"a1","name":"blog"}}`))
		case "/servers":
			_, _ = w.Write([]byte(`{"data":[{"id":"s2","name":"web2"}]}`))
		case "/dbs":
			_, _ = w.Write([]byte(`{"data":[{"id":"db1","name":"x","appid":"a1","serverid":"s1"},{"id":"db2","name":"y","appid":"a1","serverid":"s2"}]}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleListDatabases(d), map[string]any{"app": "blog", "server": "web2"})
	if strings.Contains(out, `"db1"`) || !strings.Contains(out, `"db2"`) {
		t.Errorf("got %s", out)
	}
}

func TestCleanDomains(t *testing.T) {
	got := cleanDomains([]string{"https://Example.com/", "example.com", " www.example.com ", "", "*.example.com"})
	want := []string{"example.com", "www.example.com", "*.example.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v", got)
	}
	if cleanDomains(nil) != nil {
		t.Error("nil must stay nil")
	}
}

func TestDBPassword_RejectsSurroundingWhitespace(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call: %s", r.URL.Path)
	})
	res, out := callRaw(t, handleUpdateDBPassword(d), map[string]any{"database": "db_1", "password": " supersecret"})
	if !res.IsError || !strings.Contains(out, "whitespace") {
		t.Errorf("got %s", out)
	}
}
