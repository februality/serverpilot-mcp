package spapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":                        "example.com",
		"  Example.COM  ":                    "example.com",
		"https://www.example.com/wp-admin/":  "www.example.com",
		"http://example.com:8080":            "example.com",
		"example.com.":                       "example.com",
		"https://user@example.com/?a=1#frag": "example.com",
		"example.com/path/to/thing":          "example.com",
	}
	for in, want := range cases {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func appsServer(t *testing.T, list string, onOther func(r *http.Request)) *AppsAPI {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps" {
			_, _ = w.Write([]byte(list))
			return
		}
		if onOther != nil {
			onOther(r)
		}
		w.WriteHeader(404)
	})
	return NewAppsAPI(c, NewTTLCache(60))
}

func TestAppsResolve_NameLookupMakesNoPerIDRequest(t *testing.T) {
	api := appsServer(t, appsListJSON, func(r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	})
	if got, err := api.Resolve("shop"); err != nil || got.ID != "app_2" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAppsResolve_URLAndWWWFallback(t *testing.T) {
	api := appsServer(t, `{"data":[{"id":"app_1","name":"blog","domains":["blog.com"]},{"id":"app_2","name":"shop","domains":["www.shop.com"]}]}`, nil)
	for in, want := range map[string]string{
		"https://blog.com/wp-admin": "app_1",
		"www.blog.com":              "app_1", // only blog.com registered
		"shop.com":                  "app_2", // only www.shop.com registered
	} {
		got, err := api.Resolve(in)
		if err != nil || got.ID != want {
			t.Errorf("Resolve(%q) = %+v, %v; want %s", in, got, err, want)
		}
	}
}

func TestAppsResolve_ExactDomainBeatsWWWFallback(t *testing.T) {
	api := appsServer(t, `{"data":[{"id":"app_1","name":"a","domains":["example.com"]},{"id":"app_2","name":"b","domains":["www.example.com"]}]}`, nil)
	if got, err := api.Resolve("www.example.com"); err != nil || got.ID != "app_2" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAppsResolve_AmbiguousName(t *testing.T) {
	api := appsServer(t, `{"data":[{"id":"app_1","name":"wordpress","serverid":"s1"},{"id":"app_2","name":"wordpress","serverid":"s2"}]}`, nil)
	_, err := api.Resolve("wordpress")
	if err == nil || !strings.Contains(err.Error(), "app_1") || !strings.Contains(err.Error(), "app_2") {
		t.Fatalf("expected ambiguity error listing both IDs, got %v", err)
	}
}

func TestAppsResolve_IDNotInStaleListFallsBackToGet(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/apps/app_new":
			_, _ = w.Write([]byte(`{"data":{"id":"app_new","name":"fresh"}}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewAppsAPI(c, NewTTLCache(60))
	if got, err := api.Resolve("app_new"); err != nil || got.Name != "fresh" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAppsGet_EscapesPathSegment(t *testing.T) {
	var gotPath string
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(404)
	})
	_, _ = NewAppsAPI(c, NewTTLCache(60)).Get("x/../servers")
	if gotPath != "/apps/x%2F..%2Fservers" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestServersResolve_ByIP(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/servers" {
			_, _ = w.Write([]byte(`{"data":[{"id":"srv_1","name":"web1","lastaddress":"1.2.3.4"}]}`))
			return
		}
		w.WriteHeader(404)
	})
	api := NewServersAPI(c, NewTTLCache(60))
	for _, in := range []string{"1.2.3.4", "http://1.2.3.4/", "1.2.3.4:22", "ssh://web1:22"} {
		got, err := api.Resolve(in)
		if err != nil || got.ID != "srv_1" {
			t.Errorf("Resolve(%q) = %+v, %v", in, got, err)
		}
	}
}

func TestDatabasesResolve(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dbs" {
			_, _ = w.Write([]byte(`{"data":[{"id":"db_1","name":"blogdb","serverid":"s1"},{"id":"db_2","name":"wp","serverid":"s1"},{"id":"db_3","name":"wp","serverid":"s2"}]}`))
			return
		}
		w.WriteHeader(404)
	})
	api := NewDatabasesAPI(c, NewTTLCache(60))
	if got, err := api.Resolve("BlogDB"); err != nil || got.ID != "db_1" {
		t.Errorf("by name: %+v, %v", got, err)
	}
	if _, err := api.Resolve("wp"); err == nil || !strings.Contains(err.Error(), "db_3") {
		t.Errorf("expected ambiguity, got %v", err)
	}
	if _, err := api.Resolve("nope"); err == nil || !strings.Contains(err.Error(), "Database not found: nope") {
		t.Errorf("expected not-found, got %v", err)
	}
}

func TestSysUsersResolveAny(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sysusers" {
			_, _ = w.Write([]byte(`{"data":[{"id":"su_1","name":"serverpilot","serverid":"s1"},{"id":"su_2","name":"serverpilot","serverid":"s2"},{"id":"su_3","name":"alice","serverid":"s1"}]}`))
			return
		}
		w.WriteHeader(404)
	})
	api := NewSysUsersAPI(c, NewTTLCache(60))
	if got, err := api.ResolveAny("Alice"); err != nil || got.ID != "su_3" {
		t.Errorf("by name: %+v, %v", got, err)
	}
	if got, err := api.ResolveAny("su_2"); err != nil || got.ServerID != "s2" {
		t.Errorf("by id: %+v, %v", got, err)
	}
	if _, err := api.ResolveAny("serverpilot"); err == nil || !strings.Contains(err.Error(), "su_1") {
		t.Errorf("expected ambiguity, got %v", err)
	}
}
