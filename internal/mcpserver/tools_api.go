package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/spapi"
)

// RegisterAPITools registers the ServerPilot API tools on the given server.
// Tool names, input schemas, and JSON output shapes are part of the public
// contract — downstream skills parse them, so changes are backwards-incompatible.
//
// All writes (sp_update_app_runtime, sp_update_app_domains, sp_set_app_ssl,
// sp_remove_app_ssl, sp_create_app, sp_update_db_password, sp_create_database,
// sp_delete_database) are skipped when d.Cfg.ReadOnly is true so they never
// appear in tools/list.
func RegisterAPITools(s *server.MCPServer, d *Deps) {
	s.AddTool(toolListServers(), handleListServers(d))
	s.AddTool(toolGetServer(), handleGetServer(d))
	s.AddTool(toolListApps(), handleListApps(d))
	s.AddTool(toolGetApp(), handleGetApp(d))
	s.AddTool(toolListDatabases(), handleListDatabases(d))
	s.AddTool(toolListSysUsers(), handleListSysUsers(d))
	s.AddTool(toolGetAction(), handleGetAction(d))
	if !d.Cfg.ReadOnly {
		s.AddTool(toolUpdateAppRuntime(), handleUpdateAppRuntime(d))
		s.AddTool(toolUpdateAppDomains(), handleUpdateAppDomains(d))
		s.AddTool(toolSetAppSSL(), handleSetAppSSL(d))
		s.AddTool(toolRemoveAppSSL(), handleRemoveAppSSL(d))
		s.AddTool(toolCreateApp(), handleCreateApp(d))
		s.AddTool(toolUpdateDBPassword(), handleUpdateDBPassword(d))
		s.AddTool(toolCreateDatabase(), handleCreateDatabase(d))
		s.AddTool(toolDeleteDatabase(), handleDeleteDatabase(d))
	}
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

func toolGetAction() mcp.Tool {
	return mcp.NewTool("sp_get_action",
		mcp.WithDescription("Get the status of a ServerPilot action returned by any write tool. Status is 'open' (still running), 'success', or 'error'."),
		mcp.WithString("action_id", mcp.Required(), mcp.Description("Action ID returned by a previous write tool")),
	)
}

func toolUpdateAppDomains() mcp.Tool {
	return mcp.NewTool("sp_update_app_domains",
		mcp.WithDescription("Replace the full list of domains served by an app. The API replaces, not appends — pass every domain you want active."),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
		mcp.WithArray("domains", mcp.Required(),
			mcp.Description("Complete list of domain names (e.g. ['example.com','www.example.com'])"),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func toolSetAppSSL() mcp.Tool {
	return mcp.NewTool("sp_set_app_ssl",
		mcp.WithDescription("Configure SSL for an app. Pass exactly one of: 'auto' (toggle AutoSSL), 'force' (toggle HTTPS redirect), or 'key'+'cert' (install custom certificate)."),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
		mcp.WithBoolean("auto", mcp.Description("Enable (true) or disable (false) AutoSSL via Let's Encrypt")),
		mcp.WithBoolean("force", mcp.Description("Enable (true) or disable (false) the HTTP-to-HTTPS redirect (ForceSSL)")),
		mcp.WithString("key", mcp.Description("Custom SSL: PEM-encoded private key contents")),
		mcp.WithString("cert", mcp.Description("Custom SSL: PEM-encoded certificate contents")),
		mcp.WithString("cacerts", mcp.Description("Custom SSL: PEM-encoded CA certificate(s); empty for none")),
	)
}

func toolRemoveAppSSL() mcp.Tool {
	return mcp.NewTool("sp_remove_app_ssl",
		mcp.WithDescription("Remove an app's SSL configuration (deletes the custom cert and/or disables AutoSSL)."),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
	)
}

func toolCreateApp() mcp.Tool {
	return mcp.NewTool("sp_create_app",
		mcp.WithDescription("Create a new app. Optionally installs WordPress when all four wordpress_* fields are provided."),
		mcp.WithString("name", mcp.Required(), mcp.Description("App nickname, 3–30 lowercase letters/digits")),
		mcp.WithString("server", mcp.Required(), mcp.Description("Server ID or name")),
		mcp.WithString("sysuser", mcp.Required(), mcp.Description("System user ID or name on that server")),
		mcp.WithString("runtime", mcp.Required(), mcp.Description("PHP runtime (must appear in the server's available_runtimes)")),
		mcp.WithArray("domains",
			mcp.Description("Optional list of domain names to serve this app on"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("wordpress_site_title", mcp.Description("WordPress site title (required to install WP)")),
		mcp.WithString("wordpress_admin_user", mcp.Description("WordPress admin username (required to install WP)")),
		mcp.WithString("wordpress_admin_password", mcp.Description("WordPress admin password (required to install WP)")),
		mcp.WithString("wordpress_admin_email", mcp.Description("WordPress admin email (required to install WP)")),
	)
}

func toolCreateDatabase() mcp.Tool {
	return mcp.NewTool("sp_create_database",
		mcp.WithDescription("Create a MySQL database for an app, with a single user."),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain that will own the database")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Database name, 3–64 lowercase letters/digits/dash")),
		mcp.WithString("user_name", mcp.Required(), mcp.Description("Database user name, max 16 characters")),
		mcp.WithString("password", mcp.Required(), mcp.Description("Database user password, 8–200 characters")),
	)
}

func toolDeleteDatabase() mcp.Tool {
	return mcp.NewTool("sp_delete_database",
		mcp.WithDescription("Delete a database (and its user)."),
		mcp.WithString("database", mcp.Required(), mcp.Description("Database ID")),
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

// appDetail is the locked-down shape returned by sp_get_app. It deliberately
// omits SPSSL.{Key,Cert,CACerts} and SPWordPress.AdminPassword — secrets
// readable from the server itself (wp-config.php, /etc/letsencrypt/...) via
// site_read_file when needed.
type appDetail struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Domains     []string      `json:"domains"`
	Runtime     string        `json:"runtime"`
	SSL         string        `json:"ssl"` // "auto" | "custom" | "none"
	AutoSSL     bool          `json:"autossl"`
	ServerID    string        `json:"serverid"`
	SysUserID   string        `json:"sysuserid"`
	DateCreated int64         `json:"datecreated"`
	WordPress   *wpPublicInfo `json:"wordpress,omitempty"`
}

type wpPublicInfo struct {
	SiteTitle  string `json:"site_title"`
	AdminUser  string `json:"admin_user"`
	AdminEmail string `json:"admin_email"`
	LoginURL   string `json:"login_url"`
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

// actionDetail is the locked-down shape returned by sp_get_action.
type actionDetail struct {
	ID          string `json:"id"`
	Status      string `json:"status"` // "success" | "open" | "error"
	ServerID    string `json:"serverid"`
	DateCreated int64  `json:"datecreated"`
}

// createdAppSummary is returned by sp_create_app. Mirrors the redaction policy
// of appDetail — WordPress admin password is never echoed back, even though
// the caller supplied it (callers already have it; logging tools shouldn't).
type createdAppSummary struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	ServerID  string   `json:"serverid"`
	SysUserID string   `json:"sysuserid"`
	Runtime   string   `json:"runtime"`
	Domains   []string `json:"domains"`
	WordPress bool     `json:"wordpress"`
	ActionID  string   `json:"actionid"`
}

// createdDatabaseSummary is returned by sp_create_database. Password is not
// echoed back — the caller supplied it.
type createdDatabaseSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	AppID    string `json:"appid"`
	ServerID string `json:"serverid"`
	User     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	ActionID string `json:"actionid"`
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
		detail := appDetail{
			ID:          app.ID,
			Name:        app.Name,
			Domains:     app.Domains,
			Runtime:     app.Runtime,
			SSL:         sslLabel(app),
			AutoSSL:     app.AutoSSL,
			ServerID:    app.ServerID,
			SysUserID:   app.SysUserID,
			DateCreated: app.DateCreated,
		}
		if app.WordPress != nil {
			detail.WordPress = &wpPublicInfo{
				SiteTitle:  app.WordPress.SiteTitle,
				AdminUser:  app.WordPress.AdminUser,
				AdminEmail: app.WordPress.AdminEmail,
				LoginURL:   app.WordPress.LoginURL,
			}
		}
		return jsonText(detail)
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
		srv, err := d.Servers.Resolve(app.ServerID)
		if err != nil {
			return errResult(err)
		}
		if !slices.Contains(srv.AvailableRuntimes, runtime) {
			return errResult(fmt.Errorf(
				"runtime %q not available on server %q. Available: %s",
				runtime, srv.Name, strings.Join(srv.AvailableRuntimes, ", "),
			))
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

func handleGetAction(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("action_id")
		if err != nil {
			return errResult(err)
		}
		act, err := d.Actions.Get(id)
		if err != nil {
			return errResult(err)
		}
		return jsonText(actionDetail{
			ID:          act.ID,
			Status:      act.Status,
			ServerID:    act.ServerID,
			DateCreated: act.DateCreated,
		})
	}
}

func handleUpdateAppDomains(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appID, err := req.RequireString("app")
		if err != nil {
			return errResult(err)
		}
		domains := req.GetStringSlice("domains", nil)
		if domains == nil {
			return errResult(fmt.Errorf("domains is required"))
		}
		app, err := d.Apps.Resolve(appID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Apps.UpdateDomains(app.ID, domains)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`Domains for "%s" updated to [%s]. Action ID: %s`, app.Name, strings.Join(domains, ", "), res.ActionID),
		), nil
	}
}

func handleSetAppSSL(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appID, err := req.RequireString("app")
		if err != nil {
			return errResult(err)
		}
		args := req.GetArguments()
		_, hasAuto := args["auto"]
		_, hasForce := args["force"]
		_, hasKey := args["key"]
		_, hasCert := args["cert"]

		modes := 0
		if hasAuto {
			modes++
		}
		if hasForce {
			modes++
		}
		if hasKey || hasCert {
			modes++
		}
		if modes != 1 {
			return errResult(fmt.Errorf("specify exactly one of: auto, force, or key+cert"))
		}

		body := map[string]any{}
		switch {
		case hasAuto:
			body["auto"] = req.GetBool("auto", false)
		case hasForce:
			body["force"] = req.GetBool("force", false)
		default: // custom cert
			if !hasKey || !hasCert {
				return errResult(fmt.Errorf("custom SSL requires both key and cert"))
			}
			body["key"] = req.GetString("key", "")
			body["cert"] = req.GetString("cert", "")
			if cacerts := req.GetString("cacerts", ""); cacerts != "" {
				body["cacerts"] = cacerts
			} else {
				body["cacerts"] = nil
			}
		}

		app, err := d.Apps.Resolve(appID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Apps.SetSSL(app.ID, body)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`SSL updated for "%s". Action ID: %s`, app.Name, res.ActionID),
		), nil
	}
}

func handleRemoveAppSSL(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appID, err := req.RequireString("app")
		if err != nil {
			return errResult(err)
		}
		app, err := d.Apps.Resolve(appID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Apps.RemoveSSL(app.ID)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`SSL removed for "%s". Action ID: %s`, app.Name, res.ActionID),
		), nil
	}
}

func handleCreateApp(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return errResult(err)
		}
		serverArg, err := req.RequireString("server")
		if err != nil {
			return errResult(err)
		}
		sysuserArg, err := req.RequireString("sysuser")
		if err != nil {
			return errResult(err)
		}
		runtime, err := req.RequireString("runtime")
		if err != nil {
			return errResult(err)
		}
		domains := req.GetStringSlice("domains", nil)

		srv, err := d.Servers.Resolve(serverArg)
		if err != nil {
			return errResult(err)
		}
		if !slices.Contains(srv.AvailableRuntimes, runtime) {
			return errResult(fmt.Errorf(
				"runtime %q not available on server %q. Available: %s",
				runtime, srv.Name, strings.Join(srv.AvailableRuntimes, ", "),
			))
		}
		su, err := d.SysUsers.Resolve(sysuserArg, srv.ID)
		if err != nil {
			return errResult(err)
		}

		wpTitle := req.GetString("wordpress_site_title", "")
		wpUser := req.GetString("wordpress_admin_user", "")
		wpPass := req.GetString("wordpress_admin_password", "")
		wpEmail := req.GetString("wordpress_admin_email", "")
		anyWP := wpTitle != "" || wpUser != "" || wpPass != "" || wpEmail != ""
		allWP := wpTitle != "" && wpUser != "" && wpPass != "" && wpEmail != ""
		if anyWP && !allWP {
			return errResult(fmt.Errorf("WordPress install requires all four wordpress_* fields (site_title, admin_user, admin_password, admin_email)"))
		}

		create := spapi.CreateAppRequest{
			Name:      name,
			SysUserID: su.ID,
			Runtime:   runtime,
			Domains:   domains,
		}
		if allWP {
			create.WordPress = &spapi.CreateAppWordPress{
				SiteTitle:     wpTitle,
				AdminUser:     wpUser,
				AdminPassword: wpPass,
				AdminEmail:    wpEmail,
			}
		}
		res, err := d.Apps.Create(create)
		if err != nil {
			return errResult(err)
		}
		out := createdAppSummary{
			ID:        res.App.ID,
			Name:      res.App.Name,
			ServerID:  res.App.ServerID,
			SysUserID: res.App.SysUserID,
			Runtime:   res.App.Runtime,
			Domains:   res.App.Domains,
			WordPress: allWP,
			ActionID:  res.ActionID,
		}
		if out.Domains == nil {
			out.Domains = []string{}
		}
		if out.ServerID == "" {
			// API doesn't always echo serverid in the create response; fall back
			// to the resolved server we used to validate runtime.
			out.ServerID = srv.ID
		}
		return jsonText(out)
	}
}

func handleCreateDatabase(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appArg, err := req.RequireString("app")
		if err != nil {
			return errResult(err)
		}
		name, err := req.RequireString("name")
		if err != nil {
			return errResult(err)
		}
		userName, err := req.RequireString("user_name")
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
		app, err := d.Apps.Resolve(appArg)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Databases.Create(spapi.CreateDatabaseRequest{
			AppID:    app.ID,
			Name:     name,
			UserName: userName,
			Password: password,
		})
		if err != nil {
			return errResult(err)
		}
		out := createdDatabaseSummary{
			ID:       res.Database.ID,
			Name:     res.Database.Name,
			AppID:    res.Database.AppID,
			ServerID: res.Database.ServerID,
			ActionID: res.ActionID,
		}
		out.User.ID = res.Database.User.ID
		out.User.Name = res.Database.User.Name
		if out.AppID == "" {
			out.AppID = app.ID
		}
		if out.ServerID == "" {
			out.ServerID = app.ServerID
		}
		return jsonText(out)
	}
}

func handleDeleteDatabase(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dbID, err := req.RequireString("database")
		if err != nil {
			return errResult(err)
		}
		db, err := d.Databases.Get(dbID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Databases.Delete(dbID)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`Database "%s" (%s) deleted. Action ID: %s`, db.Name, db.ID, res.ActionID),
		), nil
	}
}
