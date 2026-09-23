package compose

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployBindsEveryComposeStageToItsOwnCommit(t *testing.T) {
	t.Setenv(GitSHAEnv, "process-spoof")
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "services:\n  web:\n    image: registry.example/app:${SSHDOCK_GIT_SHA:?deploy a commit}\n")
	writeFile(t, filepath.Join(projectDir, ".env"), GitSHAEnv+"=dotenv-spoof\n")
	env := map[string]string{GitSHAEnv: "config-spoof", "TOKEN": "secret-value"}

	for _, sha := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		executor := &recordingExecutor{Outputs: []string{`{"services":{"web":{"image":"registry.example/app:` + sha + `"}}}`}}
		_, err := NewDockerRunner(executor).Deploy(context.Background(), DeployRequest{
			AppName: "external-app", ProjectDir: projectDir, ComposePath: composePath, CommitSHA: sha, Env: env,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(executor.Commands) != 4 {
			t.Fatalf("commands = %d, want config/pull/build/up", len(executor.Commands))
		}
		for _, command := range executor.Commands {
			resolved, err := interpolationEnvironment(composePath, command.Env)
			if err != nil || resolved[GitSHAEnv] != sha || resolved["TOKEN"] != "secret-value" {
				t.Fatalf("command environment = %#v, error = %v", resolved, err)
			}
		}
	}
	if env[GitSHAEnv] != "config-spoof" {
		t.Fatal("deploy mutated the shared app-config map")
	}

	// Missing authoritative metadata cannot fall back to a process/.env/config tag.
	executor := &recordingExecutor{}
	_, err := NewDockerRunner(executor).Deploy(context.Background(), DeployRequest{
		AppName: "external-app", ProjectDir: projectDir, ComposePath: composePath, Env: env,
	})
	if err == nil || !strings.Contains(err.Error(), GitSHAEnv) || len(executor.Commands) != 0 {
		t.Fatalf("missing commit error = %v, commands = %#v", err, executor.Commands)
	}
}
