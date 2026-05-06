package main

import (
	"fmt"
	"os"

	"github.com/februality/serverpilot-mcp/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
