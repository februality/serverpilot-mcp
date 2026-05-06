package spapi

import (
	"net/http"
	"testing"
)

func TestSiteResolver_Resolve(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app_1","name":"myapp","serverid":"srv_1","sysuserid":"sys_1","domains":["example.com"]}]}`))
		case "/servers/srv_1":
			_, _ = w.Write([]byte(`{"data":{"id":"srv_1","name":"web1","lastaddress":"1.2.3.4"}}`))
		case "/sysusers/sys_1":
			_, _ = w.Write([]byte(`{"data":{"id":"sys_1","name":"alice","serverid":"srv_1"}}`))
		default:
			w.WriteHeader(404)
		}
	})
	cache := NewTTLCache(60)
	apps := NewAppsAPI(c, cache)
	servers := NewServersAPI(c, cache)
	sysusers := NewSysUsersAPI(c, cache)
	r := NewSiteResolver(apps, servers, sysusers)

	site, err := r.Resolve("example.com")
	if err != nil {
		t.Fatal(err)
	}
	if site.ServerIP != "1.2.3.4" {
		t.Errorf("ServerIP = %q", site.ServerIP)
	}
	if site.SysUserName != "alice" {
		t.Errorf("SysUserName = %q", site.SysUserName)
	}
	if site.BasePath != "/srv/users/alice" {
		t.Errorf("BasePath = %q", site.BasePath)
	}
	if site.AppPath != "/srv/users/alice/apps/myapp" {
		t.Errorf("AppPath = %q", site.AppPath)
	}
	if site.PublicPath != "/srv/users/alice/apps/myapp/public" {
		t.Errorf("PublicPath = %q", site.PublicPath)
	}
}
