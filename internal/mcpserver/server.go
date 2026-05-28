package mcpserver

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

const (
	ServerName    = "serverpilot"
	ServerVersion = "2.2.0"
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
		SSHOps:    mcsh.NewOps(pool),
	}
}

// New returns an MCP server with tools registered. With deps.Cfg.ReadOnly
// true, the twelve mutating tools (sp_update_app_runtime, sp_update_app_domains,
// sp_set_app_ssl, sp_remove_app_ssl, sp_create_app, sp_update_db_password,
// sp_create_database, sp_delete_database, sp_ssh_setup, sp_ssh_remove,
// site_exec, site_write_file) are not registered and never appear in tools/list.
func New(deps *Deps) *server.MCPServer {
	s := server.NewMCPServer(ServerName, ServerVersion)
	RegisterAPITools(s, deps)
	RegisterBootstrapTools(s, deps)
	RegisterSiteTools(s, deps)
	return s
}
