//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestServerPushComposeBoundaryEndToEnd(t *testing.T) {
	// Given
	paths := setupBootstrappedServerPush(t, "fake")
	ctx := context.Background()
	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	t.Run("standard fields and compose yaml", func(t *testing.T) {
		output, pushErr := pushFilesThroughBootstrappedSSH(t, paths, "standard-app", map[string]string{
			"compose.yaml": "services:\n  web:\n    image: example/web:latest\n    command: [\"./serve\"]\n    labels: {com.example.role: web}\n    networks: [frontend]\n    configs: [app-config]\n    secrets: [app-secret]\n    deploy: {resources: {limits: {memory: 256M}}}\nnetworks: {frontend: {}}\nconfigs: {app-config: {file: ./app.conf}}\nsecrets: {app-secret: {file: ./app.secret}}\n",
		})
		if pushErr != nil {
			t.Fatalf("git push: %v\n%s", pushErr, output)
		}
		assertAppStatus(t, dbPath, "standard-app", app.AppStatusHealthy)
	})

	t.Run("missing compose lists every expected name", func(t *testing.T) {
		output, _ := pushFilesThroughBootstrappedSSH(t, paths, "missing-compose", map[string]string{"README.md": "missing compose\n"})
		for _, want := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
			if !strings.Contains(output, want) {
				t.Fatalf("git push output missing %q:\n%s", want, output)
			}
		}
	})

	t.Run("multiple compose files list conflicts", func(t *testing.T) {
		output, _ := pushFilesThroughBootstrappedSSH(t, paths, "multiple-compose", map[string]string{
			"compose.yaml": "services: {}\n",
			"compose.yml":  "services: {}\n",
		})
		for _, want := range []string{"conflicting files", "compose.yaml", "compose.yml"} {
			if !strings.Contains(output, want) {
				t.Fatalf("git push output missing %q:\n%s", want, output)
			}
		}
	})

	t.Run("anchored external extends is rejected", func(t *testing.T) {
		output, _ := pushFilesThroughBootstrappedSSH(t, paths, "external-compose", map[string]string{
			"compose.yml": "x-service: &external\n  extends:\n    file: shared.compose.yml\n    service: base\nservices:\n  web:\n    <<: *external\n",
		})
		for _, want := range []string{"services.web.extends.file", "shared.compose.yml", "external Compose files are not supported"} {
			if !strings.Contains(output, want) {
				t.Fatalf("git push output missing %q:\n%s", want, output)
			}
		}
	})

	t.Run("invalid app is not normalized into existing app", func(t *testing.T) {
		validOutput, validPushErr := pushFilesThroughBootstrappedSSH(t, paths, "my-app", map[string]string{
			"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
		})
		if validPushErr != nil {
			t.Fatalf("git push valid app: %v\n%s", validPushErr, validOutput)
		}
		output, pushErr := pushFilesThroughBootstrappedSSH(t, paths, "My_App", map[string]string{
			"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
		})
		if pushErr == nil {
			t.Fatalf("git push succeeded, want invalid app rejection:\n%s", output)
		}
		for _, want := range []string{`app name "My_App" is not a normalized DNS label; use "my-app"`, "git remote set-url sshdock git@sshdock.example.com:my-app.git"} {
			if !strings.Contains(output, want) {
				t.Fatalf("git push output missing %q:\n%s", want, output)
			}
		}

		database, openErr := store.OpenSQLite(ctx, dbPath)
		if openErr != nil {
			t.Fatalf("OpenSQLite: %v", openErr)
		}
		t.Cleanup(func() {
			if closeErr := database.Close(); closeErr != nil {
				t.Errorf("Close: %v", closeErr)
			}
		})
		if _, getErr := database.GetApp(ctx, "My_App"); !errors.Is(getErr, store.ErrNotFound) {
			t.Fatalf("GetApp(My_App) error = %v, want ErrNotFound", getErr)
		}
		if _, getErr := database.GetApp(ctx, "my-app"); getErr != nil {
			t.Fatalf("GetApp(my-app): %v", getErr)
		}
	})

	t.Run("valid app cannot collide with legacy runtime identity", func(t *testing.T) {
		database, openErr := store.OpenSQLite(ctx, dbPath)
		if openErr != nil {
			t.Fatalf("OpenSQLite: %v", openErr)
		}
		now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
		if createErr := database.CreateApp(ctx, app.App{
			ID:           "foo.bar",
			Name:         "foo.bar",
			NodeID:       "local",
			RepoPath:     filepath.Join(paths.dataDir, "apps", "foo.bar", "repo.git"),
			WorktreePath: filepath.Join(paths.dataDir, "apps", "foo.bar", "worktree"),
			Status:       app.AppStatusCreated,
			CreatedAt:    now,
			UpdatedAt:    now,
		}); createErr != nil {
			t.Fatalf("CreateApp(foo.bar): %v", createErr)
		}
		if closeErr := database.Close(); closeErr != nil {
			t.Fatalf("Close: %v", closeErr)
		}

		output, pushErr := pushFilesThroughBootstrappedSSH(t, paths, "foo-bar", map[string]string{
			"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
		})
		if pushErr == nil {
			t.Fatalf("git push succeeded, want runtime identity collision rejection:\n%s", output)
		}
		want := `app name "foo-bar" conflicts with existing app "foo.bar" because both use runtime identity "sshdock_foo-bar"; choose another app name`
		if !strings.Contains(output, want) {
			t.Fatalf("git push output missing %q:\n%s", want, output)
		}
	})
}

func pushFilesThroughBootstrappedSSH(t *testing.T, paths serverPushPaths, appName string, files map[string]string) (string, error) {
	t.Helper()
	sourceDir := t.TempDir()
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	for name, content := range files {
		path := filepath.Join(sourceDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	runGit(t, sourceDir, nil, "add", ".")
	runGit(t, sourceDir, nil, "commit", "-m", "exercise Git boundary")
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", paths.sshUser+"@127.0.0.1:"+appName+".git")

	sshPath := requireCommandOrSkip(t, "ssh")
	sshCommand := fmt.Sprintf("%s -p %d -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", sshPath, paths.sshPort, paths.clientKeyPath)
	command := exec.Command("git", "push", "sshdock", "main")
	command.Dir = sourceDir
	command.Env = append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_SSH_COMMAND="+sshCommand,
		"SSHDOCK_DATA_DIR="+paths.dataDir,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
