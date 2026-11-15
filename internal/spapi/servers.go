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
	if err := s.client.Get("/servers/"+pathSegment(id), &resp); err != nil {
		return nil, err
	}
	s.cache.Set(key, resp.Data)
	return &resp.Data, nil
}

// Resolve accepts a server ID, name (case-insensitive), or IP address. The
// cached list is checked before a per-ID GET; if the list call fails (or has
// no match) a direct GET by ID is attempted.
func (s *ServersAPI) Resolve(idOrName string) (*SPServer, error) {
	ident := strings.TrimSpace(idOrName)
	list, listErr := s.List()
	if listErr == nil {
		for i := range list {
			if list[i].ID == ident {
				if srv, err := s.Get(ident); err == nil {
					return srv, nil
				}
				return &list[i], nil
			}
		}
		var m []SPServer
		host := NormalizeDomain(ident) // "http://1.2.3.4/", "1.2.3.4:22", "Web1." → bare host
		for i := range list {
			name, ip := list[i].Name, list[i].LastAddress
			if strings.EqualFold(name, ident) || strings.EqualFold(name, host) || (ip != "" && (ip == ident || ip == host)) {
				m = append(m, list[i])
			}
		}
		switch len(m) {
		case 1:
			return &m[0], nil
		case 0:
		default:
			c := make([]string, len(m))
			for i, x := range m {
				c[i] = fmt.Sprintf("%s (id %s, ip %s)", x.Name, x.ID, x.LastAddress)
			}
			return nil, ambiguousError("server", ident, c)
		}
	}
	if ident != "" {
		if srv, err := s.Get(ident); err == nil && srv.ID != "" {
			return srv, nil
		}
	}
	if listErr != nil {
		return nil, listErr
	}
	return nil, fmt.Errorf("Server not found: %s", idOrName)
}
