package cli

import (
	"fmt"
	"regexp"
)

// accountRe restricts account names to a slug-like shape: starts with a
// lowercase letter or digit, then lowercase letters, digits, or hyphens.
// 1-31 chars total. Keeps entry names readable across JSON / TOML / shell.
var accountRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// validateAccount returns nil for the unnamed account (""), an error for
// the reserved "default" name, and an error for any name that fails the
// regex. Called from setup, install, uninstall, and accounts.
func validateAccount(name string) error {
	if name == "" {
		return nil
	}
	if name == "default" {
		return fmt.Errorf(`account name "default" is reserved (omit --account for the unnamed account)`)
	}
	if !accountRe.MatchString(name) {
		return fmt.Errorf("account name %q invalid: allowed lowercase a-z, digits 0-9, hyphen; must start with a letter or digit; 1-31 chars", name)
	}
	return nil
}
