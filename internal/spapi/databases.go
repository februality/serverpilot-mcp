package spapi

import (
	"fmt"
	"strings"
)

type DatabasesAPI struct {
	client *Client
	cache  *TTLCache
}

func NewDatabasesAPI(client *Client, cache *TTLCache) *DatabasesAPI {
	return &DatabasesAPI{client: client, cache: cache}
}

func (d *DatabasesAPI) List() ([]SPDatabase, error) {
	if v, ok := d.cache.Get("databases"); ok {
		return v.([]SPDatabase), nil
	}
	var resp spListResponse[SPDatabase]
	if err := d.client.Get("/dbs", &resp); err != nil {
		return nil, err
	}
	d.cache.Set("databases", resp.Data)
	return resp.Data, nil
}

func (d *DatabasesAPI) Get(id string) (*SPDatabase, error) {
	key := "database:" + id
	if v, ok := d.cache.Get(key); ok {
		db := v.(SPDatabase)
		return &db, nil
	}
	var resp spSingleResponse[SPDatabase]
	if err := d.client.Get("/dbs/"+pathSegment(id), &resp); err != nil {
		return nil, err
	}
	d.cache.Set(key, resp.Data)
	return &resp.Data, nil
}

func (d *DatabasesAPI) ListByApp(appID string) ([]SPDatabase, error) {
	dbs, err := d.List()
	if err != nil {
		return nil, err
	}
	out := make([]SPDatabase, 0, len(dbs))
	for _, db := range dbs {
		if db.AppID == appID {
			out = append(out, db)
		}
	}
	return out, nil
}

func (d *DatabasesAPI) ListByServer(serverID string) ([]SPDatabase, error) {
	dbs, err := d.List()
	if err != nil {
		return nil, err
	}
	out := make([]SPDatabase, 0, len(dbs))
	for _, db := range dbs {
		if db.ServerID == serverID {
			out = append(out, db)
		}
	}
	return out, nil
}

func (d *DatabasesAPI) UpdatePassword(dbID, dbUserID, password string) (*SPActionResponse, error) {
	d.cache.InvalidatePrefix("database")
	var resp SPActionResponse
	body := map[string]any{
		"user": map[string]any{"id": dbUserID, "password": password},
	}
	if err := d.client.Post("/dbs/"+pathSegment(dbID), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type CreateDatabaseRequest struct {
	AppID    string
	Name     string
	UserName string
	Password string
}

type CreateDatabaseResult struct {
	Database SPDatabase
	ActionID string
}

func (d *DatabasesAPI) Create(req CreateDatabaseRequest) (*CreateDatabaseResult, error) {
	d.cache.InvalidatePrefix("database")
	body := map[string]any{
		"appid": req.AppID,
		"name":  req.Name,
		"user":  map[string]any{"name": req.UserName, "password": req.Password},
	}
	var resp struct {
		ActionID string     `json:"actionid"`
		Data     SPDatabase `json:"data"`
	}
	if err := d.client.Post("/dbs", body, &resp); err != nil {
		return nil, err
	}
	return &CreateDatabaseResult{Database: resp.Data, ActionID: resp.ActionID}, nil
}

func (d *DatabasesAPI) Delete(id string) (*SPActionResponse, error) {
	d.cache.InvalidatePrefix("database")
	var resp SPActionResponse
	if err := d.client.Delete("/dbs/"+pathSegment(id), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Resolve accepts a database ID or name (case-insensitive). Database names
// can repeat across servers, so an ambiguous name returns an error listing
// each candidate's ID and server.
func (d *DatabasesAPI) Resolve(idOrName string) (*SPDatabase, error) {
	ident := strings.TrimSpace(idOrName)
	dbs, listErr := d.List()
	if listErr == nil {
		var m []SPDatabase
		for i := range dbs {
			if dbs[i].ID == ident {
				if db, err := d.Get(ident); err == nil {
					return db, nil
				}
				return &dbs[i], nil
			}
			if strings.EqualFold(dbs[i].Name, ident) {
				m = append(m, dbs[i])
			}
		}
		switch len(m) {
		case 1:
			return &m[0], nil
		case 0:
		default:
			c := make([]string, len(m))
			for i, x := range m {
				c[i] = fmt.Sprintf("%s (id %s, server %s)", x.Name, x.ID, x.ServerID)
			}
			return nil, ambiguousError("database", ident, c)
		}
	}
	if ident != "" {
		if db, err := d.Get(ident); err == nil && db.ID != "" {
			return db, nil
		}
	}
	if listErr != nil {
		return nil, listErr
	}
	return nil, fmt.Errorf("Database not found: %s", idOrName)
}
