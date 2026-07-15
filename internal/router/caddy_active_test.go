package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCaddyRouterRoutesReadActiveAdminConfigAcrossInstances(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/config/" {
			t.Fatalf("request path = %q, want /config/", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"apps":{"http":{"servers":{
				"local":{"listen":[":28888"],"automatic_https":{"skip":["127.0.0.1"]},"routes":[{
					"match":[{"host":["127.0.0.1"]}],
					"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"127.0.0.1:3000"}]}]}]}]
				}]},
				"public":{"listen":[":443"],"routes":[{
					"match":[{"host":["example.com"]}],
					"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"127.0.0.1:4000"}]}]}]}]
				}]}
			}}}
		}`))
	}))
	t.Cleanup(admin.Close)

	activeRouter := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath:   filepath.Join(t.TempDir(), "Caddyfile"),
		AdminAddress: strings.TrimPrefix(admin.URL, "http://"),
	})
	routes, err := activeRouter.Routes(context.Background())
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	want := []Route{
		{DomainName: "example.com", Port: 4000, HTTPS: true},
		{DomainName: "http://127.0.0.1:28888", Port: 3000},
	}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("Routes = %#v, want %#v", routes, want)
	}
}
