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

func TestCaddyRouterSyncRoutesRendersConfigAndReloads(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	executor := &recordingCaddyExecutor{}
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   executor,
	})

	route := Route{AppID: "app_1", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true}
	if err := router.SyncRoutes(ctx, []Route{route}); err != nil {
		t.Fatalf("SyncRoutes: %v", err)
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
	if executor.Commands[0].Name != "caddy" || len(executor.Commands[0].Args) != 5 || executor.Commands[0].Args[0] != "validate" || executor.Commands[0].Args[1] != "--config" || executor.Commands[0].Args[3] != "--adapter" || executor.Commands[0].Args[4] != "caddyfile" {
		t.Fatalf("validate command = %#v", executor.Commands[0])
	}
	if executor.Commands[0].Args[2] == configPath {
		t.Fatalf("validate should use the temporary config before replacing %s", configPath)
	}
	wantReload := CaddyCommand{
		Name: "caddy",
		Args: []string{"reload", "--config", configPath, "--adapter", "caddyfile"},
	}
	if !reflect.DeepEqual(executor.Commands[1], wantReload) {
		t.Fatalf("reload command = %#v, want %#v", executor.Commands[1], wantReload)
	}
}

func TestCaddyRouterReloadsMainConfigAcrossPopulatedAndEmptySnapshots(t *testing.T) {
	for _, test := range []struct {
		name     string
		separate bool
		alias    bool
		admin    string
	}{
		{name: "standalone fallback"},
		{name: "standalone custom admin", admin: "127.0.0.1:22019"},
		{name: "standalone equivalent paths", alias: true, admin: "127.0.0.1:22019"},
		{name: "imported routes", separate: true},
		{name: "main owns custom admin", separate: true, admin: "127.0.0.1:22019"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			generated := filepath.Join(dir, "sshdock.caddyfile")
			mainPath := ""
			mainConfig := ""
			wantReloadPath := generated
			if test.alias {
				mainPath = dir + string(filepath.Separator) + "." + string(filepath.Separator) + "sshdock.caddyfile"
				wantReloadPath = mainPath
			}
			if test.separate {
				mainPath = filepath.Join(dir, "Caddyfile")
				wantReloadPath = mainPath
				if test.admin != "" {
					mainConfig = "{\n\tadmin " + test.admin + "\n}\n"
				}
				mainConfig += ":80 {\n\trespond \"administrator-owned sentinel\"\n}\nimport " + generated + "\n"
				if err := os.WriteFile(mainPath, []byte(mainConfig), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			executor := &recordingCaddyExecutor{}
			router := NewCaddyRouter(CaddyRouterConfig{ConfigPath: generated, MainConfigPath: mainPath, AdminAddress: test.admin, Executor: executor})
			for _, attach := range []bool{true, false} {
				var routes []Route
				if attach {
					routes = []Route{{DomainName: "managed.example.com", Port: 3000}}
				}
				if err := router.SyncRoutes(ctx, routes); err != nil {
					t.Fatal(err)
				}
				fragment := readText(t, generated)
				if strings.Contains(fragment, "managed.example.com") != attach {
					t.Fatalf("attach=%t generated routes:\n%s", attach, fragment)
				}
				wantAdmin := test.admin != "" && !test.separate
				if strings.Contains(fragment, "admin ") != wantAdmin {
					t.Fatalf("generated admin option: got %q, standalone=%t", fragment, !test.separate)
				}
				commands := executor.Commands[len(executor.Commands)-2:]
				if commands[0].Args[0] != "validate" || commands[0].Args[2] == generated || commands[0].Args[2] == mainPath {
					t.Fatalf("fragment must be validated before replacement: %#v", commands[0])
				}
				wantArgs := []string{"reload", "--config", wantReloadPath, "--adapter", "caddyfile"}
				if test.admin != "" {
					wantArgs = append(wantArgs, "--address", test.admin)
				}
				if !reflect.DeepEqual(commands[1].Args, wantArgs) {
					t.Fatalf("reload args = %#v, want %#v", commands[1].Args, wantArgs)
				}
				if test.separate && readText(t, mainPath) != mainConfig {
					t.Fatal("route operation changed the administrator main config")
				}
			}
		})
	}
}

func TestCaddyRouterSyncRoutesReplacesConfigWithRoutes(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(configPath, []byte("old.example.com {\n\treverse_proxy 127.0.0.1:3100\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if strings.Contains(config, "old.example.com") {
		t.Fatalf("previous route survived snapshot replacement:\n%s", config)
	}
	if strings.Contains(config, "api:4000") || strings.Contains(config, "web:3000") {
		t.Fatalf("config should use host loopback upstreams, not Compose service DNS:\n%s", config)
	}
}

func TestCaddyRouterSyncRoutesWithNoRoutesReplacesStaleConfig(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	oldConfig := "old.example.com {\n\treverse_proxy 127.0.0.1:3000\n}\n"
	if err := os.WriteFile(configPath, []byte(oldConfig), 0o644); err != nil {
		t.Fatalf("WriteFile old config: %v", err)
	}
	executor := &recordingCaddyExecutor{}
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: configPath,
		Executor:   executor,
	})

	if err := router.SyncRoutes(ctx, nil); err != nil {
		t.Fatalf("SyncRoutes nil: %v", err)
	}

	config := readText(t, configPath)
	if strings.TrimSpace(config) == "" {
		t.Fatalf("no-route config is empty")
	}
	if strings.Contains(config, "old.example.com") {
		t.Fatalf("stale route remains in no-route config:\n%s", config)
	}
	if len(executor.Commands) != 2 {
		t.Fatalf("commands = %#v, want validate and reload", executor.Commands)
	}
	if executor.Commands[0].Args[0] != "validate" || executor.Commands[1].Args[0] != "reload" {
		t.Fatalf("commands = %#v, want validate then reload", executor.Commands)
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

func TestCaddyRouterReloadErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("reload failed")
	router := NewCaddyRouter(CaddyRouterConfig{
		ConfigPath: filepath.Join(t.TempDir(), "Caddyfile"),
		Executor:   &recordingCaddyExecutor{ErrByCommand: map[string]error{"reload": failure}},
	})

	err := router.SyncRoutes(ctx, []Route{{DomainName: "example.com", ServiceName: "web", Port: 3000}})
	if !errors.Is(err, failure) {
		t.Fatalf("SyncRoutes error = %v, want %v", err, failure)
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
