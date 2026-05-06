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
	a.cache.Invalidate("apps")
	a.cache.Invalidate("app:" + id)
	var resp struct {
		ActionID string `json:"actionid"`
		Data     SPApp  `json:"data"`
	}
	if err := a.client.Post("/apps/"+id, map[string]any{"runtime": runtime}, &resp); err != nil {
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
