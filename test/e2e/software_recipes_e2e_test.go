//go:build e2e

package e2e

import (
	"bytes"
	"encoding/xml"
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
	wordpressCurrentImage  = "wordpress:7.0.1-php8.3-apache@sha256:d40b86dbdfcfad808a2029acf6543c670c4a61c29f70b9d24605e7d0b31ab83d"
	wordpressPreviousImage = "wordpress:7.0.1-php8.2-apache@sha256:bfc320ed4f02dd3939186b8020de64203a48a939d6dedcf44cb92cf2368923f5"
)

func TestWordPressSoftwareRecipeDockerEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run the WordPress software recipe test")
	}
	requireDocker(t)

	// Given the public recipe running from the previous supported Apache variant.
	t.Setenv("WORDPRESS_DB_NAME", "wordpress")
	t.Setenv("WORDPRESS_DB_USER", "wordpress")
	t.Setenv("WORDPRESS_DB_PASSWORD", "wordpress-recipe-e2e-password")
	t.Setenv("WORDPRESS_DB_ROOT_PASSWORD", "wordpress-recipe-e2e-root-password")
	recipeDir, err := filepath.Abs(filepath.Join("..", "..", "examples", "software", "wordpress"))
	if err != nil {
		t.Fatalf("Abs WordPress recipe directory: %v", err)
	}
	currentCompose, err := os.ReadFile(filepath.Join(recipeDir, "compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile WordPress compose: %v", err)
	}
	previousCompose := strings.Replace(string(currentCompose), wordpressCurrentImage, wordpressPreviousImage, 1)
	if previousCompose == string(currentCompose) {
		t.Fatal("WordPress compose does not contain the current image pin")
	}
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	if err := os.WriteFile(composePath, []byte(previousCompose), 0o644); err != nil {
		t.Fatalf("WriteFile previous WordPress compose: %v", err)
	}
	projectName := compose.ProjectName("wordpress-recipe-e2e")
	t.Cleanup(func() {
		_ = runCommandNoFail(projectDir, nil, "docker", "compose", "-p", projectName, "down", "-v", "--remove-orphans")
	})
	runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "up", "--wait")

	client := &http.Client{Timeout: 20 * time.Second}
	installURL := "http://127.0.0.1:18200/wp-admin/install.php"
	request, err := http.NewRequest(http.MethodGet, installURL+"?step=1&language=en_US", nil)
	if err != nil {
		t.Fatalf("NewRequest WordPress installer: %v", err)
	}
	status, body := doWordPressRequest(t, client, request)
	if status != http.StatusOK || !strings.Contains(body, "Information needed") {
		t.Fatalf("installer response status = %d, body missing installation form", status)
	}

	// When first-run setup completes and publishes representative content.
	form := url.Values{
		"admin_email":     {"admin@example.com"},
		"admin_password":  {"WordPress-Acceptance-2026!"},
		"admin_password2": {"WordPress-Acceptance-2026!"},
		"blog_public":     {"0"},
		"language":        {"en_US"},
		"Submit":          {"Install WordPress"},
		"user_name":       {"admin"},
		"weblog_title":    {"SSHDock WordPress"},
	}
	request, err = http.NewRequest(http.MethodPost, installURL+"?step=2", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest WordPress setup: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, body = doWordPressRequest(t, client, request)
	if status != http.StatusOK || !strings.Contains(body, "Success!") {
		t.Fatalf("setup response status = %d, body missing success", status)
	}

	xmlRPC := `<?xml version="1.0"?>
<methodCall><methodName>wp.newPost</methodName><params>
<param><value><int>0</int></value></param>
<param><value><string>admin</string></value></param>
<param><value><string>WordPress-Acceptance-2026!</string></value></param>
<param><value><struct>
<member><name>post_type</name><value><string>post</string></value></member>
<member><name>post_status</name><value><string>publish</string></value></member>
<member><name>post_title</name><value><string>SSHDock persistence</string></value></member>
<member><name>post_content</name><value><string>Content survives an exact image update.</string></value></member>
</struct></value></param>
</params></methodCall>`
	request, err = http.NewRequest(http.MethodPost, "http://127.0.0.1:18200/xmlrpc.php", bytes.NewBufferString(xmlRPC))
	if err != nil {
		t.Fatalf("NewRequest WordPress XML-RPC: %v", err)
	}
	request.Header.Set("Content-Type", "text/xml")
	status, body = doWordPressRequest(t, client, request)
	var postResponse struct {
		PostID string    `xml:"params>param>value>string"`
		Fault  *struct{} `xml:"fault"`
	}
	if err := xml.Unmarshal([]byte(body), &postResponse); err != nil {
		t.Fatalf("parse post response: %v", err)
	}
	if status != http.StatusOK || postResponse.Fault != nil || postResponse.PostID == "" {
		t.Fatalf("post response status = %d, body missing post identifier: %s", status, body)
	}

	// Then the public WordPress surface exposes the representative content.
	request, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:18200", nil)
	if err != nil {
		t.Fatalf("NewRequest WordPress home: %v", err)
	}
	status, body = doWordPressRequest(t, client, request)
	for _, want := range []string{"SSHDock WordPress", "SSHDock persistence"} {
		if status != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("home response status = %d, body missing %q", status, want)
		}
	}

	// When Compose applies the current exact image pin over the same named volumes.
	if err := os.WriteFile(composePath, currentCompose, 0o644); err != nil {
		t.Fatalf("WriteFile current WordPress compose: %v", err)
	}
	runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "up", "--pull", "always", "--wait")

	// Then content remains public and the running service uses the selected image.
	request, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:18200", nil)
	if err != nil {
		t.Fatalf("NewRequest upgraded WordPress home: %v", err)
	}
	status, body = doWordPressRequest(t, client, request)
	for _, want := range []string{"SSHDock WordPress", "SSHDock persistence"} {
		if status != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("upgraded home status = %d, body missing %q", status, want)
		}
	}
	containerID := strings.TrimSpace(runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "ps", "-q", "web"))
	image := strings.TrimSpace(runCommand(t, projectDir, nil, "docker", "inspect", "--format", "{{.Config.Image}}", containerID))
	if image != wordpressCurrentImage {
		t.Fatalf("running image = %q, want %q", image, wordpressCurrentImage)
	}
}

func doWordPressRequest(t *testing.T, client *http.Client, request *http.Request) (int, string) {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.URL, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read %s %s: %v", request.Method, request.URL, err)
	}
	return response.StatusCode, string(body)
}
