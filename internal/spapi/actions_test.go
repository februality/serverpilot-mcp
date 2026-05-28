package spapi

import (
	"net/http"
	"testing"
)

func TestActionsAPI_Get(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/actions/act_1" || r.Method != http.MethodGet {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"act_1","serverid":"srv_1","status":"success","datecreated":1700000000}}`))
	})
	api := NewActionsAPI(c)
	got, err := api.Get("act_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "act_1" || got.Status != "success" || got.ServerID != "srv_1" || got.DateCreated != 1700000000 {
		t.Fatalf("got %+v", got)
	}
}
