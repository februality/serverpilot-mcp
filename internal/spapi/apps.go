package spapi

import (
	"fmt"
	"strings"
)

type AppsAPI struct {
	client *Client
	cache  *TTLCache
}

func NewAppsAPI(client *Client, cache *TTLCache) *AppsAPI {
	return &AppsAPI{client: client, cache: cache}
}

func (a *AppsAPI) List() ([]SPApp, error) {
	if v, ok := a.cache.Get("apps"); ok {
		return v.([]SPApp), nil
	}
	var resp spListResponse[SPApp]
	if err := a.client.Get("/apps", &resp); err != nil {
		return nil, err
	}
	a.cache.Set("apps", resp.Data)
	return resp.Data, nil
}

func (a *AppsAPI) Get(id string) (*SPApp, error) {
	key := "app:" + id
	if v, ok := a.cache.Get(key); ok {
		app := v.(SPApp)
		return &app, nil
	}
	var resp spSingleResponse[SPApp]
	if err := a.client.Get("/apps/"+pathSegment(id), &resp); err != nil {
		return nil, err
	}
	a.cache.Set(key, resp.Data)
	return &resp.Data, nil
}

func (a *AppsAPI) ListByServer(serverID string) ([]SPApp, error) {
	apps, err := a.List()
	if err != nil {
		return nil, err
	}
	out := make([]SPApp, 0, len(apps))
	for _, app := range apps {
		if app.ServerID == serverID {
			out = append(out, app)
		}
	}
	return out, nil
}

type UpdateRuntimeResult struct {
	ActionID string `json:"actionid"`
}

func (a *AppsAPI) UpdateRuntime(id, runtime string) (*UpdateRuntimeResult, error) {
	return a.update(id, map[string]any{"runtime": runtime})
}

func (a *AppsAPI) UpdateDomains(id string, domains []string) (*UpdateRuntimeResult, error) {
	if domains == nil {
		domains = []string{}
	}
	return a.update(id, map[string]any{"domains": domains})
}

func (a *AppsAPI) update(id string, body map[string]any) (*UpdateRuntimeResult, error) {
	a.cache.Invalidate("apps")
	a.cache.Invalidate("app:" + id)
	var resp struct {
		ActionID string `json:"actionid"`
		Data     SPApp  `json:"data"`
	}
	if err := a.client.Post("/apps/"+pathSegment(id), body, &resp); err != nil {
		return nil, err
	}
	return &UpdateRuntimeResult{ActionID: resp.ActionID}, nil
}

type CreateAppWordPress struct {
	SiteTitle     string `json:"site_title"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
	AdminEmail    string `json:"admin_email"`
}

type CreateAppRequest struct {
	Name      string              `json:"name"`
	SysUserID string              `json:"sysuserid"`
	Runtime   string              `json:"runtime"`
	Domains   []string            `json:"domains,omitempty"`
	WordPress *CreateAppWordPress `json:"wordpress,omitempty"`
}

type CreateAppResult struct {
	App      SPApp
	ActionID string
}

func (a *AppsAPI) Create(req CreateAppRequest) (*CreateAppResult, error) {
	a.cache.Invalidate("apps")
	var resp struct {
		ActionID string `json:"actionid"`
		Data     SPApp  `json:"data"`
	}
	if err := a.client.Post("/apps", req, &resp); err != nil {
		return nil, err
	}
	return &CreateAppResult{App: resp.Data, ActionID: resp.ActionID}, nil
}

// SetSSL posts to /apps/:id/ssl with one of three body shapes (handled by caller):
// {auto: bool}, {force: bool}, or {key, cert, cacerts} for a custom certificate.
func (a *AppsAPI) SetSSL(id string, body map[string]any) (*UpdateRuntimeResult, error) {
	a.cache.Invalidate("apps")
	a.cache.Invalidate("app:" + id)
	var resp struct {
		ActionID string `json:"actionid"`
	}
	if err := a.client.Post("/apps/"+pathSegment(id)+"/ssl", body, &resp); err != nil {
		return nil, err
	}
	return &UpdateRuntimeResult{ActionID: resp.ActionID}, nil
}

func (a *AppsAPI) RemoveSSL(id string) (*UpdateRuntimeResult, error) {
	a.cache.Invalidate("apps")
	a.cache.Invalidate("app:" + id)
	var resp struct {
		ActionID string `json:"actionid"`
	}
	if err := a.client.Delete("/apps/"+pathSegment(id)+"/ssl", &resp); err != nil {
		return nil, err
	}
	return &UpdateRuntimeResult{ActionID: resp.ActionID}, nil
}

// Resolve accepts an app ID, name (case-insensitive), or domain. Domains are
// normalized first (scheme, path, port stripped), and a missing "www." prefix
// is tolerated in either direction when there is no exact match. The cached
// app list is consulted before any per-ID GET so a name lookup costs no
// wasted 404 round-trip. Ambiguous names/domains return an error listing the
// candidates rather than silently picking one.
func (a *AppsAPI) Resolve(idOrNameOrDomain string) (*SPApp, error) {
	ident := strings.TrimSpace(idOrNameOrDomain)
	apps, listErr := a.List()
	if listErr == nil {
		for i := range apps {
			if apps[i].ID == ident {
				if app, err := a.Get(ident); err == nil {
					return app, nil
				}
				return &apps[i], nil
			}
		}
		if m := filterApps(apps, func(x *SPApp) bool { return strings.EqualFold(x.Name, ident) }); len(m) > 0 {
			return pickApp(ident, m)
		}
		host := NormalizeDomain(ident)
		alt := "www." + host
		if strings.HasPrefix(host, "www.") {
			alt = strings.TrimPrefix(host, "www.")
		}
		for _, h := range []string{host, alt} {
			if m := filterApps(apps, func(x *SPApp) bool { return hasDomain(x, h) }); len(m) > 0 {
				return pickApp(ident, m)
			}
		}
	}
	// Not in the (possibly stale) list — try it as an ID directly.
	if ident != "" {
		if app, err := a.Get(ident); err == nil && app.ID != "" {
			return app, nil
		}
	}
	if listErr != nil {
		return nil, listErr
	}
	return nil, fmt.Errorf("App not found: %s", idOrNameOrDomain)
}

func filterApps(apps []SPApp, keep func(*SPApp) bool) []SPApp {
	var out []SPApp
	for i := range apps {
		if keep(&apps[i]) {
			out = append(out, apps[i])
		}
	}
	return out
}

func hasDomain(app *SPApp, host string) bool {
	for _, d := range app.Domains {
		if strings.EqualFold(d, host) {
			return true
		}
	}
	return false
}

func pickApp(ident string, m []SPApp) (*SPApp, error) {
	if len(m) == 1 {
		return &m[0], nil
	}
	c := make([]string, len(m))
	for i, x := range m {
		c[i] = fmt.Sprintf("%s (id %s, server %s)", x.Name, x.ID, x.ServerID)
	}
	return nil, ambiguousError("app", ident, c)
}
