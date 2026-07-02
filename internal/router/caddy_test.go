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
	if !strings.Contains(config, "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("config missing reverse proxy:\n%s", config)
	}
	if strings.Contains(config, "tls ") {
		t.Fatalf("config should rely on Caddy automatic HTTPS defaults:\n%s", config)
	}

	if len(executor.Commands) != 2 {
		t.Fatalf("commands = %#v, want validate and reload", executor.Commands)
	}
	if executor.Commands[0].Name != "caddy" || len(executor.Commands[0].Args) != 3 || executor.Commands[0].Args[0] != "validate" || executor.Commands[0].Args[1] != "--config" {
		t.Fatalf("validate command = %#v", executor.Commands[0])
	}
	if executor.Commands[0].Args[2] == configPath {
		t.Fatalf("validate should use the temporary config before replacing %s", configPath)
	}
	wantReload := CaddyCommand{
		Name: "caddy",
		Args: []string{"reload", "--config", configPath},
	}
	if !reflect.DeepEqual(executor.Commands[1], wantReload) {
		t.Fatalf("reload command = %#v, want %#v", executor.Commands[1], wantReload)
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
	for _, want := range []string{"one.example.com {", "reverse_proxy 127.0.0.1:3000", "two.example.com {", "reverse_proxy 127.0.0.1:4000"} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q:\n%s", want, config)
		}
	}
}

func TestCaddyRouterSyncRoutesReplacesConfigWithRoutes(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   &recordingCaddyExecutor{},
	})

	routes := []Route{
		{AppID: "app_2", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true},
		{AppID: "app_1", ServiceName: "web", DomainName: "www.example.com", Port: 3000, HTTPS: true},
	}
	if err := router.SyncRoutes(ctx, routes); err != nil {
		t.Fatalf("SyncRoutes: %v", err)
	}

	config := readText(t, configPath)
	for _, want := range []string{
		"api.example.com {",
		"reverse_proxy 127.0.0.1:4000",
		"www.example.com {",
		"reverse_proxy 127.0.0.1:3000",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q:\n%s", want, config)
		}
	}
	if strings.Contains(config, "api:4000") || strings.Contains(config, "web:3000") {
		t.Fatalf("config should use host loopback upstreams, not Compose service DNS:\n%s", config)
	}
}

func TestCaddyRouterValidationFailureDoesNotReplaceExistingConfigOrReload(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	oldConfig := "old.example.com {\n\treverse_proxy 127.0.0.1:3000\n}\n"
	if err := os.WriteFile(configPath, []byte(oldConfig), 0o644); err != nil {
		t.Fatalf("WriteFile old config: %v", err)
	}
	failure := errors.New("validate failed")
	executor := &recordingCaddyExecutor{ErrByCommand: map[string]error{"validate": failure}}
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   executor,
	})

	err := router.SyncRoutes(ctx, []Route{{DomainName: "new.example.com", ServiceName: "web", Port: 3000}})
	if !errors.Is(err, failure) {
		t.Fatalf("SyncRoutes error = %v, want %v", err, failure)
	}
	if got := readText(t, configPath); got != oldConfig {
		t.Fatalf("config after validation failure = %q, want %q", got, oldConfig)
	}
	if len(executor.Commands) != 1 || executor.Commands[0].Args[0] != "validate" {
		t.Fatalf("commands after validation failure = %#v, want validate only", executor.Commands)
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
	Commands     []CaddyCommand
	Err          error
	ErrByCommand map[string]error
}

func (r *recordingCaddyExecutor) Run(_ context.Context, command CaddyCommand) error {
	r.Commands = append(r.Commands, command)
	if len(command.Args) > 0 && r.ErrByCommand != nil {
		if err := r.ErrByCommand[command.Args[0]]; err != nil {
			return err
		}
	}
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
