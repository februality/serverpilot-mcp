package mcpserver

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/config"
	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

const (
	ServerName    = "serverpilot"
	ServerVersion = "2.0.1"
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
		Resolver:  spapi.NewSiteResolver(apps, servers, sysusers),
		SSHPool:   pool,
		SSHOps:    mcsh.NewOps(pool),
	}
}

// New returns an MCP server with all 14 tools registered.
func New(deps *Deps) *server.MCPServer {
	s := server.NewMCPServer(ServerName, ServerVersion)
	RegisterAPITools(s, deps)
	RegisterBootstrapTools(s, deps)
	RegisterSiteTools(s, deps)
	return s
}
