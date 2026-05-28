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
	if err := a.client.Get("/apps/"+id, &resp); err != nil {
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

func (a *AppsAPI) FindByName(name string) (*SPApp, error) {
	apps, err := a.List()
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(name)
	for i := range apps {
		if strings.ToLower(apps[i].Name) == target {
			return &apps[i], nil
		}
	}
	return nil, nil
}

func (a *AppsAPI) FindByDomain(domain string) (*SPApp, error) {
	apps, err := a.List()
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(domain)
	for i := range apps {
		for _, d := range apps[i].Domains {
			if strings.ToLower(d) == target {
				return &apps[i], nil
			}
		}
	}
	return nil, nil
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
	if err := a.client.Post("/apps/"+id, body, &resp); err != nil {
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
	if err := a.client.Post("/apps/"+id+"/ssl", body, &resp); err != nil {
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
	if err := a.client.Delete("/apps/"+id+"/ssl", &resp); err != nil {
		return nil, err
	}
	return &UpdateRuntimeResult{ActionID: resp.ActionID}, nil
}

// Resolve tries ID, then name, then domain. Mirrors src/api/apps.ts:resolve.
func (a *AppsAPI) Resolve(idOrNameOrDomain string) (*SPApp, error) {
	if app, err := a.Get(idOrNameOrDomain); err == nil {
		return app, nil
	}
	if app, err := a.FindByName(idOrNameOrDomain); err != nil {
		return nil, err
	} else if app != nil {
		return app, nil
	}
	app, err := a.FindByDomain(idOrNameOrDomain)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, fmt.Errorf("App not found: %s", idOrNameOrDomain)
	}
	return app, nil
}
