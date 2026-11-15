package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPaginate_FitsMatchesPlainCompactJSON(t *testing.T) {
	items := []sysUserSummary{{ID: "a", Name: "x", ServerID: "s"}, {ID: "b", Name: "y", ServerID: "s"}}
	want, _ := json.Marshal(items)
	got, shown, err := paginate(items, 0, 100, pageBudgetBytes)
	if err != nil || shown != 2 || string(got) != string(want) {
		t.Fatalf("got %s (shown %d, err %v), want %s", got, shown, err, want)
	}
	empty, shown, err := paginate([]sysUserSummary{}, 0, 100, pageBudgetBytes)
	if err != nil || shown != 0 || string(empty) != "[]" {
		t.Fatalf("empty: %s %d %v", empty, shown, err)
	}
}

func TestPaginate_LimitBudgetOffset(t *testing.T) {
	items := make([]string, 50)
	for i := range items {
		items[i] = strings.Repeat("x", 98) // 100 bytes encoded
	}
	_, shown, _ := paginate(items, 0, 10, pageBudgetBytes)
	if shown != 10 {
		t.Errorf("limit: shown %d", shown)
	}
	out, shown, _ := paginate(items, 0, 100, 550)
	if shown != 5 || len(out) > 550 {
		t.Errorf("budget: shown %d len %d", shown, len(out))
	}
	var decoded []string
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Errorf("budget-cut page is not valid JSON: %v", err)
	}
	// An item bigger than the budget is still returned so paging progresses.
	if _, shown, _ = paginate(items, 0, 100, 10); shown != 1 {
		t.Errorf("oversized item: shown %d", shown)
	}
	if _, shown, _ = paginate(items, 45, 100, pageBudgetBytes); shown != 5 {
		t.Errorf("offset: shown %d", shown)
	}
	if _, _, err := paginate(items, 50, 10, pageBudgetBytes); err == nil {
		t.Error("offset == len should error")
	}
}

func TestPageNote(t *testing.T) {
	if n := pageNote("apps", 0, 5, 5, "x"); n != "" {
		t.Errorf("complete list should have no note: %q", n)
	}
	if n := pageNote("apps", 0, 100, 250, "narrow with query"); n != "Showing apps 1-100 of 250. Pass offset=100 (with the same filters) for the next page, or narrow with query." {
		t.Errorf("first page: %q", n)
	}
	if n := pageNote("apps", 200, 50, 250, ""); n != "Showing apps 201-250 of 250. This is the last page." {
		t.Errorf("last page: %q", n)
	}
}

func TestMatchesQuery(t *testing.T) {
	if !matchesQuery("", "anything") || !matchesQuery("BLOG", "myblog") || !matchesQuery("https://Shop.example.com/cart", "x", "shop.example.com") || matchesQuery("nope", "a", "b") {
		t.Error("matchesQuery misbehaves")
	}
}

func manyApps(n int) string {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"app_%d","name":"site%d","serverid":"s1","sysuserid":"su_%d","domains":["site%d.example.com","www.site%d.example.com"]}`, i, i, i, i, i)
	}
	b.WriteString(`]}`)
	return b.String()
}

func TestListApps_PagesLargeAccounts(t *testing.T) {
	apps := manyApps(250)
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(apps))
		case "/servers":
			_, _ = w.Write([]byte(`{"data":[{"id":"s1","name":"web1"}]}`))
		default:
			w.WriteHeader(404)
		}
	})

	res, _ := callRaw(t, handleListApps(d), nil)
	if len(res.Content) != 2 {
		t.Fatalf("want JSON + note, got %d blocks", len(res.Content))
	}
	var page []map[string]any
	text := callText(t, handleListApps(d), nil) // first text block = JSON array
	if err := json.Unmarshal([]byte(text), &page); err != nil || len(page) != defaultPageLimit {
		t.Fatalf("page len %d err %v", len(page), err)
	}
	if len(text) > 50_000 {
		t.Errorf("page is %d chars, over the 50,000-char spill threshold", len(text))
	}
	_, all := callRaw(t, handleListApps(d), nil)
	if !strings.Contains(all, "Showing apps 1-100 of 250. Pass offset=100") {
		t.Errorf("note: %s", all[len(all)-200:])
	}

	_, last := callRaw(t, handleListApps(d), map[string]any{"offset": float64(200)})
	if !strings.Contains(last, "Showing apps 201-250 of 250. This is the last page.") {
		t.Errorf("last page note missing")
	}

	_, one := callRaw(t, handleListApps(d), map[string]any{"query": "https://www.site42.example.com/"})
	if !strings.Contains(one, `"id":"app_42"`) || strings.Contains(one, "Showing") {
		t.Errorf("query: %s", one)
	}
}

func TestListApps_BudgetCapsHugePages(t *testing.T) {
	apps := manyApps(1000)
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps" {
			_, _ = w.Write([]byte(apps))
			return
		}
		w.WriteHeader(404)
	})
	text := callText(t, handleListApps(d), map[string]any{"limit": float64(1000)})
	if len(text) > pageBudgetBytes {
		t.Errorf("page is %d bytes, over budget %d", len(text), pageBudgetBytes)
	}
}

func TestSSHStatus_PagesBeforePerUserLookups(t *testing.T) {
	var users strings.Builder
	users.WriteString(`{"data":[`)
	for i := 0; i < 250; i++ {
		if i > 0 {
			users.WriteByte(',')
		}
		fmt.Fprintf(&users, `{"id":"su_%d","name":"u%d","serverid":"s1"}`, i, i)
	}
	users.WriteString(`]}`)
	var perUser atomic.Int32
	d, _ := newTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sshkeys":
			_, _ = w.Write([]byte(`{"data":[{"id":"key_1","name":"k"}]}`))
		case r.URL.Path == "/sysusers":
			_, _ = w.Write([]byte(users.String()))
		case strings.HasSuffix(r.URL.Path, "/sshkeys"):
			perUser.Add(1)
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"blog","sysuserid":"su_7","domains":["blog.com"]}]}`))
		case r.URL.Path == "/sysusers/su_7":
			_, _ = w.Write([]byte(`{"data":{"id":"su_7","name":"u7","serverid":"s1"}}`))
		default:
			w.WriteHeader(404)
		}
	})
	d.Cfg.SSHKeyName = "k"

	_, out := callRaw(t, handleSSHStatus(d), nil)
	if perUser.Load() != defaultPageLimit {
		t.Errorf("per-user lookups = %d, want %d", perUser.Load(), defaultPageLimit)
	}
	if !strings.Contains(out, "Showing system users 1-100 of 250. Pass offset=100") {
		t.Errorf("missing note")
	}

	perUser.Store(0)
	_, out = callRaw(t, handleSSHStatus(d), map[string]any{"site": "blog.com"})
	if perUser.Load() != 1 || !strings.Contains(out, `"id":"su_7"`) || strings.Contains(out, "Showing") {
		t.Errorf("site filter: lookups=%d out=%s", perUser.Load(), out)
	}
}
