package ssh

import "strings"

// ShellEscape wraps a string in single quotes, escaping any embedded single
// quotes. Mirrors src/ssh/operations.ts:shellEscape — used to safely insert
// arbitrary user input into a shell command line.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
