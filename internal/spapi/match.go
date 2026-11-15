package spapi

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeDomain turns whatever a model passes as a "domain" into a bare,
// lowercase host: "https://WWW.Example.com:443/wp-admin/" → "www.example.com".
// Strips scheme, userinfo, path/query/fragment, port, and a trailing dot.
func NormalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[:i], ":") {
		s = s[:i]
	}
	return strings.TrimSuffix(s, ".")
}

// pathSegment escapes a caller-supplied identifier for use as a single URL
// path segment, so an identifier like "x/../y" can't address another endpoint.
func pathSegment(id string) string { return url.PathEscape(id) }

// ambiguousError lists every candidate so the model can retry with an ID
// instead of guessing.
func ambiguousError(kind, ident string, candidates []string) error {
	return fmt.Errorf("Multiple %ss match %q: %s. Pass the ID instead.",
		kind, ident, strings.Join(candidates, "; "))
}
