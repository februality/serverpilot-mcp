package spapi

import (
	"fmt"
	"strings"
)

type ServersAPI struct {
	client *Client
	cache  *TTLCache
}

func NewServersAPI(client *Client, cache *TTLCache) *ServersAPI {
	return &ServersAPI{client: client, cache: cache}
}

func (s *ServersAPI) List() ([]SPServer, error) {
	if v, ok := s.cache.Get("servers"); ok {
		return v.([]SPServer), nil
	}
	var resp spListResponse[SPServer]
	if err := s.client.Get("/servers", &resp); err != nil {
		return nil, err
	}
	s.cache.Set("servers", resp.Data)
	return resp.Data, nil
}

func (s *ServersAPI) Get(id string) (*SPServer, error) {
	key := "server:" + id
	if v, ok := s.cache.Get(key); ok {
		srv := v.(SPServer)
		return &srv, nil
	}
	var resp spSingleResponse[SPServer]
	if err := s.client.Get("/servers/"+id, &resp); err != nil {
		return nil, err
	}
	s.cache.Set(key, resp.Data)
	return &resp.Data, nil
}

func (s *ServersAPI) FindByName(name string) (*SPServer, error) {
	servers, err := s.List()
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(name)
	for i := range servers {
		if strings.ToLower(servers[i].Name) == target {
			return &servers[i], nil
		}
	}
	return nil, nil
}

// Resolve tries ID first, then name. Mirrors src/api/servers.ts:resolve —
// any error from Get() (not just 404) falls through to the name lookup.
func (s *ServersAPI) Resolve(idOrName string) (*SPServer, error) {
	if srv, err := s.Get(idOrName); err == nil {
		return srv, nil
	}
	srv, err := s.FindByName(idOrName)
	if err != nil {
		return nil, err
	}
	if srv == nil {
		return nil, fmt.Errorf("Server not found: %s", idOrName)
	}
	return srv, nil
}
