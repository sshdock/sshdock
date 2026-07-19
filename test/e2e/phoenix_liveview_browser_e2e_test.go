//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type phoenixBrowserAcceptance struct {
	baseURL   string
	sshTarget string
	sshKey    string
	appName   string
	session   string
}

func TestPhoenixLiveViewBrowserEndToEnd(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("SSHDOCK_E2E_PHOENIX_URL"), "/")
	if baseURL == "" {
		t.Skip("set SSHDOCK_E2E_PHOENIX_URL to run the external Phoenix LiveView browser test")
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		t.Fatalf("SSHDOCK_E2E_PHOENIX_URL must be an absolute HTTPS URL, got %q", baseURL)
	}
	sshTarget := os.Getenv("SSHDOCK_E2E_PHOENIX_SSH_TARGET")
	sshKey := os.Getenv("SSHDOCK_E2E_PHOENIX_SSH_KEY")
	if sshTarget == "" || sshKey == "" {
		t.Fatal("SSHDOCK_E2E_PHOENIX_SSH_TARGET and SSHDOCK_E2E_PHOENIX_SSH_KEY are required")
	}
	if _, err := os.Stat(sshKey); err != nil {
		t.Fatalf("stat SSHDOCK_E2E_PHOENIX_SSH_KEY: %v", err)
	}
	appName := os.Getenv("SSHDOCK_E2E_PHOENIX_APP")
	if appName == "" {
		appName = "phoenix"
	}
	acceptance := phoenixBrowserAcceptance{
		baseURL:   baseURL,
		sshTarget: sshTarget,
		sshKey:    sshKey,
		appName:   appName,
		session:   fmt.Sprintf("sshdock-phoenix-%d", os.Getpid()),
	}
	t.Cleanup(func() {
		_, _ = runAgentBrowser(acceptance, "close")
	})

	// Given an HTTPS LiveView page with an active secure WebSocket.
	runAgentBrowserRequired(t, acceptance, "open", acceptance.baseURL+"/items")
	runAgentBrowserRequired(t, acceptance, "wait", "--text", "Listing Items")
	firstItem := fmt.Sprintf("before-restart-%d", os.Getpid())
	createPhoenixItem(t, acceptance, firstItem)
	watcher := `(() => {
  const endpoint = window.liveSocket?.socket?.endPointURL();
  if (!window.liveSocket?.isConnected() || !endpoint?.startsWith("wss://")) {
    throw new Error("LiveView secure WebSocket is not connected");
  }
  window.__sshdockReconnect = new Promise((resolve, reject) => {
    const started = Date.now();
    let disconnected = false;
    const observe = () => {
      const connected = Boolean(window.liveSocket?.isConnected());
      disconnected = disconnected || !connected;
      if (disconnected && connected) return resolve("reconnected");
      if (Date.now() - started > 45000) return reject(new Error("LiveView did not reconnect"));
      setTimeout(observe, 100);
    };
    observe();
  });
  return endpoint;
})()`
	endpoint := runAgentBrowserRequired(t, acceptance, "eval", "-b", base64.StdEncoding.EncodeToString([]byte(watcher)))
	if !strings.Contains(endpoint, "wss://") {
		t.Fatalf("LiveView endpoint = %q, want secure WebSocket", endpoint)
	}

	// When the app restarts through the restricted SSH operator surface.
	restartPhoenixApp(t, acceptance)
	reconnected := runAgentBrowserRequired(t, acceptance, "eval", "window.__sshdockReconnect")
	if !strings.Contains(reconnected, "reconnected") {
		t.Fatalf("reconnect result = %q, want reconnected", reconnected)
	}

	// Then the same browser session submits another LiveView update and retains both records.
	secondItem := fmt.Sprintf("after-restart-%d", os.Getpid())
	createPhoenixItem(t, acceptance, secondItem)
	runAgentBrowserRequired(t, acceptance, "open", acceptance.baseURL+"/items")
	runAgentBrowserRequired(t, acceptance, "wait", "--text", firstItem)
	runAgentBrowserRequired(t, acceptance, "wait", "--text", secondItem)
}

func createPhoenixItem(t *testing.T, acceptance phoenixBrowserAcceptance, itemName string) {
	t.Helper()
	runAgentBrowserRequired(t, acceptance, "open", acceptance.baseURL+"/items")
	runAgentBrowserRequired(t, acceptance, "click", `a[href="/items/new"]`)
	runAgentBrowserRequired(t, acceptance, "wait", "--url", "**/items/new")
	runAgentBrowserRequired(t, acceptance, "find", "label", "Name", "fill", itemName)
	runAgentBrowserRequired(t, acceptance, "click", `button[type="submit"]`)
	runAgentBrowserRequired(t, acceptance, "wait", "--text", itemName)
}

func restartPhoenixApp(t *testing.T, acceptance phoenixBrowserAcceptance) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ssh", "-T", "-i", acceptance.sshKey, "-o", "BatchMode=yes", acceptance.sshTarget, "apps", "restart", acceptance.appName)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("restricted SSH restart: %v\n%s", err, output)
	}
}

func runAgentBrowserRequired(t *testing.T, acceptance phoenixBrowserAcceptance, args ...string) string {
	t.Helper()
	output, err := runAgentBrowser(acceptance, args...)
	if err != nil {
		t.Fatalf("agent-browser %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func runAgentBrowser(acceptance phoenixBrowserAcceptance, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	commandArgs := append([]string{"--session", acceptance.session}, args...)
	command := exec.CommandContext(ctx, "agent-browser", commandArgs...)
	output, err := command.CombinedOutput()
	return string(output), err
}
