package spapi

import "fmt"

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
	if err := s.client.Get("/sysusers/"+id, &resp); err != nil {
		return nil, err
	}
	if resp.Data.ID == "" {
		//nolint:staticcheck // ST1005: error format is public contract — downstream skills parse it
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
