package mcpserver

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

const (
	ServerName    = "serverpilot"
	ServerVersion = "2.3.0"
)

type Deps struct {
	Cfg       *config.Config
	Cache     *spapi.TTLCache
	Client    *spapi.Client
	Servers   *spapi.ServersAPI
	Apps      *spapi.AppsAPI
	SysUsers  *spapi.SysUsersAPI
	Databases *spapi.DatabasesAPI
	SSHKeys   *spapi.SSHKeysAPI
	Actions   *spapi.ActionsAPI
	Resolver  *spapi.SiteResolver
	SSHPool   *mcsh.Pool
	SSHOps    *mcsh.Ops
}

// Build wires all dependencies from a loaded config.
func Build(cfg *config.Config) *Deps {
	cache := spapi.NewTTLCache(cfg.CacheTTLSeconds)
	client := spapi.NewClient(cfg.ClientID, cfg.APIKey)
	servers := spapi.NewServersAPI(client, cache)
	apps := spapi.NewAppsAPI(client, cache)
	sysusers := spapi.NewSysUsersAPI(client, cache)
	databases := spapi.NewDatabasesAPI(client, cache)
	sshkeys := spapi.NewSSHKeysAPI(client)
	actions := spapi.NewActionsAPI(client)
	verifier := mcsh.NewHostKeyVerifier(cfg.KnownHostsPath, cfg.InsecureHostKey)
	pool := mcsh.NewPool(cfg.SSHKeyPath, cfg.SSHTimeoutMs, verifier)
	return &Deps{
		Cfg:       cfg,
		Cache:     cache,
		Client:    client,
		Servers:   servers,
		Apps:      apps,
		SysUsers:  sysusers,
		Databases: databases,
		SSHKeys:   sshkeys,
		Actions:   actions,
		Resolver:  spapi.NewSiteResolver(apps, servers, sysusers),
		SSHPool:   pool,
		// In read-only mode the path sandbox is the only boundary (site_exec
		// is hidden), so also reject symlinks that point outside it.
		SSHOps: mcsh.NewOps(pool, cfg.ReadOnly),
	}
}

// New returns an MCP server with tools registered. With deps.Cfg.ReadOnly
// true, the fourteen mutating tools (sp_update_app_runtime,
// sp_update_app_domains, sp_set_app_ssl, sp_remove_app_ssl, sp_create_app,
// sp_update_db_password, sp_create_database, sp_delete_database, sp_ssh_setup,
// sp_ssh_remove, site_exec, site_write_file, site_edit_file, site_upload_file)
// are not registered and never appear in tools/list.
func New(deps *Deps) *server.MCPServer {
	s := server.NewMCPServer(ServerName, ServerVersion,
		server.WithInstructions(Instructions(deps.Cfg)),
		server.WithRecovery(),
	)
	RegisterAPITools(s, deps)
	RegisterBootstrapTools(s, deps)
	RegisterSiteTools(s, deps)
	return s
}

// Instructions is the server-level guidance sent in the initialize response.
// Clients put it in the model's context once, so it carries the cross-tool
// workflow that no single tool description can: the async action model, the
// SSH prerequisite, and where things live on a ServerPilot server.
func Instructions(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("Manage ServerPilot-hosted servers and apps. sp_* tools call the ServerPilot API; " +
		"site_* tools work inside one app over SSH/SFTP as that app's system user.\n")
	if cfg.Account != "" {
		fmt.Fprintf(&b, "This server is bound to the ServerPilot account %q. Other serverpilot-* servers, if present, are other accounts.\n", cfg.Account)
	}
	if cfg.ReadOnly {
		b.WriteString("READ-ONLY MODE: tools that change anything are not available. Report what should change instead of looking for a way to do it.\n")
	}
	b.WriteString(`
Identifiers: sp_* tools take app (ID, name, or domain), server (ID, name, or IP) and database (ID or name). site_* tools take site (app name or domain). Apps, sites and servers also accept a full URL (https://example.com/...). An ambiguous name returns an error listing the candidate IDs; retry with an ID.
`)
	fmt.Fprintf(&b, `
Lists are paged: list tools return up to %d items per call (%d for site_list_files), and when there are more, a note after the JSON gives the offset for the next page (keep the same filters). To find one thing, use the query/server filters or a get tool (sp_get_app) instead of paging through everything.
`, defaultPageLimit, defaultListLimit)
	if !cfg.ReadOnly {
		b.WriteString(`
Writes are asynchronous: the sp_* API write tools (sp_create_*, sp_update_*, sp_set_app_ssl, sp_remove_app_ssl, sp_delete_database) return an action ID and complete in the background; most finish within seconds, while creating an app with WordPress or issuing an AutoSSL certificate can take longer. Call sp_get_action with wait_seconds to wait until it reports success or error before relying on the change, e.g. before running site_* tools on a newly created app. sp_ssh_setup and sp_ssh_remove finish before they return.
`)
	}
	if cfg.ReadOnly {
		b.WriteString(`
SSH prerequisite: site_* tools need this machine's SSH key to be authorized for the app's system user. sp_ssh_status shows which users have it. Authorizing a user is a write (sp_ssh_setup), so in read-only mode the person must do it outside this session.
`)
	} else {
		b.WriteString(`
SSH prerequisite: site_* tools need a one-time sp_ssh_setup, which creates a local key and authorizes it for system users. sp_ssh_status shows which users have it. If a site_* call fails with an SSH authentication error, set up that system user.
`)
	}
	b.WriteString(`
Server layout (relative paths resolve against the system user's home, /srv/users/<sysuser>; sp_get_app returns the exact paths):
`)
	if cfg.ReadOnly {
		b.WriteString("- apps/<app>/public: web root; for WordPress, wp-config.php is here.\n")
	} else {
		b.WriteString("- apps/<app>/public: web root; for WordPress, wp-config.php is here. site_exec runs here by default and wp-cli works from here.\n")
	}
	b.WriteString(`- log/<app>/: web server and PHP error logs. Read the end with site_read_file tail=200.
- Access is limited to the system user's home and permissions; there is no root/sudo.

`)
	fmt.Fprintf(&b, "Output limits: site_read_file returns at most %d lines / %d KB per call; when it returns less than the whole file, a note after the content says how to continue (offset to page, tail for the end). ",
		mcsh.DefaultReadLines, mcsh.DefaultReadBytes/1000)
	if !cfg.ReadOnly {
		fmt.Fprintf(&b, "site_exec keeps the start and end of output beyond ~%d KB and drops the middle, so send large output to a file and read it in parts.\n", mcsh.ExecOutputBudget/1000)
		b.WriteString("Edit files with site_edit_file (exact string replacement) rather than re-sending whole files through site_write_file, and never write back content from a partial read.\n")
	} else {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "File transfer: site_download_file copies a server file to this machine (binaries, backups, large logs) without passing it through the conversation. Local paths must be inside %s.\n", cfg.TransferDir)
	return b.String()
}

// toolKind sets the MCP annotation hints. mcp-go's NewTool defaults every
// tool to readOnlyHint=false, destructiveHint=true, which makes clients treat
// a harmless list call like a deletion. Every tool declares its kind.
type toolKind int

const (
	kindRead        toolKind = iota // no side effects
	kindAdditive                    // creates something; changes nothing that exists
	kindIdempotent                  // modifies existing state; repeating it is a no-op
	kindDestructive                 // modifies or deletes existing state; not idempotent
)

// newTool wraps mcp.NewTool with annotations. openWorld marks tools whose
// effects reach beyond the ServerPilot account (arbitrary shell commands).
func newTool(name, title string, kind toolKind, openWorld bool, opts ...mcp.ToolOption) mcp.Tool {
	ann := []mcp.ToolOption{
		mcp.WithTitleAnnotation(title),
		mcp.WithReadOnlyHintAnnotation(kind == kindRead),
		mcp.WithDestructiveHintAnnotation(kind == kindIdempotent || kind == kindDestructive),
		mcp.WithIdempotentHintAnnotation(kind == kindRead || kind == kindIdempotent),
		mcp.WithOpenWorldHintAnnotation(openWorld),
	}
	return mcp.NewTool(name, append(opts, ann...)...)
}
