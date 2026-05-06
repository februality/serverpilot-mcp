package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/spapi"
)

// RegisterAPITools registers the 8 ServerPilot API tools on the given server.
// Tool names, input schemas, and JSON output shapes are part of the public
// contract — downstream skills parse them, so changes are backwards-incompatible.
func RegisterAPITools(s *server.MCPServer, d *Deps) {
	s.AddTool(toolListServers(), handleListServers(d))
	s.AddTool(toolGetServer(), handleGetServer(d))
	s.AddTool(toolListApps(), handleListApps(d))
	s.AddTool(toolGetApp(), handleGetApp(d))
	s.AddTool(toolUpdateAppRuntime(), handleUpdateAppRuntime(d))
	s.AddTool(toolListDatabases(), handleListDatabases(d))
	s.AddTool(toolUpdateDBPassword(), handleUpdateDBPassword(d))
	s.AddTool(toolListSysUsers(), handleListSysUsers(d))
}

// ---- Tool definitions ----

func toolListServers() mcp.Tool {
	return mcp.NewTool("sp_list_servers",
		mcp.WithDescription("List all ServerPilot servers with IPs, plans, and available runtimes"),
	)
}

func toolGetServer() mcp.Tool {
	return mcp.NewTool("sp_get_server",
		mcp.WithDescription("Get details for a specific server by ID or name"),
		mcp.WithString("server", mcp.Required(), mcp.Description("Server ID or name")),
	)
}

func toolListApps() mcp.Tool {
	return mcp.NewTool("sp_list_apps",
		mcp.WithDescription("List all apps, optionally filtered by server"),
		mcp.WithString("server", mcp.Description("Filter by server ID or name")),
	)
}

func toolGetApp() mcp.Tool {
	return mcp.NewTool("sp_get_app",
		mcp.WithDescription("Get details for a specific app by ID, name, or domain"),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
	)
}

func toolUpdateAppRuntime() mcp.Tool {
	return mcp.NewTool("sp_update_app_runtime",
		mcp.WithDescription("Change an app's PHP runtime version"),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
		mcp.WithString("runtime", mcp.Required(), mcp.Description("PHP runtime (e.g. php8.0, php8.3)")),
	)
}

func toolListDatabases() mcp.Tool {
	return mcp.NewTool("sp_list_databases",
		mcp.WithDescription("List databases, optionally filtered by app or server"),
		mcp.WithString("app", mcp.Description("Filter by app ID, name, or domain")),
		mcp.WithString("server", mcp.Description("Filter by server ID or name")),
	)
}

func toolUpdateDBPassword() mcp.Tool {
	return mcp.NewTool("sp_update_db_password",
		mcp.WithDescription("Change a database user's MySQL password"),
		mcp.WithString("database", mcp.Required(), mcp.Description("Database ID")),
		mcp.WithString("password", mcp.Required(), mcp.Description("New password (min 8 characters)")),
	)
}

func toolListSysUsers() mcp.Tool {
	return mcp.NewTool("sp_list_sysusers",
		mcp.WithDescription("List system users, optionally filtered by server"),
		mcp.WithString("server", mcp.Description("Filter by server ID or name")),
	)
}

// ---- Output shape helpers (locked-down JSON shapes — public contract) ----

type serverSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	IP          string   `json:"ip"`
	Plan        string   `json:"plan"`
	Runtimes    []string `json:"runtimes"`
	Firewall    bool     `json:"firewall"`
	Autoupdates bool     `json:"autoupdates"`
}

type appSummary struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Domains   []string `json:"domains"`
	Runtime   string   `json:"runtime"`
	SSL       string   `json:"ssl"` // "auto" | "custom" | "none"
	ServerID  string   `json:"serverid"`
	WordPress bool     `json:"wordpress"`
}

type databaseSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	User     string `json:"user"`
	UserID   string `json:"userId"`
	AppID    string `json:"appId"`
	ServerID string `json:"serverId"`
}

type sysUserSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ServerID string `json:"serverId"`
}

func sslLabel(a *spapi.SPApp) string {
	switch {
	case a.AutoSSL:
		return "auto"
	case a.SSL != nil:
		return "custom"
	default:
		return "none"
	}
}

func jsonText(v any) (*mcp.CallToolResult, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

func errResult(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(err.Error()), nil
}

// ---- Handlers ----

func handleListServers(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		list, err := d.Servers.List()
		if err != nil {
			return errResult(err)
		}
		out := make([]serverSummary, len(list))
		for i, s := range list {
			out[i] = serverSummary{
				ID:          s.ID,
				Name:        s.Name,
				IP:          s.LastAddress,
				Plan:        s.Plan,
				Runtimes:    s.AvailableRuntimes,
				Firewall:    s.Firewall,
				Autoupdates: s.Autoupdates,
			}
		}
		return jsonText(out)
	}
}

func handleGetServer(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("server")
		if err != nil {
			return errResult(err)
		}
		srv, err := d.Servers.Resolve(id)
		if err != nil {
			return errResult(err)
		}
		return jsonText(srv)
	}
}

func handleListApps(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var apps []spapi.SPApp
		if filter := req.GetString("server", ""); filter != "" {
			srv, err := d.Servers.Resolve(filter)
			if err != nil {
				return errResult(err)
			}
			apps, err = d.Apps.ListByServer(srv.ID)
			if err != nil {
				return errResult(err)
			}
		} else {
			a, err := d.Apps.List()
			if err != nil {
				return errResult(err)
			}
			apps = a
		}
		out := make([]appSummary, len(apps))
		for i, a := range apps {
			out[i] = appSummary{
				ID:        a.ID,
				Name:      a.Name,
				Domains:   a.Domains,
				Runtime:   a.Runtime,
				SSL:       sslLabel(&a),
				ServerID:  a.ServerID,
				WordPress: a.WordPress != nil,
			}
		}
		return jsonText(out)
	}
}

func handleGetApp(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("app")
		if err != nil {
			return errResult(err)
		}
		app, err := d.Apps.Resolve(id)
		if err != nil {
			return errResult(err)
		}
		return jsonText(app)
	}
}

func handleUpdateAppRuntime(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appID, err := req.RequireString("app")
		if err != nil {
			return errResult(err)
		}
		runtime, err := req.RequireString("runtime")
		if err != nil {
			return errResult(err)
		}
		app, err := d.Apps.Resolve(appID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Apps.UpdateRuntime(app.ID, runtime)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`PHP runtime for "%s" updated to %s. Action ID: %s`, app.Name, runtime, res.ActionID),
		), nil
	}
}

func handleListDatabases(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var dbs []spapi.SPDatabase
		appFilter := req.GetString("app", "")
		serverFilter := req.GetString("server", "")
		switch {
		case appFilter != "":
			a, err := d.Apps.Resolve(appFilter)
			if err != nil {
				return errResult(err)
			}
			dbs, err = d.Databases.ListByApp(a.ID)
			if err != nil {
				return errResult(err)
			}
		case serverFilter != "":
			srv, err := d.Servers.Resolve(serverFilter)
			if err != nil {
				return errResult(err)
			}
			dbs, err = d.Databases.ListByServer(srv.ID)
			if err != nil {
				return errResult(err)
			}
		default:
			d2, err := d.Databases.List()
			if err != nil {
				return errResult(err)
			}
			dbs = d2
		}
		out := make([]databaseSummary, len(dbs))
		for i, db := range dbs {
			out[i] = databaseSummary{
				ID:       db.ID,
				Name:     db.Name,
				User:     db.User.Name,
				UserID:   db.User.ID,
				AppID:    db.AppID,
				ServerID: db.ServerID,
			}
		}
		return jsonText(out)
	}
}

func handleUpdateDBPassword(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dbID, err := req.RequireString("database")
		if err != nil {
			return errResult(err)
		}
		password, err := req.RequireString("password")
		if err != nil {
			return errResult(err)
		}
		if len(password) < 8 {
			return errResult(fmt.Errorf("password must be at least 8 characters"))
		}
		db, err := d.Databases.Get(dbID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Databases.UpdatePassword(dbID, db.User.ID, password)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`Password updated for database user "%s" on database "%s". Action ID: %s`, db.User.Name, db.Name, res.ActionID),
		), nil
	}
}

func handleListSysUsers(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var users []spapi.SPSysUser
		if filter := req.GetString("server", ""); filter != "" {
			srv, err := d.Servers.Resolve(filter)
			if err != nil {
				return errResult(err)
			}
			users, err = d.SysUsers.ListByServer(srv.ID)
			if err != nil {
				return errResult(err)
			}
		} else {
			u, err := d.SysUsers.List()
			if err != nil {
				return errResult(err)
			}
			users = u
		}
		out := make([]sysUserSummary, len(users))
		for i, u := range users {
			out[i] = sysUserSummary{
				ID:       u.ID,
				Name:     u.Name,
				ServerID: u.ServerID,
			}
		}
		return jsonText(out)
	}
}
