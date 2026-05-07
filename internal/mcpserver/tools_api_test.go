package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/spapi"
)

func newTestDeps(t *testing.T, handler http.HandlerFunc) (*Deps, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := &config.Config{ClientID: "cid", APIKey: "key", CacheTTLSeconds: 60}
	cache := spapi.NewTTLCache(cfg.CacheTTLSeconds)
	client := spapi.NewClientWithBaseURL(cfg.ClientID, cfg.APIKey, srv.URL)
	return &Deps{
		Cfg:       cfg,
		Cache:     cache,
		Client:    client,
		Servers:   spapi.NewServersAPI(client, cache),
		Apps:      spapi.NewAppsAPI(client, cache),
		SysUsers:  spapi.NewSysUsersAPI(client, cache),
		Databases: spapi.NewDatabasesAPI(client, cache),
	}, srv
}

func callText(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) string {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res.Content)
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatalf("no text content in result: %+v", res.Content)
	return ""
}

func TestListServers_Shape(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
		  {"id":"srv_1","name":"web1","lastaddress":"1.2.3.4","plan":"1gb",
		   "available_runtimes":["php8.0","php8.3"],"firewall":true,"autoupdates":false}
		]}`))
	})
	out := callText(t, handleListServers(d), nil)
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	want := map[string]any{
		"id":          "srv_1",
		"name":        "web1",
		"ip":          "1.2.3.4",
		"plan":        "1gb",
		"runtimes":    []any{"php8.0", "php8.3"},
		"firewall":    true,
		"autoupdates": false,
	}
	for k, v := range want {
		if !jsonEq(got[0][k], v) {
			t.Errorf("field %s: got %v, want %v", k, got[0][k], v)
		}
	}
}

func TestListApps_SSLLabel(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
		  {"id":"a1","name":"auto","autossl":true,"ssl":null,"serverid":"s1","domains":["a.com"]},
		  {"id":"a2","name":"custom","autossl":false,"ssl":{"key":"k","cert":"c","cacerts":null,"auto":false,"force":false},"serverid":"s1","domains":["b.com"]},
		  {"id":"a3","name":"none","autossl":false,"ssl":null,"serverid":"s1","domains":["c.com"]}
		]}`))
	})
	out := callText(t, handleListApps(d), nil)
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"auto", "custom", "none"}
	for i, w := range want {
		if got[i]["ssl"] != w {
			t.Errorf("apps[%d].ssl = %v, want %s", i, got[i]["ssl"], w)
		}
	}
}

func TestUpdateAppRuntime_TextOutput(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"BlogSite","autossl":false,"ssl":null,"domains":["x.com"],"serverid":"s1","sysuserid":"u1"}]}`))
		case r.URL.Path == "/servers/s1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{"id":"s1","name":"web1","available_runtimes":["php8.0","php8.3"]}}`))
		case r.URL.Path == "/apps/app_1" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"actionid":"act_42","data":{}}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleUpdateAppRuntime(d), map[string]any{"app": "BlogSite", "runtime": "php8.3"})
	want := `PHP runtime for "BlogSite" updated to php8.3. Action ID: act_42`
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestUpdateAppRuntime_RejectsUnavailableRuntime(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"BlogSite","autossl":false,"ssl":null,"domains":["x.com"],"serverid":"s1","sysuserid":"u1"}]}`))
		case r.URL.Path == "/servers/s1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{"id":"s1","name":"web1","available_runtimes":["php8.0","php8.3"]}}`))
		case r.URL.Path == "/apps/app_1" && r.Method == http.MethodPost:
			t.Fatalf("UpdateRuntime should not have been called for an invalid runtime")
		default:
			w.WriteHeader(404)
		}
	})
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"app": "BlogSite", "runtime": "php9.9"}
	res, err := handleUpdateAppRuntime(d)(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected error result for unavailable runtime")
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			if !strings.Contains(tc.Text, "not available") || !strings.Contains(tc.Text, "php8.0, php8.3") {
				t.Errorf("unexpected error message: %s", tc.Text)
			}
		}
	}
}

func TestUpdateDBPassword_TextOutput(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dbs/db_1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{"id":"db_1","name":"appdb","user":{"id":"u1","name":"appuser"},"appid":"a1","serverid":"s1"}}`))
		case r.URL.Path == "/dbs/db_1" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"actionid":"act_99","data":{}}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleUpdateDBPassword(d), map[string]any{"database": "db_1", "password": "supersecret"})
	want := `Password updated for database user "appuser" on database "appdb". Action ID: act_99`
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestUpdateDBPassword_RejectsShortPassword(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
	})
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"database": "db_1", "password": "short"}
	res, err := handleUpdateDBPassword(d)(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected error result")
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			if !strings.Contains(tc.Text, "8 characters") {
				t.Errorf("unexpected error message: %s", tc.Text)
			}
		}
	}
}

func TestGetApp_RedactsSecrets(t *testing.T) {
	const adminPassword = "wp-admin-supersecret-123"
	const sslKeyMarker = "SENSITIVEKEYMATERIAL"
	const sslKey = `-----BEGIN PRIVATE KEY-----\n` + sslKeyMarker + `\n-----END PRIVATE KEY-----`
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps/app_1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{
			  "id":"app_1","name":"BlogSite","sysuserid":"u1","serverid":"s1","runtime":"php8.3",
			  "autossl":false,"domains":["x.com"],"datecreated":1700000000,
			  "ssl":{"key":"` + sslKey + `","cert":"CERTBODY","cacerts":null,"auto":false,"force":true},
			  "wordpress":{"site_title":"My Blog","admin_user":"admin","admin_password":"` + adminPassword + `","admin_email":"a@x.com","login_url":"https://x.com/wp-admin"}
			}}`))
		default:
			w.WriteHeader(404)
		}
	})
	out := callText(t, handleGetApp(d), map[string]any{"app": "app_1"})
	if strings.Contains(out, adminPassword) {
		t.Errorf("output leaked WordPress admin password:\n%s", out)
	}
	if strings.Contains(out, sslKeyMarker) {
		t.Errorf("output leaked SSL private key:\n%s", out)
	}
	if strings.Contains(out, "CERTBODY") {
		t.Errorf("output leaked SSL cert body:\n%s", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got["ssl"] != "custom" {
		t.Errorf("ssl label = %v, want \"custom\"", got["ssl"])
	}
	wp, ok := got["wordpress"].(map[string]any)
	if !ok {
		t.Fatalf("wordpress not present or wrong shape: %v", got["wordpress"])
	}
	if wp["admin_user"] != "admin" {
		t.Errorf("admin_user = %v", wp["admin_user"])
	}
	if _, hasPw := wp["admin_password"]; hasPw {
		t.Errorf("admin_password key should not be present in output")
	}
}

func TestListSysUsers_Shape(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"u1","name":"alice","serverid":"s1"}]}`))
	})
	out := callText(t, handleListSysUsers(d), nil)
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got[0]["serverId"] != "s1" {
		t.Errorf("serverId = %v", got[0]["serverId"])
	}
}

func TestListDatabases_Shape(t *testing.T) {
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"db_1","name":"d1","appid":"a1","serverid":"s1","user":{"id":"u1","name":"appuser"}}]}`))
	})
	out := callText(t, handleListDatabases(d), nil)
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	row := got[0]
	if row["user"] != "appuser" || row["userId"] != "u1" || row["appId"] != "a1" || row["serverId"] != "s1" {
		t.Errorf("row = %+v", row)
	}
}

func jsonEq(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}
