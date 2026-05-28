package spapi

import (
	"net/http"
	"strings"
	"testing"
)

const sysusersListJSON = `{"data":[
  {"id":"sys_1","name":"alice","serverid":"srv_1"},
  {"id":"sys_2","name":"bob","serverid":"srv_2"},
  {"id":"sys_3","name":"alice","serverid":"srv_2"}
]}`

func TestSysUsersAPI_Resolve_ByName(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sysusers":
			_, _ = w.Write([]byte(sysusersListJSON))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewSysUsersAPI(c, NewTTLCache(60))
	got, err := api.Resolve("ALICE", "srv_2") // case-insensitive, server-scoped
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "sys_3" {
		t.Fatalf("got %+v", got)
	}
}

func TestSysUsersAPI_Resolve_ByID(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sysusers/sys_1":
			_, _ = w.Write([]byte(`{"data":{"id":"sys_1","name":"alice","serverid":"srv_1"}}`))
		case "/sysusers":
			_, _ = w.Write([]byte(sysusersListJSON))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewSysUsersAPI(c, NewTTLCache(60))
	got, err := api.Resolve("sys_1", "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "sys_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestSysUsersAPI_Resolve_IDOnDifferentServerFallsToName(t *testing.T) {
	// sys_1 exists on srv_1; if caller asks for it on srv_2, Resolve must not
	// return it — it should fall through to a name lookup within srv_2 and fail.
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sysusers/sys_1":
			_, _ = w.Write([]byte(`{"data":{"id":"sys_1","name":"alice","serverid":"srv_1"}}`))
		case "/sysusers":
			_, _ = w.Write([]byte(`{"data":[{"id":"sys_2","name":"bob","serverid":"srv_2"}]}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewSysUsersAPI(c, NewTTLCache(60))
	_, err := api.Resolve("sys_1", "srv_2")
	if err == nil || !strings.Contains(err.Error(), "System user not found on server") {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestSysUsersAPI_Resolve_NotFound(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sysusers":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(404)
		}
	})
	api := NewSysUsersAPI(c, NewTTLCache(60))
	_, err := api.Resolve("ghost", "srv_1")
	if err == nil || !strings.Contains(err.Error(), "System user not found on server") {
		t.Fatalf("expected not-found, got %v", err)
	}
}
