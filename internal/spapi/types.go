package spapi

type SPServer struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Autoupdates        bool     `json:"autoupdates"`
	Firewall           bool     `json:"firewall"`
	DenyUnknownDomains bool     `json:"deny_unknown_domains"`
	LastAddress        string   `json:"lastaddress"`
	LastConn           string   `json:"lastconn"`
	DateCreated        int64    `json:"datecreated"`
	Plan               string   `json:"plan"`
	AvailableRuntimes  []string `json:"available_runtimes"`
}

type SPSSL struct {
	Key     string  `json:"key"`
	Cert    string  `json:"cert"`
	CACerts *string `json:"cacerts"`
	Auto    bool    `json:"auto"`
	Force   bool    `json:"force"`
}

type SPWordPress struct {
	SiteTitle     string `json:"site_title"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password,omitempty"`
	AdminEmail    string `json:"admin_email"`
	LoginURL      string `json:"login_url"`
}

type SPApp struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	SysUserID   string       `json:"sysuserid"`
	ServerID    string       `json:"serverid"`
	Runtime     string       `json:"runtime"`
	SSL         *SPSSL       `json:"ssl"`
	AutoSSL     bool         `json:"autossl"`
	Domains     []string     `json:"domains"`
	DateCreated int64        `json:"datecreated"`
	WordPress   *SPWordPress `json:"wordpress"`
}

type SPSysUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ServerID string `json:"serverid"`
}

type SPDatabaseUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Password string `json:"password,omitempty"`
}

type SPDatabase struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	AppID    string         `json:"appid"`
	ServerID string         `json:"serverid"`
	User     SPDatabaseUser `json:"user"`
}

type SPSSHKey struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	SHA256Fingerprint string `json:"sha256_fingerprint"`
	DateCreated       string `json:"date_created"`
}

type SPActionResponse struct {
	ActionID string         `json:"actionid"`
	Data     map[string]any `json:"data"`
}

type spListResponse[T any] struct {
	Data []T `json:"data"`
}

type spSingleResponse[T any] struct {
	Data T `json:"data"`
}
