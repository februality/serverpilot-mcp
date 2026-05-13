# ServerPilot MCP

Manage [ServerPilot](https://serverpilot.io)-hosted sites from Claude Code, Claude Desktop, Cursor, Windsurf, VS Code, or Codex CLI through a single static binary. Built on the [ServerPilot API](https://github.com/ServerPilot/API) and SSH. No runtime dependencies; no hand-edited config files.

> This is an independent, community-built project and is not affiliated with, endorsed by, or sponsored by ServerPilot. "ServerPilot" is a trademark of its respective owner; it is used here only to describe the service this tool integrates with.

## Install

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/februality/serverpilot-mcp/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/februality/serverpilot-mcp/main/install.ps1 | iex
```

**Before you start:** grab a Client ID and API Key from the [ServerPilot API page](https://manage.serverpilot.io/account/api) — keep that tab open.

After the installer downloads the binary, it launches a setup wizard. Step through it:

1. **Paste your Client ID** when prompted, then **paste your API Key** (input is hidden). The wizard verifies them against the ServerPilot API and prints a server / app count on success. It stores them in your OS keychain (or a `0600` file on headless Linux). If you want to bypass the keychain, hit Ctrl+C and re-run with `SERVERPILOT_CLIENT_ID=... SERVERPILOT_API_KEY=... serverpilot-mcp setup`.
2. **Confirm SSH key assignment** (default: Yes). The wizard generates an Ed25519 key at `~/.ssh/serverpilot-mcp`, registers it with ServerPilot, and assigns it to every sysuser on your account. Re-running the wizard later is safe — already-assigned users are skipped.
3. **Pick which AI tools to patch** from the multi-select list. Detected tools are pre-checked; press space to toggle, enter to confirm. Skip anything you don't want touched.
4. **Restart your AI tool** (quit and relaunch Claude Code, Cursor, etc.). The 15 tools listed below are now available.
5. **Verify** with `serverpilot-mcp doctor` — checks credentials, hits the API, and inspects each patched client config.

If anything looks off, `serverpilot-mcp status` shows what's configured and where, and `serverpilot-mcp setup` is always re-runnable.

## Usage examples

Once the wizard is done and your AI tool has been restarted, just talk to it. Below are five example tasks — light to heavy — that normally measure in hours of careful work, delegated in one sentence each. Prompts and agent responses are illustrative; your own runs will use your real sites and data.

### 1. Get the full picture of a site

> **You:** Tell me everything you can about example.com.
>
> **Agent:**
>
> **example.com** on **web2** (`5.6.7.8`, 2 GB plan)
> - Sysuser: `exampleuser`, path `/srv/users/exampleuser/apps/example/`
> - Runtime: PHP 8.3.12 (server also offers 8.1 and 8.4)
> - SSL: AutoSSL, valid until 2026-08-04
> - Domains: `example.com`, `www.example.com`
>
> **WordPress 6.5.2** (latest)
> - 11 plugins active; 3 have updates available (Elementor, WP Rocket, Yoast SEO)
> - Active theme: `twentytwentyfour` 1.4
> - 47 pages, 312 posts; last published 2026-05-04
> - DB size: 84 MB · autoload: 412 KB
>
> **Disk** — 3.2 GB total under `apps/example/`:
> ```
> 1.8G  wp-content/uploads
> 612M  wp-content/cache
> 388M  wp-content/plugins
>  92M  wp-content/themes
> ```
>
> **Health**
> - PHP error log clean for the past 7 days
> - WP cron healthy (14 events in next 24h)
> - Last `git pull` on `main`: 2026-04-29
> - `HEAD https://example.com/` → `200 OK`, 380 ms
>
> Anything you want to dig into?

### 2. Reclaim disk space on a near-full server

> **You:** web1 is at 92% disk. Find what's filling it and tell me what's safe to delete.
>
> **Agent:** Audited web1 (24 GB / 26 GB used). The big buckets:
>
> ```
> 14.2 GB  apps/example/wp-content/uploads          (live customer media)
>  4.8 GB  apps/oldproject/wp-content/cache         (last write 2025-11)
>  2.1 GB  apps/example/wp-content/backups          (14 plugin-generated copies)
>  1.1 GB  apps/staging/wp-content/debug.log        (WP_DEBUG_LOG left on since Feb)
>  870 MB  /tmp on web1                             (orphaned PHP session files, oldest 2025-08)
> ```
>
> **Safe to reclaim (~7.6 GB):**
> - `oldproject` cache — site last received traffic in November; regenerates on demand.
> - 11 of 14 `example` backup-plugin copies (keep the 3 most recent).
> - `staging/debug.log` — and toggle `WP_DEBUG_LOG` off so it doesn't refill.
> - `/tmp` session files older than 7 days.
>
> **Leaving alone:**
> - `example/wp-content/uploads` (14 GB) — real customer media; needs your call before pruning.
>
> Want me to run the cleanup? Each step is a single `site_exec` you can approve or skip.

### 3. Plan a PHP sunset across the fleet

> **You:** ServerPilot is dropping PHP 8.0 next quarter. Across all my apps, tell me which can move to 8.3 today and which need work first.
>
> **Agent:** 17 apps total: 11 already on 8.1+, 6 on 8.0. Analysis of the 6:
>
> | App | Server | Blockers | Verdict |
> |---|---|---|---|
> | client-1.example.com | web1 | none — WP 6.5.2, all plugins compat | ✅ safe now |
> | client-2.example.com | web1 | `acf-pro 5.12` (8.2 max), `wp-rocket 3.10` (8.2 max) | ⚠️ update plugins first |
> | client-3.example.com | web1 | `revslider 6.5.x` — vendor abandoned, no 8.3 build | 🚫 needs replacement |
> | client-4.example.com | web2 | none | ✅ safe now |
> | legacy.example.com | web2 | custom theme `vintage-2018` uses deprecated `mysql_*` API | 🚫 theme rewrite required |
> | internal.example.com | api1 | none — WP 6.5.2, only 3 plugins | ✅ safe now |
>
> **Suggested sequence:**
> 1. **Upgrade now** (3 apps): `client-1`, `client-4`, `internal`. I can do all three in one pass.
> 2. **Plugin updates first** (1 app): `client-2`. ACF Pro 6.x and WP Rocket 3.16+ are 8.3-compatible.
> 3. **Replace plugin** (1 app): `client-3`. Either license Slider Revolution 6.7+ or migrate sliders to Elementor.
> 4. **Theme rewrite** (1 app): `legacy.example.com`. The `mysql_*` calls will fatal under 8.3 — roughly 8–12 h of work.
>
> Want me to start with step 1?

### 4. Sweep the fleet for WordPress malware

> **You:** Scan every WordPress app on my account for malware indicators — modified core files, suspicious uploads, unfamiliar admin users.
>
> **Agent:** Scanned 17 apps across web1, web2, api1. 14 are clean. Three need attention:
>
> **🚩 oldsite.example.com (web1)** — likely fully compromised
> - `wp-content/uploads/2024/cache.php` (4.1 KB, created 2026-04-22 03:14) contains `eval(base64_decode(...))`. Uploads should never contain executable PHP.
> - Admin user `wp_admin_ZZH` created 2026-04-22 03:11 — same window, email `attacker@protonmail[.]com`.
> - `wp-includes/class-wp-hook.php` SHA-256 differs from upstream WP 6.4.3. Modified 2026-04-22.
>
> **🚩 staging.example.com (web1)**
> - `.htaccess` has a `RewriteRule` redirecting all 404s to `https://farma-spam[.]ru/...`. Added 2026-04-29.
>
> **🚩 client-portal.example.com (web2)**
> - `wp-content/mu-plugins/0-cache.php` looks like a normal mu-plugin but `file_get_contents()`s a remote payload at boot. Uploaded 2026-03-15.
>
> **In priority order:**
> 1. **oldsite.example.com is fully compromised.** Rotate the DB password, delete `wp_admin_ZZH`, restore `class-wp-hook.php` from a clean WP 6.4.3 download, remove `cache.php`, then re-scan.
> 2. **staging.example.com** — strip the `.htaccess` injection.
> 3. **client-portal.example.com** — quarantine `0-cache.php` and audit which IPs hit the site since March 15.
>
> Want me to start with oldsite cleanup?

### 5. Migrate a site between ServerPilot servers

> **You:** Migrate example.com from web1 to web2. Rewrite anything that references the old path, smoke-test the new server, and tell me when it's ready for DNS cutover.
>
> **Agent:** Walking through it.
>
> **Provision (ServerPilot API)**
> - Created app `example` on web2, sysuser `exampleuser2` (action `act_5021`).
> - Created database `example_wp` (action `act_5022`).
> - Set runtime to `php8.3` to match the source.
>
> **Migrate**
> ```
> [web1] mysqldump example_wp | gzip > /tmp/db.sql.gz       (87 MB)
> [web1] tar -czf /tmp/files.tar.gz apps/example/            (428 MB, 9,108 files)
> [transfer] web1 -> local -> web2 via SFTP                  (515 MB in 73s)
> [web2] tar -xzf files.tar.gz                               (extracted)
> [web2] gunzip -c db.sql.gz | mysql example_wp              (47 tables imported)
> ```
>
> **Rewrite**
> - Updated `wp-config.php` to point at the new database; table prefix kept identical.
> - `wp search-replace '/srv/users/exampleuser/apps/example' '/srv/users/exampleuser2/apps/example' --all-tables --skip-columns=guid` → 18 replacements across 4 tables.
>
> **Smoke test**
> - `curl -sI -H 'Host: example.com' http://5.6.7.8/` → `HTTP/1.1 200`, `X-Powered-By: PHP/8.3.12` ✓
> - `wp option get blogname` → `example.com` ✓
> - `wp post list --post_type=page --posts_per_page=1` → returned 1 page ✓
>
> Ready for DNS cutover — point the `example.com` A record at `5.6.7.8`. Want me to tear down the old app on web1 once you confirm DNS has propagated?

## Tools

| Tool | Purpose |
|---|---|
| `sp_list_servers` | List all servers with IPs, plans, available runtimes |
| `sp_get_server` | Server details by ID or name |
| `sp_list_apps` | List apps, optionally filtered by server |
| `sp_get_app` | App details by ID, name, or domain (secrets redacted) |
| `sp_update_app_runtime` | Change an app's PHP runtime version (validated against the server's available runtimes) |
| `sp_list_databases` | List databases, optionally filtered by app or server |
| `sp_update_db_password` | Change a database user's MySQL password |
| `sp_list_sysusers` | List system users, optionally filtered by server |
| `sp_ssh_setup` | Generate the SSH key, register with ServerPilot, assign to sysusers |
| `sp_ssh_status` | Show SSH key status (local + remote + per-sysuser) |
| `sp_ssh_remove` | Unassign the SSH key from sysusers; optionally delete it from ServerPilot |
| `site_exec` | Run a shell command on a site's server as the site's sysuser |
| `site_read_file` | Read a file via SFTP |
| `site_write_file` | Write a file via SFTP (overwrites — there is no append mode) |
| `site_list_files` | List directory contents via SFTP |

`site_*` tools accept the app's name or domain. All file paths are sandboxed to `/srv/users/USERNAME/`.

## CLI

```
serverpilot-mcp setup                       # Interactive wizard (re-runs are safe)
serverpilot-mcp status [--json]             # What's configured and where
serverpilot-mcp doctor                      # Verify creds, API ping, per-client config
serverpilot-mcp install --all               # Patch every detected AI tool
serverpilot-mcp install --client cursor     # Just one
serverpilot-mcp install --all --dry-run     # Preview diffs without writing
serverpilot-mcp install --all --read-only   # Hide every write tool (see Security notes)
serverpilot-mcp uninstall --all             # Reverse the install
serverpilot-mcp uninstall --remove-creds    # Also wipe stored credentials
serverpilot-mcp uninstall --remove-key      # Also delete the local SSH key
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

## Multiple ServerPilot accounts

Register the binary multiple times under distinct names with credentials passed via `env`:

```json
{
  "mcpServers": {
    "serverpilot-acme": {
      "command": "/usr/local/bin/serverpilot-mcp",
      "args": ["serve"],
      "env": {
        "SERVERPILOT_CLIENT_ID": "...",
        "SERVERPILOT_API_KEY":   "..."
      }
    },
    "serverpilot-globex": {
      "command": "/usr/local/bin/serverpilot-mcp",
      "args": ["serve"],
      "env": {
        "SERVERPILOT_CLIENT_ID": "...",
        "SERVERPILOT_API_KEY":   "..."
      }
    }
  }
}
```

Each entry runs an independent `serve` process bound to its own account; tool names are namespaced by your MCP client.

## Configuration

Environment variables (all optional except credentials):

| Variable | Default | Purpose |
|---|---|---|
| `SERVERPILOT_CLIENT_ID` | — | ServerPilot API client ID (overrides keychain) |
| `SERVERPILOT_API_KEY` | — | ServerPilot API key (overrides keychain) |
| `SP_SSH_KEY_PATH` | `~/.ssh/serverpilot-mcp` | SSH private key path |
| `SP_SSH_KEY_NAME` | `claude-mcp-serverpilot` | Name registered with ServerPilot |
| `SP_KNOWN_HOSTS_PATH` | `~/.ssh/serverpilot-mcp_known_hosts` | TOFU host-key file (separate from your personal `~/.ssh/known_hosts`) |
| `SP_CACHE_TTL_SECONDS` | `300` | API response cache TTL |
| `SP_SSH_TIMEOUT_MS` | `30000` | SSH connection timeout |
| `SP_SITE_EXEC_TIMEOUT_MS` | `120000` | `site_exec` default timeout. Pass `timeout: 0` from the tool call to disable. |
| `SP_INSECURE_DISABLE_HOST_KEY_CHECK` | unset | Set to `1` to bypass host-key verification (testing only — logs a warning) |
| `SP_READ_ONLY` | unset | Set to `1` to hide every write tool (`site_exec`, `site_write_file`, `sp_update_app_runtime`, `sp_update_db_password`, `sp_ssh_setup`, `sp_ssh_remove`) so the AI tool can read but cannot change anything. `setup` and `install` both accept `--read-only` to bake this into the client config. |

## Building from source

Requires Go 1.25+.

```bash
git clone https://github.com/februality/serverpilot-mcp
cd serverpilot-mcp
go build -o serverpilot-mcp ./cmd/serverpilot-mcp
go test ./...
```

## Security notes

> ⚠️ **Safety.** We deliberately left out the API tools for deleting sites, servers, sysusers, and databases, so your model can't tear those down through ServerPilot itself. But it can still do plenty of damage if you're not careful: it has shell access as your sysuser, so it can `rm -rf` your site files, drop database tables, overwrite files with no backup, change PHP runtimes, and reset database passwords. Read what it's about to do before you approve it, and keep your own backups.
>
> **Read-only mode.** If you'd rather not give the AI tool any way to change things, run `serverpilot-mcp setup --read-only` (or pass `--read-only` to `install`). The MCP server will start up with the six write tools (`site_exec`, `site_write_file`, `sp_update_app_runtime`, `sp_update_db_password`, `sp_ssh_setup`, `sp_ssh_remove`) hidden — the model never sees them in `tools/list`, so it's technically unable to call them. `serverpilot-mcp status` shows which clients are configured this way.

- **Credentials** are stored in the OS keychain (macOS Keychain / Windows Credential Manager / Linux Secret Service) or a `0600` file in your config directory if no keychain is available. Env vars override both.
- **SSH host keys** are pinned on first contact (TOFU) into `~/.ssh/serverpilot-mcp_known_hosts`, separate from your personal `known_hosts`. A subsequent mismatch fails loud and refuses to connect; remove the offending line manually if you genuinely re-imaged the server.
- **File operations** are sandboxed to `/srv/users/USERNAME/`. Path traversal attempts are rejected before reaching SFTP, with a fuzz test guarding the boundary.
- **`sp_get_app`** does not expose the WordPress admin password or SSL key/cert material. Read those from the server itself (`wp-config.php`, `/etc/letsencrypt/...`) via `site_read_file` if needed.
- **`site_exec`** has a 2-minute default timeout. Pass `timeout: 0` from the tool call to disable for long-running tasks.

## License

MIT
