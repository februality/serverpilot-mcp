package config

import "testing"

func TestIsTrueEnv(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"  ", false},
		{"anything", false},

		{"1", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"  1  ", true},
	}
	for _, c := range cases {
		if got := IsTrueEnv(c.in); got != c.want {
			t.Errorf("IsTrueEnv(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLoad_ReadOnlyFromEnv(t *testing.T) {
	// Load() needs credentials; skip if not present in this environment.
	t.Setenv("SERVERPILOT_CLIENT_ID", "cid_test")
	t.Setenv("SERVERPILOT_API_KEY", "key_test")

	t.Setenv("SP_READ_ONLY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReadOnly {
		t.Error("ReadOnly should default to false when SP_READ_ONLY is unset")
	}

	t.Setenv("SP_READ_ONLY", "1")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ReadOnly {
		t.Error("ReadOnly should be true when SP_READ_ONLY=1")
	}

	t.Setenv("SP_READ_ONLY", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReadOnly {
		t.Error("ReadOnly should be false when SP_READ_ONLY=false")
	}
}
