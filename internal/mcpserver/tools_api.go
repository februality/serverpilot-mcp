package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

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

const asyncNote = " Asynchronous: returns an action ID immediately; call sp_get_action with wait_seconds to wait for it to finish."

func refreshParam() mcp.ToolOption {
	return mcp.WithBoolean("refresh", mcp.Description("Bypass the 5-minute cache, e.g. after changes made in the ServerPilot dashboard"))
}

func toolListServers() mcp.Tool {
	return newTool("sp_list_servers", "List servers", kindRead, false,
		append([]mcp.ToolOption{
			mcp.WithDescription("List ServerPilot servers with ID, name, IP, plan, available PHP runtimes, firewall and auto-update status. Paged: a note after the list gives the next offset when there are more."),
			refreshParam(),
		}, pagingParams("servers", defaultPageLimit, maxPageLimit)...)...,
	)
}

func toolGetServer() mcp.Tool {
	return newTool("sp_get_server", "Get server", kindRead, false,
		mcp.WithDescription("Get full details for one server."),
		mcp.WithString("server", mcp.Required(), mcp.Description("Server ID, name, or IP address")),
	)
}

func toolListApps() mcp.Tool {
	return newTool("sp_list_apps", "List apps", kindRead, false,
		append([]mcp.ToolOption{
			mcp.WithDescription("List apps (sites) with their domains, PHP runtime, SSL state, server, system user, and whether WordPress is installed. " +
				"Filter with query and/or server; to look up one known app, sp_get_app is cheaper. Paged: a note after the list gives the next offset when there are more."),
			mcp.WithString("server", mcp.Description("Only apps on this server (ID, name, or IP)")),
			queryParam("apps whose name or a domain contains this text"),
			refreshParam(),
		}, pagingParams("apps", defaultPageLimit, maxPageLimit)...)...,
	)
}

func toolGetApp() mcp.Tool {
	return newTool("sp_get_app", "Get app", kindRead, false,
		mcp.WithDescription("Get details for one app, including its filesystem paths (web root, logs) for use with site_* tools. Secrets (WordPress admin password, SSL private key) are never returned."),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
	)
}

func toolUpdateAppRuntime() mcp.Tool {
	return newTool("sp_update_app_runtime", "Change PHP version", kindIdempotent, false,
		mcp.WithDescription("Change an app's PHP runtime version. The runtime must be one the server offers (see sp_list_servers)."+asyncNote),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
		mcp.WithString("runtime", mcp.Required(), mcp.Description("PHP runtime exactly as listed in the server's runtimes, e.g. php8.3")),
	)
}

func toolListDatabases() mcp.Tool {
	return newTool("sp_list_databases", "List databases", kindRead, false,
		append([]mcp.ToolOption{
			mcp.WithDescription("List MySQL databases with their user, app, and server. Filter with app, server, and/or query. Passwords are never returned. " +
				"Paged: a note after the list gives the next offset when there are more."),
			mcp.WithString("app", mcp.Description("Only databases of this app (ID, name, or domain)")),
			mcp.WithString("server", mcp.Description("Only databases on this server (ID, name, or IP)")),
			queryParam("databases whose name or user name contains this text"),
			refreshParam(),
		}, pagingParams("databases", defaultPageLimit, maxPageLimit)...)...,
	)
}

func toolUpdateDBPassword() mcp.Tool {
	return newTool("sp_update_db_password", "Change database password", kindIdempotent, false,
		mcp.WithDescription("Set a new MySQL password for a database's user. The app's config (e.g. DB_PASSWORD in wp-config.php) is NOT updated; change it too or the site loses its database connection."+asyncNote),
		mcp.WithString("database", mcp.Required(), mcp.Description("Database ID or name")),
		mcp.WithString("password", mcp.Required(), mcp.MinLength(8), mcp.MaxLength(200), mcp.Description("New password, 8-200 characters, no leading or trailing whitespace")),
	)
}

func toolListSysUsers() mcp.Tool {
	return newTool("sp_list_sysusers", "List system users", kindRead, false,
		append([]mcp.ToolOption{
			mcp.WithDescription("List system (Linux) users, which own apps and are what site_* tools log in as. Filter with server and/or query. " +
				"Paged: a note after the list gives the next offset when there are more."),
			mcp.WithString("server", mcp.Description("Only users on this server (ID, name, or IP)")),
			queryParam("users whose name contains this text"),
			refreshParam(),
		}, pagingParams("system users", defaultPageLimit, maxPageLimit)...)...,
	)
}

func toolGetAction() mcp.Tool {
	return newTool("sp_get_action", "Check action status", kindRead, false,
		mcp.WithDescription("Get the status of an action returned by a write tool: 'open' (still running), 'success', or 'error'. Pass wait_seconds to wait server-side until the action finishes instead of polling."),
		mcp.WithString("action_id", mcp.Required(), mcp.Description("Action ID returned by a previous write tool")),
		mcp.WithNumber("wait_seconds", mcp.Min(0), mcp.Max(maxActionWaitSeconds),
			mcp.Description("Wait up to this many seconds for the action to leave 'open' (max 50; call again to keep waiting). 0 or omitted returns the current status immediately.")),
	)
}

func toolUpdateAppDomains() mcp.Tool {
	return newTool("sp_update_app_domains", "Set app domains", kindDestructive, false,
		mcp.WithDescription("Replace the full list of domains an app serves. This REPLACES the list, it does not append: to add a domain, pass every existing domain plus the new one (sp_get_app shows the current list). The response includes the previous list."+asyncNote),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
		mcp.WithArray("domains", mcp.Required(),
			mcp.Description("Complete list of domain names, without scheme, e.g. ['example.com','www.example.com']"),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func toolSetAppSSL() mcp.Tool {
	return newTool("sp_set_app_ssl", "Configure SSL", kindIdempotent, false,
		mcp.WithDescription("Configure SSL for an app. Pass exactly one of: 'auto: true' (enable AutoSSL; ServerPilot can only do this once an AutoSSL certificate is available, which requires the app's domains to point at the server), 'force' (HTTP-to-HTTPS redirect; SSL must already be active), or 'key'+'cert' (custom certificate). To turn SSL off, use sp_remove_app_ssl."+asyncNote),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
		mcp.WithBoolean("auto", mcp.Description("true enables AutoSSL. The API does not accept false; use sp_remove_app_ssl to disable it")),
		mcp.WithBoolean("force", mcp.Description("Enable (true) or disable (false) the HTTP-to-HTTPS redirect")),
		mcp.WithString("key", mcp.Description("Custom SSL: PEM-encoded private key")),
		mcp.WithString("cert", mcp.Description("Custom SSL: PEM-encoded certificate")),
		mcp.WithString("cacerts", mcp.Description("Custom SSL: PEM-encoded CA chain; omit for none")),
	)
}

func toolRemoveAppSSL() mcp.Tool {
	return newTool("sp_remove_app_ssl", "Remove SSL", kindIdempotent, false,
		mcp.WithDescription("Remove an app's SSL configuration (deletes a custom certificate and/or disables AutoSSL). The site will no longer serve HTTPS."+asyncNote),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain")),
	)
}

func toolCreateApp() mcp.Tool {
	return newTool("sp_create_app", "Create app", kindAdditive, false,
		mcp.WithDescription("Create a new app on a server, owned by a system user. Installs WordPress too when all four wordpress_* fields are given. The app is not usable until its action succeeds."+asyncNote),
		mcp.WithString("name", mcp.Required(), mcp.Pattern("^[a-z0-9]{3,30}$"), mcp.Description("App name, 3-30 lowercase letters/digits (no dashes); also its directory name")),
		mcp.WithString("server", mcp.Required(), mcp.Description("Server ID, name, or IP")),
		mcp.WithString("sysuser", mcp.Required(), mcp.Description("System user ID or name on that server (see sp_list_sysusers)")),
		mcp.WithString("runtime", mcp.Required(), mcp.Description("PHP runtime from the server's runtimes list (sp_list_servers), e.g. php8.3")),
		mcp.WithArray("domains",
			mcp.Description("Domain names to serve, without scheme"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("wordpress_site_title", mcp.Description("WordPress site title (required to install WP)")),
		mcp.WithString("wordpress_admin_user", mcp.Description("WordPress admin username (required to install WP)")),
		mcp.WithString("wordpress_admin_password", mcp.Description("WordPress admin password, 8-200 characters (required to install WP)")),
		mcp.WithString("wordpress_admin_email", mcp.Description("WordPress admin email (required to install WP)")),
	)
}

func toolCreateDatabase() mcp.Tool {
	return newTool("sp_create_database", "Create database", kindAdditive, false,
		mcp.WithDescription("Create a MySQL database for an app, with a single user."+asyncNote),
		mcp.WithString("app", mcp.Required(), mcp.Description("App ID, name, or domain that will own the database")),
		mcp.WithString("name", mcp.Required(), mcp.Pattern("^[a-z0-9-]{3,64}$"), mcp.Description("Database name, 3-64 lowercase letters/digits/dashes")),
		mcp.WithString("user_name", mcp.Required(), mcp.MaxLength(16), mcp.Description("Database user name, at most 16 characters")),
		mcp.WithString("password", mcp.Required(), mcp.MinLength(8), mcp.MaxLength(200), mcp.Description("Database user password, 8-200 characters, no leading or trailing whitespace")),
	)
}

func toolDeleteDatabase() mcp.Tool {
	return newTool("sp_delete_database", "Delete database", kindDestructive, false,
		mcp.WithDescription("Permanently delete a database and its user. All data in it is lost; there is no undo. Take a backup first (e.g. site_exec 'wp db export')."+asyncNote),
		mcp.WithString("database", mcp.Required(), mcp.Description("Database ID or name")),
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
	// Additive fields: save the model a sp_list_servers / sp_get_app round-trip.
	Server    string `json:"server,omitempty"` // server name
	SysUserID string `json:"sysuserid"`
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
	Paths       *appPaths     `json:"paths,omitempty"`
}

// appPaths are absolute server paths for the app, ready to pass to site_*
// tools. Omitted when the system user can't be looked up.
type appPaths struct {
	SysUser string `json:"sysuser"`
	Home    string `json:"home"`
	App     string `json:"app"`
	Public  string `json:"public"`
	Logs    string `json:"logs"`
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

// jsonText returns v as compact JSON. Indentation costs roughly a quarter to
// a third more tokens on list output and models don't need it.
func jsonText(v any) (*mcp.CallToolResult, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

func errResult(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(err.Error()), nil
}

// cleanDomains reduces each entry to a bare lowercase host (models often pass
// "https://example.com/"), drops empties and duplicates, and keeps order.
// A nil input stays nil so "domains omitted" is still distinguishable.
func cleanDomains(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, d := range in {
		h := spapi.NormalizeDomain(d)
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// maybeRefresh clears the API cache when the caller passed refresh: true.
func maybeRefresh(d *Deps, req mcp.CallToolRequest) {
	if req.GetBool("refresh", false) {
		d.Cache.Clear()
	}
}

// ---- Handlers ----

func handleListServers(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		maybeRefresh(d, req)
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
		return pagedJSON(out, req, defaultPageLimit, "servers", "")
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
		maybeRefresh(d, req)
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
		serverNames := map[string]string{}
		if servers, err := d.Servers.List(); err == nil { // best effort
			for _, srv := range servers {
				serverNames[srv.ID] = srv.Name
			}
		}
		query := req.GetString("query", "")
		out := make([]appSummary, 0, len(apps))
		for _, a := range apps {
			if !matchesQuery(query, append([]string{a.Name}, a.Domains...)...) {
				continue
			}
			out = append(out, appSummary{
				ID:        a.ID,
				Name:      a.Name,
				Domains:   a.Domains,
				Runtime:   a.Runtime,
				SSL:       sslLabel(&a),
				ServerID:  a.ServerID,
				WordPress: a.WordPress != nil,
				Server:    serverNames[a.ServerID],
				SysUserID: a.SysUserID,
			})
		}
		return pagedJSON(out, req, defaultPageLimit, "apps", "narrow with query or server")
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
		if su, err := d.SysUsers.Get(app.SysUserID); err == nil && su.Name != "" { // best effort
			home := "/srv/users/" + su.Name
			detail.Paths = &appPaths{
				SysUser: su.Name,
				Home:    home,
				App:     home + "/apps/" + app.Name,
				Public:  home + "/apps/" + app.Name + "/public",
				Logs:    home + "/log/" + app.Name,
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
		maybeRefresh(d, req)
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
			if serverFilter != "" { // both given: apply both
				srv, err := d.Servers.Resolve(serverFilter)
				if err != nil {
					return errResult(err)
				}
				dbs = slices.DeleteFunc(dbs, func(db spapi.SPDatabase) bool { return db.ServerID != srv.ID })
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
		query := req.GetString("query", "")
		out := make([]databaseSummary, 0, len(dbs))
		for _, db := range dbs {
			if !matchesQuery(query, db.Name, db.User.Name) {
				continue
			}
			out = append(out, databaseSummary{
				ID:       db.ID,
				Name:     db.Name,
				User:     db.User.Name,
				UserID:   db.User.ID,
				AppID:    db.AppID,
				ServerID: db.ServerID,
			})
		}
		return pagedJSON(out, req, defaultPageLimit, "databases", "narrow with app, server, or query")
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
		if strings.TrimSpace(password) != password {
			return errResult(fmt.Errorf("password must not start or end with whitespace (ServerPilot API rule)"))
		}
		db, err := d.Databases.Resolve(dbID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Databases.UpdatePassword(db.ID, db.User.ID, password)
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
		maybeRefresh(d, req)
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
		query := req.GetString("query", "")
		out := make([]sysUserSummary, 0, len(users))
		for _, u := range users {
			if !matchesQuery(query, u.Name) {
				continue
			}
			out = append(out, sysUserSummary{
				ID:       u.ID,
				Name:     u.Name,
				ServerID: u.ServerID,
			})
		}
		return pagedJSON(out, req, defaultPageLimit, "system users", "narrow with server or query")
	}
}

// maxActionWaitSeconds stays under the 60 s request timeout that several MCP
// clients default to (Codex CLI tool_timeout_sec, the MCP TypeScript SDK);
// longer waits are done by calling again.
const maxActionWaitSeconds = 50

// actionPollInterval is a var so tests can shorten it.
var actionPollInterval = func(attempt int) time.Duration {
	return min(time.Duration(attempt+1)*2*time.Second, 5*time.Second)
}

func handleGetAction(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("action_id")
		if err != nil {
			return errResult(err)
		}
		wait := min(req.GetFloat("wait_seconds", 0), maxActionWaitSeconds)
		deadline := time.Now().Add(time.Duration(wait * float64(time.Second)))

		act, err := d.Actions.Get(id)
		for attempt := 0; err == nil && act.Status == "open" && time.Now().Before(deadline); attempt++ {
			sleep := min(actionPollInterval(attempt), time.Until(deadline))
			select {
			case <-ctx.Done():
				return errResult(fmt.Errorf("stopped waiting for action %s: %w", id, ctx.Err()))
			case <-time.After(sleep):
			}
			act, err = d.Actions.Get(id)
		}
		if err != nil {
			return errResult(err)
		}
		if act.Status != "open" {
			// The change has landed (or failed); cached app/database data
			// may predate it.
			d.Cache.Clear()
		}
		res, err := jsonText(actionDetail{
			ID:          act.ID,
			Status:      act.Status,
			ServerID:    act.ServerID,
			DateCreated: act.DateCreated,
		})
		if err == nil && act.Status == "open" && wait > 0 {
			res.Content = append(res.Content, mcp.NewTextContent(
				fmt.Sprintf("Still running after waiting %.0fs. Call sp_get_action again to keep waiting.", wait)))
		}
		return res, err
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
		domains = cleanDomains(domains)
		app, err := d.Apps.Resolve(appID)
		if err != nil {
			return errResult(err)
		}
		previous := append([]string(nil), app.Domains...)
		res, err := d.Apps.UpdateDomains(app.ID, domains)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`Domains for "%s" updated to [%s] (previously [%s]). Action ID: %s`,
				app.Name, strings.Join(domains, ", "), strings.Join(previous, ", "), res.ActionID),
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
			if !req.GetBool("auto", false) {
				return errResult(fmt.Errorf("the ServerPilot API only accepts auto: true. To disable AutoSSL, use sp_remove_app_ssl"))
			}
			body["auto"] = true
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
		domains := cleanDomains(req.GetStringSlice("domains", nil))

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
		if strings.TrimSpace(password) != password {
			return errResult(fmt.Errorf("password must not start or end with whitespace (ServerPilot API rule)"))
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
		db, err := d.Databases.Resolve(dbID)
		if err != nil {
			return errResult(err)
		}
		res, err := d.Databases.Delete(db.ID)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(
			fmt.Sprintf(`Database "%s" (%s) deleted. Action ID: %s`, db.Name, db.ID, res.ActionID),
		), nil
	}
}
