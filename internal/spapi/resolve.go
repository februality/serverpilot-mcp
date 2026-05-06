package spapi

// ResolvedSite is the output of SiteResolver.Resolve.
type ResolvedSite struct {
	AppID       string
	AppName     string
	ServerIP    string
	ServerID    string
	SysUserName string
	SysUserID   string
	Domains     []string
	BasePath    string // /srv/users/USERNAME
	AppPath     string // /srv/users/USERNAME/apps/APPNAME
	PublicPath  string // /srv/users/USERNAME/apps/APPNAME/public
}

// SiteResolver resolves a site identifier (app ID, name, or domain) into the
// concrete server IP, sysuser, and filesystem paths needed by the SSH layer.
type SiteResolver struct {
	apps     *AppsAPI
	servers  *ServersAPI
	sysusers *SysUsersAPI
}

func NewSiteResolver(apps *AppsAPI, servers *ServersAPI, sysusers *SysUsersAPI) *SiteResolver {
	return &SiteResolver{apps: apps, servers: servers, sysusers: sysusers}
}

func (r *SiteResolver) Resolve(siteIdentifier string) (*ResolvedSite, error) {
	app, err := r.apps.Resolve(siteIdentifier)
	if err != nil {
		return nil, err
	}
	server, err := r.servers.Get(app.ServerID)
	if err != nil {
		return nil, err
	}
	sysuser, err := r.sysusers.Get(app.SysUserID)
	if err != nil {
		return nil, err
	}
	basePath := "/srv/users/" + sysuser.Name
	return &ResolvedSite{
		AppID:       app.ID,
		AppName:     app.Name,
		ServerIP:    server.LastAddress,
		ServerID:    server.ID,
		SysUserName: sysuser.Name,
		SysUserID:   sysuser.ID,
		Domains:     app.Domains,
		BasePath:    basePath,
		AppPath:     basePath + "/apps/" + app.Name,
		PublicPath:  basePath + "/apps/" + app.Name + "/public",
	}, nil
}
