package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferDefaultRoutePrefersWebService(t *testing.T) {
	path := writeRouteCompose(t, `
services:
  api:
    image: example/api
    ports:
      - "4000:80"
  web:
    image: example/web
    ports:
      - "127.0.0.1:3000:80"
`)

	target, ok, reason, err := InferDefaultRoute(path)
	if err != nil {
		t.Fatalf("InferDefaultRoute: %v", err)
	}
	if !ok {
		t.Fatalf("InferDefaultRoute ok = false, reason = %q", reason)
	}
	if target.ServiceName != "web" || target.Port != 3000 {
		t.Fatalf("target = %#v, want web:3000", target)
	}
}

func TestInferDefaultRouteSupportsLongPortSyntax(t *testing.T) {
	path := writeRouteCompose(t, `
services:
  web:
    image: example/web
    ports:
      - published: "3000"
        target: 80
`)

	target, ok, reason, err := InferDefaultRoute(path)
	if err != nil {
		t.Fatalf("InferDefaultRoute: %v", err)
	}
	if !ok {
		t.Fatalf("InferDefaultRoute ok = false, reason = %q", reason)
	}
	if target.ServiceName != "web" || target.Port != 3000 {
		t.Fatalf("target = %#v, want web:3000", target)
	}
}

func TestInferDefaultRouteUsesOnlyPublishedServiceFallback(t *testing.T) {
	path := writeRouteCompose(t, `
services:
  worker:
    image: example/worker
  api:
    image: example/api
    ports:
      - "4000:80"
`)

	target, ok, reason, err := InferDefaultRoute(path)
	if err != nil {
		t.Fatalf("InferDefaultRoute: %v", err)
	}
	if !ok {
		t.Fatalf("InferDefaultRoute ok = false, reason = %q", reason)
	}
	if target.ServiceName != "api" || target.Port != 4000 {
		t.Fatalf("target = %#v, want api:4000", target)
	}
}

func TestInferDefaultRouteSkipsAmbiguousServices(t *testing.T) {
	path := writeRouteCompose(t, `
services:
  api:
    image: example/api
    ports:
      - "4000:80"
  admin:
    image: example/admin
    ports:
      - "5000:80"
`)

	_, ok, reason, err := InferDefaultRoute(path)
	if err != nil {
		t.Fatalf("InferDefaultRoute: %v", err)
	}
	if ok {
		t.Fatal("InferDefaultRoute ok = true, want false")
	}
	if !strings.Contains(reason, "ambiguous") || !strings.Contains(reason, "api") || !strings.Contains(reason, "admin") {
		t.Fatalf("reason = %q, want ambiguous service names", reason)
	}
}

func TestInferDefaultRouteSkipsServicesWithoutPublishedPorts(t *testing.T) {
	path := writeRouteCompose(t, `
services:
  web:
    image: example/web
    expose:
      - "80"
`)

	_, ok, reason, err := InferDefaultRoute(path)
	if err != nil {
		t.Fatalf("InferDefaultRoute: %v", err)
	}
	if ok {
		t.Fatal("InferDefaultRoute ok = true, want false")
	}
	if !strings.Contains(reason, "host-published TCP port") {
		t.Fatalf("reason = %q, want actionable published port guidance", reason)
	}
}

func TestInferDefaultRouteIgnoresUnsafePortShapes(t *testing.T) {
	path := writeRouteCompose(t, `
services:
  web:
    image: example/web
    ports:
      - "3000-3002:80"
      - target: 80
        published: 9000
        protocol: udp
`)

	_, ok, reason, err := InferDefaultRoute(path)
	if err != nil {
		t.Fatalf("InferDefaultRoute: %v", err)
	}
	if ok {
		t.Fatal("InferDefaultRoute ok = true, want false")
	}
	if !strings.Contains(reason, "host-published TCP port") {
		t.Fatalf("reason = %q, want published TCP guidance", reason)
	}
}

func writeRouteCompose(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
