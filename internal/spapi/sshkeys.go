package spapi

// SSHKeysAPI wraps the /sshkeys and per-sysuser SSH-key endpoints.
// No caching — keys change too often for it to be useful.
type SSHKeysAPI struct {
	client *Client
}

func NewSSHKeysAPI(client *Client) *SSHKeysAPI {
	return &SSHKeysAPI{client: client}
}

func (s *SSHKeysAPI) List() ([]SPSSHKey, error) {
	var resp spListResponse[SPSSHKey]
	if err := s.client.Get("/sshkeys", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (s *SSHKeysAPI) Create(name, publicKey string) (*SPSSHKey, error) {
	var resp spSingleResponse[SPSSHKey]
	body := map[string]any{"name": name, "public_key": publicKey}
	if err := s.client.Post("/sshkeys", body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (s *SSHKeysAPI) Delete(id string) error {
	return s.client.Delete("/sshkeys/"+id, nil)
}

func (s *SSHKeysAPI) AddToSysUser(sshKeyID, sysUserID string) error {
	return s.client.Post("/sysusers/"+sysUserID+"/sshkeys",
		map[string]any{"sshkey_id": sshKeyID}, nil)
}

func (s *SSHKeysAPI) RemoveFromSysUser(sshKeyID, sysUserID string) error {
	return s.client.Delete("/sysusers/"+sysUserID+"/sshkeys/"+sshKeyID, nil)
}

func (s *SSHKeysAPI) ListForSysUser(sysUserID string) ([]SPSSHKey, error) {
	var resp spListResponse[SPSSHKey]
	if err := s.client.Get("/sysusers/"+sysUserID+"/sshkeys", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// FindByName searches /sshkeys for an exact (case-sensitive) name match,
// matching src/api/sshkeys.ts behavior.
func (s *SSHKeysAPI) FindByName(name string) (*SPSSHKey, error) {
	keys, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].Name == name {
			return &keys[i], nil
		}
	}
	return nil, nil
}
