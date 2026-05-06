package wizard

import (
	"fmt"
	"io"

	"github.com/februality/serverpilot-mcp/internal/mcpserver"
)

// printBanner writes a brief welcome to w.
func printBanner(w io.Writer) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  ServerPilot MCP — Setup")
	_, _ = fmt.Fprintln(w, "  ───────────────────────")
	_, _ = fmt.Fprintf(w, "  v%s\n", mcpserver.ServerVersion)
	_, _ = fmt.Fprintln(w)
}
