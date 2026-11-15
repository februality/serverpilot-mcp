package spapi

import (
	"fmt"
	"strings"
)

type SysUsersAPI struct {
	client *Client
	cache  *TTLCache
}

func NewSysUsersAPI(client *Client, cache *TTLCache) *SysUsersAPI {
	return &SysUsersAPI{client: client, cache: cache}
}

func (s *SysUsersAPI) List() ([]SPSysUser, error) {
	if v, ok := s.cache.Get("sysusers"); ok {
		return v.([]SPSysUser), nil
	}
	var resp spListResponse[SPSysUser]
	if err := s.client.Get("/sysusers", &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		resp.Data = []SPSysUser{}
	}
	s.cache.Set("sysusers", resp.Data)
	return resp.Data, nil
}

func (s *SysUsersAPI) Get(id string) (*SPSysUser, error) {
	key := "sysuser:" + id
	if v, ok := s.cache.Get(key); ok {
		u := v.(SPSysUser)
		return &u, nil
	}
	var resp spSingleResponse[SPSysUser]
	if err := s.client.Get("/sysusers/"+pathSegment(id), &resp); err != nil {
		return nil, err
	}
	if resp.Data.ID == "" {
		return nil, fmt.Errorf("System user %s not found", id)
	}
	s.cache.Set(key, resp.Data)
	return &resp.Data, nil
}

func (s *SysUsersAPI) ListByServer(serverID string) ([]SPSysUser, error) {
	users, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]SPSysUser, 0, len(users))
	for _, u := range users {
		if u.ServerID == serverID {
			out = append(out, u)
		}
	}
	return out, nil
}

// Resolve tries ID first, then name within the given server. Names are not
// unique across servers, so a server scope is required.
func (s *SysUsersAPI) Resolve(idOrName, serverID string) (*SPSysUser, error) {
	if u, err := s.Get(idOrName); err == nil && u.ServerID == serverID {
		return u, nil
	}
	users, err := s.ListByServer(serverID)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(idOrName)
	for i := range users {
		if strings.ToLower(users[i].Name) == target {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("System user not found on server: %s", idOrName)
}

// ResolveAny accepts a system user ID or name across all servers. Names like
// "serverpilot" exist on every server, so a name that matches more than one
// user returns an error listing each candidate's ID and server.
func (s *SysUsersAPI) ResolveAny(idOrName string) (*SPSysUser, error) {
	ident := strings.TrimSpace(idOrName)
	users, listErr := s.List()
	if listErr == nil {
		var m []SPSysUser
		for i := range users {
			if users[i].ID == ident {
				return &users[i], nil
			}
			if strings.EqualFold(users[i].Name, ident) {
				m = append(m, users[i])
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
			return nil, ambiguousError("system user", ident, c)
		}
	}
	if ident != "" {
		if u, err := s.Get(ident); err == nil {
			return u, nil
		}
	}
	if listErr != nil {
		return nil, listErr
	}
	return nil, fmt.Errorf("System user %s not found", idOrName)
}
