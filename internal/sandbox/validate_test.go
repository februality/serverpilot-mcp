package sandbox

import (
	"strings"
	"testing"
)

const base = "/srv/users/alice"

func TestValidatePath_Allowed(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"apps/myapp", "/srv/users/alice/apps/myapp"},
		{"apps/myapp/public/index.php", "/srv/users/alice/apps/myapp/public/index.php"},
		{"/srv/users/alice/apps/myapp", "/srv/users/alice/apps/myapp"},
		{"/srv/users/alice", "/srv/users/alice"},
		{"./apps/myapp", "/srv/users/alice/apps/myapp"},
		{"apps//myapp", "/srv/users/alice/apps/myapp"},
		{"apps/foo/../myapp", "/srv/users/alice/apps/myapp"},
	}
	for _, c := range cases {
		got, err := ValidatePath(base, c.in)
		if err != nil {
			t.Errorf("ValidatePath(%q) unexpected err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ValidatePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidatePath_Rejected(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/srv/users/bob",
		"../bob",
		"apps/../../bob",
		"/srv/users/alice/../bob",
		"../../../etc/passwd",
		"apps/myapp/../../../../etc/passwd",
	}
	for _, in := range cases {
		got, err := ValidatePath(base, in)
		if err == nil {
			t.Errorf("ValidatePath(%q) = %q, want error", in, got)
		}
	}
}

func TestValidatePath_BasePathBoundary(t *testing.T) {
	// "/srv/users/alice2" should be rejected — must not be a prefix match
	// against "/srv/users/alice".
	if _, err := ValidatePath(base, "/srv/users/alice2"); err == nil {
		t.Error("expected /srv/users/alice2 to be rejected")
	}
}

func FuzzValidatePath(f *testing.F) {
	seeds := []string{
		"apps/myapp",
		"../escape",
		"/etc/passwd",
		"./.././..",
		"\x00null",
		strings.Repeat("../", 100),
		"apps/" + strings.Repeat("a", 1000),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := ValidatePath(base, in)
		if err != nil {
			return
		}
		if !strings.HasPrefix(got, base+"/") && got != base {
			t.Fatalf("ValidatePath(%q) = %q escaped base %q", in, got, base)
		}
	})
}
