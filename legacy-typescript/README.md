# Legacy TypeScript implementation

This directory contains the original TypeScript implementation of `serverpilot-mcp`. It is preserved for reference but is **no longer maintained**. New work happens in the Go layout at the repository root.

If you need to run this version locally:

```bash
cd legacy-typescript
npm install
npm run build
node build/index.js
```

Behavior, tool names, and JSON output shapes match the Go rewrite — the Go version was ported with byte-for-byte parity in mind. If you find a divergence, the Go version is the canonical implementation.

For the current installer, see the top-level [README](../README.md).
