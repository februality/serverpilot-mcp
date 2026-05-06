# ServerPilot MCP

MCP server for managing ServerPilot-hosted sites via API and SSH. Single static Go binary that self-installs into Claude Code, Claude Desktop, Cursor, Windsurf, VS Code, and Codex CLI.

## Build & test

```bash
go build -o serverpilot-mcp ./cmd/serverpilot-mcp
go test ./...
go vet ./...
```

Requires Go 1.22+. No CGO.

## Local development

Test the freshly built binary against Claude Code without re-installing globally:

```bash
go build -o serverpilot-mcp ./cmd/serverpilot-mcp
cp .mcp.json.example .mcp.json   # registers ./serverpilot-mcp as `serverpilot-dev`
```

Open this directory in Claude Code — the project-scoped `.mcp.json` is picked up automatically. `.env.example` documents the env-var path if you want to bypass the keychain during development.

## CLI surface

```
serverpilot-mcp                # default: serve if !TTY, else help
serverpilot-mcp serve          # MCP stdio server (what MCP clients invoke)
serverpilot-mcp setup          # interactive wizard
serverpilot-mcp install        # non-interactive client config patcher
serverpilot-mcp uninstall      # reverse setup
serverpilot-mcp status [--json]
serverpilot-mcp doctor
serverpilot-mcp version
```

The TTY heuristic in `internal/cli/root.go`: explicit subcommand wins; no args + stdin&stdout TTY → help; no args + pipe → `serve`. This prevents MCP clients from accidentally launching the wizard.

## Architecture

```
cmd/
  serverpilot-mcp/main.go    # cobra entry point
internal/
  cli/                       # cobra subcommands — one file per command (see CLI surface above)
  config/                    # env-var + creds resolution
  creds/                     # OS keychain (99designs/keyring) + 0600-file fallback
  spapi/                     # ServerPilot REST API client + TTL cache;
                             #   one file per resource (servers/apps/sysusers/databases/sshkeys)
                             #   plus client.go, cache.go, resolve.go
  sandbox/                   # path validator — the security boundary for site_* tools
  ssh/                       # SSH/SFTP: keygen (Ed25519 OpenSSH encoder), pool (connection cache),
                             #   ops (exec/SFTP), shellescape
  mcpserver/                 # mark3labs/mcp-go wiring; tools split as
                             #   tools_api.go / tools_bootstrap.go / tools_site.go
                             #   (matches the inventory below)
  wizard/                    # 11-step setup orchestration (charmbracelet/huh)
  clients/                   # MCP-client config patchers
                             #   registry + per-client patchers
                             #   (claude_code, claude_desktop, cursor, windsurf, vscode, codex)
                             #   entry.go (shared stdioEntry/vscodeEntry shape),
                             #   jsonpatch.go, tomlpatch.go, paths.go
  logging/logger.go          # slog → stderr ONLY (stdout is the MCP protocol channel)
install.sh                   # POSIX sh installer (macOS/Linux, reattaches /dev/tty)
install.ps1                  # PowerShell installer (Windows)
.goreleaser.yml              # build matrix darwin/linux/windows × amd64/arm64
```

## Tool inventory (15)

API tools (8): `sp_list_servers`, `sp_get_server`, `sp_list_apps`, `sp_get_app`, `sp_update_app_runtime`, `sp_list_databases`, `sp_update_db_password`, `sp_list_sysusers`.

Bootstrap tools (3): `sp_ssh_setup`, `sp_ssh_status`, `sp_ssh_remove`.

Site tools (4): `site_exec`, `site_read_file`, `site_write_file`, `site_list_files`.

Site tools take `site` (app name or domain). API tools take `app` or `server` (name, domain, or ID). Don't mix them.

## Credentials

Resolution order (in `internal/creds/store.go`):

1. `SERVERPILOT_CLIENT_ID` + `SERVERPILOT_API_KEY` env vars
2. OS keychain (macOS Keychain / Windows Credential Manager / Linux Secret Service)
3. `~/.config/serverpilot-mcp/credentials.json` (mode 0600) — file fallback for headless Linux
4. Returns `ErrNoCredentials` → CLI prints "Run `serverpilot-mcp setup`"

Never log credentials. Never write them to stdout. The `serve` command uses stdout exclusively for the MCP JSON-RPC protocol.

## MCP-client config patching

Each patcher in `internal/clients/` implements:

```go
type Patcher interface {
    ID() string
    DisplayName() string
    Detect() (installed bool, configPath string, err error)
    Patch(binaryPath string, dryRun bool) (changed bool, diff string, err error)
    Unpatch() (changed bool, err error)
}
```

JSON patching uses `tidwall/sjson` to set values by dot-path while preserving the rest of the document — critical for not destroying users' other `mcpServers` entries. Atomic write: `path.tmp` + `os.Rename`. Refuses to write if existing JSON is malformed.

VS Code is the odd one out — uses `servers.serverpilot` (not `mcpServers.*`) with a required `type:"stdio"` field. Codex uses TOML (`pelletier/go-toml/v2`); comments are not preserved on round-trip.

## Gotchas

- **`site_write_file` overwrites the entire file.** No append/patch mode. Always read → modify → write.
- **`site_exec` default timeout is 30s.** Pass `timeout: 120000` for broad scans.
- **Path sandbox in `internal/sandbox/validate.go`** is the only security boundary for `site_*` tools. Rules: pop on `..` even at empty stack, normalize before prefix-check, reject `/srv/users/alice2` when basePath is `/srv/users/alice` (must require `/` separator after basePath). The fuzz test in `validate_test.go` enforces these — don't relax it.
- **SSH host key verification is disabled** (`ssh.InsecureIgnoreHostKey()`) — there's no `known_hosts` plumbing yet. Hardening is tracked as a follow-up. Documented in `internal/ssh/pool.go`.
- **Logs go to stderr.** stdout is reserved for MCP JSON-RPC. Use `slog` via `internal/logging`.
- **Cache invalidation:** `apps.UpdateRuntime` invalidates `apps` + `app:{id}`; `databases.UpdatePassword` invalidates the `database` prefix.
- **Don't widen `Patcher.Detect()` heuristics.** Currently checks parent-dir-or-file existence (Claude Code is always-detected because it reads `~/.claude.json` whether or not it exists). Adding registry / app-bundle checks adds platform code without much benefit.

## Testing

- `internal/spapi/*_test.go` — `httptest.Server` with canned JSON; covers Basic auth, caching, invalidation, resolve fallbacks, and locked-down JSON output shapes.
- `internal/sandbox/validate_test.go` — table-driven + `FuzzValidatePath` to catch traversal escapes.
- `internal/ssh/keygen_test.go` — proves the `openssh-key-v1` encoder roundtrips through `golang.org/x/crypto/ssh.ParsePrivateKey` and produces signing-correct keys.
- `internal/clients/*_test.go` — golden in/out: file-not-exists, file-with-other-servers, stale-our-entry, malformed (must error), unpatch.
- `internal/ssh/pool_test.go` and the wire-level portions of `ops_test.go` — **integration only**, requires a real SSH host; not in CI. (`ssh/ops_test.go` itself only exercises `classifyMode`, which runs anywhere.)

## Code style

- Standard Go layout: `cmd/` for entry points, `internal/` for everything else (no `pkg/` until external consumers exist).
- `slog` for logging (stderr only).
- Error messages from API tools/handlers must preserve their documented text format — downstream skills parse them (e.g., `Server not found: <name>`, `App not found: <name>`).
- No emojis in code or commits unless explicitly requested.
