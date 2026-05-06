# ServerPilot MCP

Manage ServerPilot-hosted sites from Claude Code, Claude Desktop, Cursor, Windsurf, VS Code, or Codex CLI through a single static binary. No runtime dependencies; no hand-edited config files.

## Install

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/februality/serverpilot-mcp/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/februality/serverpilot-mcp/main/install.ps1 | iex
```

The installer downloads a verified release binary, drops it in your PATH, then launches an interactive setup wizard that:

1. Prompts for your ServerPilot Client ID and API Key
2. Verifies them against the ServerPilot API
3. Stores them in your OS keychain (or a permission-restricted file on headless Linux)
4. Generates an Ed25519 SSH key, registers it with ServerPilot, and assigns it to all your sysusers
5. Detects which MCP clients you have installed and patches each of their config files

After it finishes, restart your MCP client. The 15 tools (see below) are available immediately.

## Tools

| Tool | Purpose |
|---|---|
| `sp_list_servers` | List all ServerPilot servers with IPs, plans, runtimes |
| `sp_get_server` | Server details by ID or name |
| `sp_list_apps` | List apps, optionally filtered by server |
| `sp_get_app` | App details by ID, name, or domain |
| `sp_update_app_runtime` | Change an app's PHP runtime version |
| `sp_list_databases` | List databases, optionally filtered by app or server |
| `sp_update_db_password` | Change a database user's MySQL password |
| `sp_list_sysusers` | List system users, optionally filtered by server |
| `sp_ssh_setup` | Generate SSH key, register with ServerPilot, assign to sysusers |
| `sp_ssh_status` | Show SSH key status (local + remote + per-sysuser) |
| `sp_ssh_remove` | Remove the SSH key from sysusers, optionally delete it from ServerPilot |
| `site_exec` | Run a shell command on a site's server as the site's sysuser |
| `site_read_file` | Read a file via SFTP |
| `site_write_file` | Write a file via SFTP |
| `site_list_files` | List directory contents via SFTP |

`site_*` tools accept the app's name or domain. All file paths are sandboxed to `/srv/users/USERNAME/`.

## CLI

```
serverpilot-mcp setup                       # Interactive wizard (re-runs are safe)
serverpilot-mcp status [--json]             # What's configured and where
serverpilot-mcp doctor                      # Verify creds, API ping, per-client config
serverpilot-mcp install --all               # Patch every detected MCP client
serverpilot-mcp install --client cursor     # Just one
serverpilot-mcp install --all --dry-run     # Preview diffs without writing
serverpilot-mcp uninstall --all             # Reverse the install
serverpilot-mcp uninstall --remove-creds    # Also wipe stored credentials
serverpilot-mcp uninstall --remove-key      # Also delete local SSH key
serverpilot-mcp serve                       # Run the MCP stdio server (what clients invoke)
serverpilot-mcp version
```

## Manual configuration

If you'd rather not run the installer, download the binary from [Releases](https://github.com/februality/serverpilot-mcp/releases), put it on your `PATH`, and add this entry to your MCP client config:

**Claude Code (`~/.claude.json`), Claude Desktop, Cursor (`~/.cursor/mcp.json`), Windsurf (`~/.codeium/windsurf/mcp_config.json`):**

```json
{
  "mcpServers": {
    "serverpilot": {
      "command": "/usr/local/bin/serverpilot-mcp",
      "args": ["serve"]
    }
  }
}
```

**VS Code (`~/.vscode/mcp.json`)** uses `servers` (not `mcpServers`) and requires `type`:

```json
{
  "servers": {
    "serverpilot": {
      "type": "stdio",
      "command": "/usr/local/bin/serverpilot-mcp",
      "args": ["serve"]
    }
  }
}
```

**Codex CLI (`~/.codex/config.toml`):**

```toml
[mcp_servers.serverpilot]
command = "/usr/local/bin/serverpilot-mcp"
args = ["serve"]
```

Then either run `serverpilot-mcp setup --skip-clients` to handle credentials and SSH key, or set these env vars in your shell:

```bash
export SERVERPILOT_CLIENT_ID=cid_xxxxxxxxxxxx
export SERVERPILOT_API_KEY=sk_xxxxxxxxxxxxxxxxxxxxxxxx
```

## Configuration

Environment variables (all optional except creds):

| Variable | Default | Purpose |
|---|---|---|
| `SERVERPILOT_CLIENT_ID` | — | ServerPilot API client ID (overrides keychain) |
| `SERVERPILOT_API_KEY` | — | ServerPilot API key (overrides keychain) |
| `SP_SSH_KEY_PATH` | `~/.ssh/serverpilot-mcp` | SSH private key path |
| `SP_SSH_KEY_NAME` | `claude-mcp-serverpilot` | Name registered with ServerPilot |
| `SP_CACHE_TTL_SECONDS` | `300` | API response cache TTL |
| `SP_SSH_TIMEOUT_MS` | `30000` | SSH connection timeout |

## Building from source

Requires Go 1.22+.

```bash
git clone https://github.com/februality/serverpilot-mcp
cd serverpilot-mcp
go build -o serverpilot-mcp ./cmd/serverpilot-mcp
go test ./...
```

## Security notes

- Credentials are stored in the OS keychain (macOS Keychain / Windows Credential Manager / Linux Secret Service) or a `0600` file in your config dir if no keychain is available.
- The SSH layer uses `ssh.InsecureIgnoreHostKey()` — there's no `known_hosts` plumbing yet. Hardening to first-time-trust + on-disk verification is a planned follow-up.
- All SFTP file operations are sandboxed to `/srv/users/USERNAME/`. Path traversal attempts are rejected before reaching SFTP.

## License

MIT
