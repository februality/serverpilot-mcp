package spapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestServersAPI_List_Cached(t *testing.T) {
	hits := 0
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"data":[{"id":"srv_1","name":"web1","lastaddress":"1.2.3.4"}]}`))
	})
	api := NewServersAPI(c, NewTTLCache(60))
	for i := 0; i < 3; i++ {
		got, err := api.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "srv_1" {
			t.Fatalf("got %+v", got)
		}
	}
	if hits != 1 {
		t.Fatalf("expected 1 HTTP hit, got %d", hits)
	}
}

func TestServersAPI_Resolve_ByID(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/servers/srv_1" {
			_, _ = w.Write([]byte(`{"data":{"id":"srv_1","name":"web1"}}`))
			return
		}
		w.WriteHeader(404)
	})
	api := NewServersAPI(c, NewTTLCache(60))
	got, err := api.Resolve("srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "srv_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestServersAPI_Resolve_ByName_CaseInsensitive(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/servers" {
			_, _ = w.Write([]byte(`{"data":[{"id":"srv_1","name":"Web1"}]}`))
			return
		}
		w.WriteHeader(404)
	})
	api := NewServersAPI(c, NewTTLCache(60))
	got, err := api.Resolve("web1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "srv_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestServersAPI_List_DecodesNumericLastConn(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"srv_1","name":"web1","lastaddress":"1.2.3.4","lastconn":1714987234,"datecreated":1700000000,"plan":"1gb","available_runtimes":["php8.3"]}]}`))
	})
	api := NewServersAPI(c, NewTTLCache(60))
	got, err := api.List()
	if err != nil {
		t.Fatalf("decode failed (the ServerPilot API returns lastconn as a number): %v", err)
	}
	if len(got) != 1 || got[0].LastConn != 1714987234 {
		t.Fatalf("LastConn = %d, want 1714987234", got[0].LastConn)
	}
}

func TestServersAPI_Resolve_NotFound(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/servers" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.WriteHeader(404)
	})
	api := NewServersAPI(c, NewTTLCache(60))
	_, err := api.Resolve("nope")
	if err == nil || !strings.Contains(err.Error(), "Server not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}
