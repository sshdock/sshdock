package tui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHServerServesDashboardSessionWithAuthorizedKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hostKeyPath := writeTestRSAKey(t, filepath.Join(t.TempDir(), "ssh_host_rsa_key"))
	clientSigner, authorizedKey := newTestClientSigner(t)
	authorizedKeysPath := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(authorizedKeysPath, authorizedKey, 0o600); err != nil {
		t.Fatalf("WriteFile authorized_keys: %v", err)
	}
	listener := listenLocalhost(t)
	server := NewSSHServer(SSHServerConfig{
		ListenAddr:         listener.Addr().String(),
		OperatorUser:       "dashboard",
		HostKeyPath:        hostKeyPath,
		AuthorizedKeysPath: authorizedKeysPath,
		Handler: SessionHandlerFunc(func(_ context.Context, session Session) error {
			if session.User() != "dashboard" {
				t.Fatalf("session user = %q", session.User())
			}
			_, err := fmt.Fprintln(session, "dashboard ok")
			return err
		}),
	})
	runSSHServerForTest(t, ctx, server, listener)

	client := dialTestSSH(t, listener.Addr().String(), "dashboard", clientSigner)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	output, err := session.Output("dashboard")
	if err != nil {
		t.Fatalf("session output: %v", err)
	}
	if strings.TrimSpace(string(output)) != "dashboard ok" {
		t.Fatalf("output = %q", output)
	}
}

func TestSSHServerRejectsWrongOperatorUserOrKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hostKeyPath := writeTestRSAKey(t, filepath.Join(t.TempDir(), "ssh_host_rsa_key"))
	authorizedSigner, authorizedKey := newTestClientSigner(t)
	otherSigner, _ := newTestClientSigner(t)
	authorizedKeysPath := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(authorizedKeysPath, authorizedKey, 0o600); err != nil {
		t.Fatalf("WriteFile authorized_keys: %v", err)
	}
	listener := listenLocalhost(t)
	server := NewSSHServer(SSHServerConfig{
		ListenAddr:         listener.Addr().String(),
		OperatorUser:       "dashboard",
		HostKeyPath:        hostKeyPath,
		AuthorizedKeysPath: authorizedKeysPath,
		Handler:            SessionHandlerFunc(func(context.Context, Session) error { return nil }),
	})
	runSSHServerForTest(t, ctx, server, listener)

	if _, err := dialSSH(listener.Addr().String(), "root", authorizedSigner); err == nil {
		t.Fatal("root user authenticated, want failure")
	}
	if _, err := dialSSH(listener.Addr().String(), "dashboard", otherSigner); err == nil {
		t.Fatal("unauthorized key authenticated, want failure")
	}
}

func TestLoadAuthorizedKeysAllowsMissingOrEmptyFile(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing_authorized_keys")
	keys, err := loadAuthorizedKeys(missingPath)
	if err != nil {
		t.Fatalf("load missing authorized_keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("missing authorized_keys loaded %d keys, want 0", len(keys))
	}

	emptyPath := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0o600); err != nil {
		t.Fatalf("WriteFile empty authorized_keys: %v", err)
	}
	keys, err = loadAuthorizedKeys(emptyPath)
	if err != nil {
		t.Fatalf("load empty authorized_keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("empty authorized_keys loaded %d keys, want 0", len(keys))
	}
}

func runSSHServerForTest(t *testing.T, ctx context.Context, server *SSHServer, listener net.Listener) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- server.ServeListener(ctx, listener)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("SSH server did not stop")
		}
	})
}

func listenLocalhost(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return listener
}

func dialTestSSH(t *testing.T, address string, user string, signer ssh.Signer) *ssh.Client {
	t.Helper()
	client, err := dialSSH(address, user, signer)
	if err != nil {
		t.Fatalf("dial ssh: %v", err)
	}
	return client
}

func dialSSH(address string, user string, signer ssh.Signer) (*ssh.Client, error) {
	return ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
}

func newTestClientSigner(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey: %v", err)
	}
	return signer, ssh.MarshalAuthorizedKey(signer.PublicKey())
}

func writeTestRSAKey(t *testing.T, path string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey RSA: %v", err)
	}
	data := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile host key: %v", err)
	}
	return path
}
