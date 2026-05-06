// Package logging provides the project's slog handler. Output goes to
// stderr only — stdout is reserved for MCP JSON-RPC framing.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Init configures the default slog logger to write to stderr.
// stdout is reserved for the MCP JSON-RPC protocol channel — never log there.
func Init(level string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(h))
}
