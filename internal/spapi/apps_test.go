package spapi

import (
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
