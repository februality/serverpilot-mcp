package spapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDatabasesAPI_UpdatePassword_Body(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"actionid":"act_1","data":{}}`))
	})
	api := NewDatabasesAPI(c, NewTTLCache(60))
	if _, err := api.UpdatePassword("db_1", "dbu_1", "supersecret"); err != nil {
		t.Fatal(err)
	}
	user := gotBody["user"].(map[string]any)
	if user["id"] != "dbu_1" || user["password"] != "supersecret" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestDatabasesAPI_ListByApp(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
		  {"id":"db_1","name":"d1","appid":"app_1","serverid":"srv_1","user":{"id":"u1","name":"u1"}},
		  {"id":"db_2","name":"d2","appid":"app_2","serverid":"srv_1","user":{"id":"u2","name":"u2"}}
		]}`))
	})
	api := NewDatabasesAPI(c, NewTTLCache(60))
	got, err := api.ListByApp("app_2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "db_2" {
		t.Fatalf("got %+v", got)
	}
}

func TestDatabasesAPI_UpdatePassword_InvalidatesCache(t *testing.T) {
	listCalls := 0
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dbs" && r.Method == http.MethodGet:
			listCalls++
			_, _ = w.Write([]byte(`{"data":[{"id":"db_1","name":"d1","user":{"id":"u1","name":"u1"}}]}`))
		case r.URL.Path == "/dbs/db_1" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"actionid":"act_1","data":{}}`))
		}
	})
	api := NewDatabasesAPI(c, NewTTLCache(60))
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	if _, err := api.UpdatePassword("db_1", "u1", "newpass"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.List(); err != nil {
		t.Fatal(err)
	}
	if listCalls != 2 {
		t.Fatalf("expected 2 list calls, got %d", listCalls)
	}
}
