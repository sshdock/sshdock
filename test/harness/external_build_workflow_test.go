package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExternalBuildWorkflowRejectsAcceptedButUnhealthyDeployment(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct{ Name, Run string }
		}
	}
	content := readTextFile(t, filepath.Join(repoRoot(t), "examples/labs/external-build/deploy.yml"))
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	for _, step := range workflow.Jobs["deploy"].Steps {
		if step.Name == "Push the exact commit and verify deployment" {
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("missing deployment verification step")
	}
	bin := t.TempDir()
	for name, content := range map[string]string{
		"git":  "#!/bin/sh\nexit 0\n", // Ref acceptance alone cannot prove success.
		"curl": "#!/bin/sh\nprintf '%s' \"$HTTP_BODY\"\n",
		"ssh": `#!/bin/sh
case "$*" in
  *' deployments logs '*) printf 'deploy: terminal\n' ;;
  *' apps health '*) printf 'health: %s\ncurrent main: %s\nlatest deploy: dep_1 %s commit=%s trigger=push\n' "$HEALTH" "$ACTUAL_SHA" "$STATUS" "$ACTUAL_SHA" ;;
  *' apps exec '*) printf '%s\n' "$ACTUAL_SHA" ;;
  *) exit 91 ;;
esac
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sha := strings.Repeat("a", 40)
	for _, test := range []struct {
		name, status, actualSHA, health, body string
		wantSuccess                           bool
	}{
		{"healthy exact revision", "succeeded", sha, "ok", "pong", true},
		{"failed attempt still serving HTTP", "failed", sha, "ok", "pong", false},
		{"older revision still serving HTTP", "succeeded", strings.Repeat("b", 40), "ok", "pong", false},
		{"unhealthy services", "succeeded", sha, "fail", "pong", false},
		{"wrong public response", "succeeded", sha, "ok", "wrong", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			runnerDir := t.TempDir()
			command := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"RUNNER_TEMP="+runnerDir, "DEPLOY_KEY=fixture-key", "KNOWN_HOSTS=fixture-host", "SSHDOCK_HOST=server.example.com", "SSHDOCK_APP=gin-external", "SSHDOCK_HEALTH_URL=https://gin.example.com/ping",
				"GITHUB_SHA="+sha, "STATUS="+test.status, "ACTUAL_SHA="+test.actualSHA, "HEALTH="+test.health, "HTTP_BODY="+test.body)
			output, err := command.CombinedOutput()
			if (err == nil) != test.wantSuccess {
				t.Fatalf("result=%v, want success=%v:\n%s", err, test.wantSuccess, output)
			}
			if _, err := os.Stat(filepath.Join(runnerDir, "sshdock-key")); !os.IsNotExist(err) {
				t.Fatalf("deploy key was not removed: %v", err)
			}
		})
	}
}
