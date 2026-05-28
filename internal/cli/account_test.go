package cli

import "testing"

func TestValidateAccount(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"", false},
		{"acme", false},
		{"a", false},
		{"acme-prod", false},
		{"acme-2", false},
		{"a-b-c-d", false},
		{"0123456789012345678901234567890", false}, // 31 chars (max)
		{"01234567890123456789012345678901", true}, // 32 chars (over)

		{"default", true},
		{"Acme", true},        // uppercase
		{"-acme", true},       // leading hyphen
		{"acme_prod", true},   // underscore
		{"acme.prod", true},   // dot
		{"acme prod", true},   // space
		{"acme/prod", true},   // slash
	}
	for _, c := range cases {
		err := validateAccount(c.name)
		if (err != nil) != c.wantErr {
			t.Errorf("validateAccount(%q) err=%v wantErr=%v", c.name, err, c.wantErr)
		}
	}
}
