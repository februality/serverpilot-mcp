package spapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const appsListJSON = `{"data":[
  {"id":"app_1","name":"BlogSite","serverid":"srv_1","sysuserid":"sys_1","runtime":"php8.3","domains":["example.com","www.example.com"],"autossl":true},
  {"id":"app_2","name":"shop","serverid":"srv_2","sysuserid":"sys_2","runtime":"php8.0","domains":["shop.example.com"]}
]}`

func TestAppsAPI_Resolve_ByName(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(appsListJSON))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	got, err := api.Resolve("blogsite") // case-insensitive
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "app_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestAppsAPI_Resolve_ByDomain(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(appsListJSON))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	got, err := api.Resolve("WWW.EXAMPLE.COM")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "app_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestAppsAPI_Resolve_NotFound(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	_, err := api.Resolve("missing")
	if err == nil || !strings.Contains(err.Error(), "App not found") {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestAppsAPI_UpdateRuntime_InvalidatesCache(t *testing.T) {
	listCalls := 0
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps" && r.Method == http.MethodGet:
			listCalls++
			_, _ = w.Write([]byte(appsListJSON))
		case r.URL.Path == "/apps/app_1" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"actionid":"act_1","data":{"id":"app_1","runtime":"php8.3"}}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	if _, err := api.UpdateRuntime("app_1", "php8.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	if listCalls != 2 {
		t.Fatalf("expected 2 list calls (cache invalidated after update), got %d", listCalls)
	}
}

func TestAppsAPI_UpdateDomains_Body(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/app_1" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"actionid":"act_2","data":{"id":"app_1"}}`))
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	res, err := api.UpdateDomains("app_1", []string{"a.com", "b.com"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ActionID != "act_2" {
		t.Fatalf("actionid = %q", res.ActionID)
	}
	domains, ok := gotBody["domains"].([]any)
	if !ok || len(domains) != 2 || domains[0] != "a.com" || domains[1] != "b.com" {
		t.Fatalf("body = %v", gotBody)
	}
	if _, ok := gotBody["runtime"]; ok {
		t.Fatalf("runtime field should not be sent: %v", gotBody)
	}
}

func TestAppsAPI_Create_BodyAndCacheInvalidation(t *testing.T) {
	var gotBody map[string]any
	listCalls := 0
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps" && r.Method == http.MethodGet:
			listCalls++
			_, _ = w.Write([]byte(appsListJSON))
		case r.URL.Path == "/apps" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{"actionid":"act_3","data":{"id":"app_new","name":"newapp","sysuserid":"sys_1","serverid":"srv_1","runtime":"php8.3","domains":["x.com"]}}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	res, err := api.Create(CreateAppRequest{
		Name:      "newapp",
		SysUserID: "sys_1",
		Runtime:   "php8.3",
		Domains:   []string{"x.com"},
		WordPress: &CreateAppWordPress{
			SiteTitle:     "Hello",
			AdminUser:     "admin",
			AdminPassword: "supersecret",
			AdminEmail:    "a@b.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ActionID != "act_3" || res.App.ID != "app_new" {
		t.Fatalf("result = %+v", res)
	}
	if gotBody["name"] != "newapp" || gotBody["sysuserid"] != "sys_1" || gotBody["runtime"] != "php8.3" {
		t.Fatalf("body = %v", gotBody)
	}
	wp, ok := gotBody["wordpress"].(map[string]any)
	if !ok || wp["admin_user"] != "admin" || wp["admin_password"] != "supersecret" {
		t.Fatalf("wordpress body = %v", gotBody["wordpress"])
	}
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	if listCalls != 2 {
		t.Fatalf("expected 2 list calls after Create invalidation, got %d", listCalls)
	}
}

func TestAppsAPI_Create_OmitsWordPressWhenNil(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"actionid":"act_4","data":{"id":"app_new"}}`))
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	if _, err := api.Create(CreateAppRequest{Name: "plain", SysUserID: "sys_1", Runtime: "php8.3"}); err != nil {
		t.Fatal(err)
	}
	if _, present := gotBody["wordpress"]; present {
		t.Fatalf("wordpress should be omitted: %v", gotBody)
	}
	if _, present := gotBody["domains"]; present {
		t.Fatalf("domains should be omitted when nil: %v", gotBody)
	}
}

func TestAppsAPI_SetSSL_AutoAndInvalidatesCache(t *testing.T) {
	var gotBody map[string]any
	listCalls := 0
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apps" && r.Method == http.MethodGet:
			listCalls++
			_, _ = w.Write([]byte(appsListJSON))
		case r.URL.Path == "/apps/app_1/ssl" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{"actionid":"act_5","data":{"auto":true,"force":false}}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	res, err := api.SetSSL("app_1", map[string]any{"auto": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.ActionID != "act_5" {
		t.Fatalf("actionid = %q", res.ActionID)
	}
	if gotBody["auto"] != true {
		t.Fatalf("body = %v", gotBody)
	}
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	if listCalls != 2 {
		t.Fatalf("expected 2 list calls after SetSSL invalidation, got %d", listCalls)
	}
}

func TestAppsAPI_RemoveSSL(t *testing.T) {
	gotMethod := ""
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/app_1/ssl" {
			w.WriteHeader(404)
			return
		}
		gotMethod = r.Method
		_, _ = w.Write([]byte(`{"actionid":"act_6","data":{}}`))
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	res, err := api.RemoveSSL("app_1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q", gotMethod)
	}
	if res.ActionID != "act_6" {
		t.Fatalf("actionid = %q", res.ActionID)
	}
}

func TestAppsAPI_ListByServer(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(appsListJSON))
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	got, err := api.ListByServer("srv_2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "app_2" {
		t.Fatalf("got %+v", got)
	}
}
