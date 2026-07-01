package router

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCaddyRouterAttachDomainRendersConfigAndReloads(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	executor := &recordingCaddyExecutor{}
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   executor,
	})

	route := Route{AppID: "app_1", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true}
	if err := router.AttachDomain(ctx, route); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}

	config := readText(t, configPath)
	if !strings.Contains(config, "example.com {") {
		t.Fatalf("config missing site block:\n%s", config)
	}
	if !strings.Contains(config, "reverse_proxy web:3000") {
		t.Fatalf("config missing reverse proxy:\n%s", config)
	}
	if strings.Contains(config, "tls ") {
		t.Fatalf("config should rely on Caddy automatic HTTPS defaults:\n%s", config)
	}

	wantCommands := []CaddyCommand{
		{Name: "caddy", Args: []string{"reload", "--config", configPath}},
	}
	if !reflect.DeepEqual(executor.Commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, wantCommands)
	}
}

func TestCaddyRouterPreservesExistingRoutesWhenAddingDomain(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   &recordingCaddyExecutor{},
	})

	if err := router.AttachDomain(ctx, Route{AppID: "app_1", ServiceName: "web", DomainName: "one.example.com", Port: 3000, HTTPS: true}); err != nil {
		t.Fatalf("AttachDomain one: %v", err)
	}
	if err := router.AttachDomain(ctx, Route{AppID: "app_2", ServiceName: "api", DomainName: "two.example.com", Port: 4000, HTTPS: true}); err != nil {
		t.Fatalf("AttachDomain two: %v", err)
	}

	config := readText(t, configPath)
	for _, want := range []string{"one.example.com {", "reverse_proxy web:3000", "two.example.com {", "reverse_proxy api:4000"} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q:\n%s", want, config)
		}
	}
}

func TestCaddyRouterDetachDomainRendersRemainingRoutes(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   &recordingCaddyExecutor{},
	})

	if err := router.AttachDomain(ctx, Route{AppID: "app_1", ServiceName: "web", DomainName: "one.example.com", Port: 3000, HTTPS: true}); err != nil {
		t.Fatalf("AttachDomain one: %v", err)
	}
	if err := router.AttachDomain(ctx, Route{AppID: "app_2", ServiceName: "api", DomainName: "two.example.com", Port: 4000, HTTPS: true}); err != nil {
		t.Fatalf("AttachDomain two: %v", err)
	}
	if err := router.DetachDomain(ctx, "one.example.com"); err != nil {
		t.Fatalf("DetachDomain: %v", err)
	}

	config := readText(t, configPath)
	if strings.Contains(config, "one.example.com") {
		t.Fatalf("detached route remains in config:\n%s", config)
	}
	if !strings.Contains(config, "two.example.com") {
		t.Fatalf("remaining route missing from config:\n%s", config)
	}
}

func TestCaddyRouterReloadErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("reload failed")
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: filepath.Join(t.TempDir(), "Caddyfile"),
		Executor:   &recordingCaddyExecutor{Err: failure},
	})

	err := router.AttachDomain(ctx, Route{DomainName: "example.com", ServiceName: "web", Port: 3000})
	if !errors.Is(err, failure) {
		t.Fatalf("AttachDomain error = %v, want %v", err, failure)
	}
}

type recordingCaddyExecutor struct {
	Commands []CaddyCommand
	Err      error
}

func (r *recordingCaddyExecutor) Run(_ context.Context, command CaddyCommand) error {
	r.Commands = append(r.Commands, command)
	return r.Err
}

func readText(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
