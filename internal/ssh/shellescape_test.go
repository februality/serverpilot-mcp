package ssh

import "testing"

func TestShellEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{`hello`, `'hello'`},
		{`hello world`, `'hello world'`},
		{`it's fine`, `'it'\''s fine'`},
		{`/srv/users/alice/apps`, `'/srv/users/alice/apps'`},
		{``, `''`},
		{`$(rm -rf /)`, `'$(rm -rf /)'`},
	}
	for _, c := range cases {
		if got := ShellEscape(c.in); got != c.want {
			t.Errorf("ShellEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
