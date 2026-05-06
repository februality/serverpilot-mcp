package spapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer spins up an httptest server and returns a Client wired to it.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClientWithBaseURL("cid", "key", srv.URL)
	return c, srv
}

func TestClient_BasicAuthHeader(t *testing.T) {
	var gotAuth string
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	var resp spListResponse[SPServer]
	if err := c.Get("/servers", &resp); err != nil {
		t.Fatal(err)
	}
	// Basic base64("cid:key") = "Y2lkOmtleQ=="
	if gotAuth != "Basic Y2lkOmtleQ==" {
		t.Fatalf("auth header = %q", gotAuth)
	}
}

func TestClient_ErrorResponse(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`unauthorized`))
	})
	err := c.Get("/servers", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Status != 401 || !strings.Contains(apiErr.Body, "unauthorized") {
		t.Fatalf("unexpected error: %v", apiErr)
	}
}

func TestClient_EmptyResponse(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	if err := c.Delete("/sshkeys/abc", nil); err != nil {
		t.Fatal(err)
	}
}

func TestClient_PostBody(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"actionid":"act_1"}`))
	})
	var resp UpdateRuntimeResult
	if err := c.Post("/apps/x", map[string]any{"runtime": "php8.3"}, &resp); err != nil {
		t.Fatal(err)
	}
	if gotBody["runtime"] != "php8.3" {
		t.Fatalf("body = %v", gotBody)
	}
	if resp.ActionID != "act_1" {
		t.Fatalf("actionid = %q", resp.ActionID)
	}
}
