//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

const (
	giteaCurrentImage  = "docker.gitea.com/gitea:1.27.0-rootless@sha256:414ba5b2b1163480e9ed4213a989cd798579cfa88582a2359303273009b2b852"
	giteaPreviousImage = "docker.gitea.com/gitea:1.26.4-rootless@sha256:cd1d2614b403fc9b085fa52ceb4424dde9c4dcf5da8e3263abb27955562070c4"
)

func TestGiteaSoftwareRecipeDockerEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run the Gitea software recipe test")
	}
	requireDocker(t)
	requireGit(t)
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")

	// Given the public recipe running from the previous supported rootless release.
	t.Setenv("GITEA_DOMAIN", "127.0.0.1")
	t.Setenv("GITEA_ROOT_URL", "http://127.0.0.1:18201/")
	t.Setenv("GITEA_SECRET_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("GITEA_INTERNAL_TOKEN", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
	recipeDir, err := filepath.Abs(filepath.Join("..", "..", "examples", "software", "gitea"))
	if err != nil {
		t.Fatalf("Abs Gitea recipe directory: %v", err)
	}
	currentCompose, err := os.ReadFile(filepath.Join(recipeDir, "compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile Gitea compose: %v", err)
	}
	previousCompose := strings.Replace(string(currentCompose), giteaCurrentImage, giteaPreviousImage, 1)
	if previousCompose == string(currentCompose) {
		t.Fatal("Gitea compose does not contain the current image pin")
	}
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	if err := os.WriteFile(composePath, []byte(previousCompose), 0o644); err != nil {
		t.Fatalf("WriteFile previous Gitea compose: %v", err)
	}
	projectName := compose.ProjectName("gitea-recipe-e2e")
	t.Cleanup(func() {
		_ = runCommandNoFail(projectDir, nil, "docker", "compose", "-p", projectName, "down", "-v", "--remove-orphans")
	})
	runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "up", "--wait")

	client := &http.Client{Timeout: 20 * time.Second}
	installGitea(t, client)
	waitForGiteaInstalled(t, client)
	password := "Gitea-Acceptance-2026!"
	createGiteaRepository(t, client, password)

	keyPath := filepath.Join(t.TempDir(), "gitea_ed25519")
	runCommand(t, "", nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", keyPath)
	publicKey, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("ReadFile Gitea public key: %v", err)
	}
	addGiteaSSHKey(t, client, password, string(publicKey))

	// When a real repository is pushed and cloned through Gitea's SSH service.
	remoteURL := "ssh://git@127.0.0.1:18222/acceptance/recipe-proof.git"
	sshCommand := fmt.Sprintf("%s -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", sshPath, keyPath)
	gitEnv := append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
	sourceDir := t.TempDir()
	runGit(t, sourceDir, nil, "init", "-b", "main")
	runGit(t, sourceDir, nil, "config", "user.email", "acceptance@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Acceptance")
	if err := os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("Gitea persistence through SSHDock\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Gitea repository README: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "README.md")
	runGit(t, sourceDir, nil, "commit", "-m", "Prove Gitea Git")
	runGit(t, sourceDir, nil, "remote", "add", "origin", remoteURL)
	runGit(t, sourceDir, gitEnv, "push", "-u", "origin", "main")
	assertGiteaClone(t, gitEnv, remoteURL)

	// When Compose applies the current exact rootless image over the same named volumes.
	if err := os.WriteFile(composePath, currentCompose, 0o644); err != nil {
		t.Fatalf("WriteFile current Gitea compose: %v", err)
	}
	runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "up", "--pull", "always", "--wait")

	// Then the repository survives and the running service uses the selected image.
	assertGiteaClone(t, gitEnv, remoteURL)
	containerID := strings.TrimSpace(runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "ps", "-q", "web"))
	image := strings.TrimSpace(runCommand(t, projectDir, nil, "docker", "inspect", "--format", "{{.Config.Image}}", containerID))
	if image != giteaCurrentImage {
		t.Fatalf("running image = %q, want %q", image, giteaCurrentImage)
	}
}

func installGitea(t *testing.T, client *http.Client) {
	t.Helper()
	form := url.Values{
		"db_type":              {"sqlite3"},
		"db_path":              {"/var/lib/gitea/data/gitea.db"},
		"app_name":             {"SSHDock Gitea"},
		"repo_root_path":       {"/var/lib/gitea/git/repositories"},
		"lfs_root_path":        {"/var/lib/gitea/data/lfs"},
		"run_user":             {"git"},
		"domain":               {"127.0.0.1"},
		"ssh_port":             {"18222"},
		"http_port":            {"3000"},
		"app_url":              {"http://127.0.0.1:18201/"},
		"log_root_path":        {"/var/lib/gitea/log"},
		"disable_registration": {"on"},
		"admin_name":           {"acceptance"},
		"admin_email":          {"acceptance@example.com"},
		"admin_passwd":         {"Gitea-Acceptance-2026!"},
		"admin_confirm_passwd": {"Gitea-Acceptance-2026!"},
	}
	response, err := client.PostForm("http://127.0.0.1:18201/", form)
	if err != nil {
		t.Fatalf("install Gitea: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		t.Fatalf("install Gitea status = %d: %s", response.StatusCode, body)
	}
}

func waitForGiteaInstalled(t *testing.T, client *http.Client) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	var lastStatus int
	for time.Now().Before(deadline) {
		response, err := client.Get("http://127.0.0.1:18201/api/v1/version")
		lastErr = err
		if err == nil {
			lastStatus = response.StatusCode
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for installed Gitea API: status=%d err=%v", lastStatus, lastErr)
}

func createGiteaRepository(t *testing.T, client *http.Client, password string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"name": "recipe-proof", "private": false})
	if err != nil {
		t.Fatalf("Marshal Gitea repository request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:18201/api/v1/user/repos", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest create Gitea repository: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("acceptance", password)
	status, body := doSoftwareRecipeRequest(t, client, request)
	if status != http.StatusCreated || !strings.Contains(body, `"name":"recipe-proof"`) {
		t.Fatalf("create Gitea repository status = %d: %s", status, body)
	}
}

func addGiteaSSHKey(t *testing.T, client *http.Client, password, publicKey string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"title": "acceptance", "key": strings.TrimSpace(publicKey)})
	if err != nil {
		t.Fatalf("Marshal Gitea SSH key request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:18201/api/v1/user/keys", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest add Gitea SSH key: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("acceptance", password)
	status, body := doSoftwareRecipeRequest(t, client, request)
	if status != http.StatusCreated {
		t.Fatalf("add Gitea SSH key status = %d: %s", status, body)
	}
}

func assertGiteaClone(t *testing.T, gitEnv []string, remoteURL string) {
	t.Helper()
	cloneDir := filepath.Join(t.TempDir(), "recipe-proof")
	runGit(t, "", gitEnv, "clone", remoteURL, cloneDir)
	contents, err := os.ReadFile(filepath.Join(cloneDir, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile cloned Gitea README: %v", err)
	}
	if string(contents) != "Gitea persistence through SSHDock\n" {
		t.Fatalf("cloned Gitea README = %q", contents)
	}
}
