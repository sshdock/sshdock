package tui

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

type SSHServerConfig struct {
	ListenAddr         string
	DashboardUser      string
	HostKeyPath        string
	AuthorizedKeysPath string
	Handler            SessionHandler
}

type SSHServer struct {
	listenAddr         string
	hostKeyPath        string
	authorizedKeysPath string
	server             *Server
}

func NewSSHServer(config SSHServerConfig) *SSHServer {
	return &SSHServer{
		listenAddr:         config.ListenAddr,
		hostKeyPath:        config.HostKeyPath,
		authorizedKeysPath: config.AuthorizedKeysPath,
		server: NewServer(ServerConfig{
			ListenAddr:    config.ListenAddr,
			DashboardUser: config.DashboardUser,
			Handler:       config.Handler,
		}),
	}
}

func (s *SSHServer) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	return s.ServeListener(ctx, listener)
}

func (s *SSHServer) ServeListener(ctx context.Context, listener net.Listener) error {
	signer, err := loadOrCreateHostSigner(s.hostKeyPath)
	if err != nil {
		return fmt.Errorf("load dashboard SSH host key: %w", err)
	}
	authorizedKeys, err := loadAuthorizedKeys(s.authorizedKeysPath)
	if err != nil {
		return fmt.Errorf("load dashboard authorized keys: %w", err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() != s.server.DashboardUser() {
				return nil, fmt.Errorf("unauthorized dashboard user %q", conn.User())
			}
			if !authorizedKeys[string(key.Marshal())] {
				return nil, fmt.Errorf("unauthorized dashboard key for %q", conn.User())
			}
			return &ssh.Permissions{Extensions: map[string]string{"user": conn.User()}}, nil
		},
	}
	config.AddHostKey(signer)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn, config)
	}
}

func (s *SSHServer) handleConn(ctx context.Context, conn net.Conn, config *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ctx, serverConn.User(), channel, requests)
	}
}

func (s *SSHServer) handleSession(ctx context.Context, user string, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "pty-req", "env":
			if request.WantReply {
				_ = request.Reply(true, nil)
			}
		case "shell", "exec":
			if request.WantReply {
				_ = request.Reply(true, nil)
			}
			if err := s.server.AcceptSession(ctx, sshDashboardSession{user: user, writer: channel}); err != nil {
				_, _ = fmt.Fprintln(channel.Stderr(), err)
				sendExitStatus(channel, 1)
				return
			}
			sendExitStatus(channel, 0)
			return
		default:
			if request.WantReply {
				_ = request.Reply(false, nil)
			}
		}
	}
}

func sendExitStatus(channel ssh.Channel, status uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct {
		Status uint32
	}{Status: status}))
}

type sshDashboardSession struct {
	user   string
	writer io.Writer
}

func (s sshDashboardSession) User() string {
	return s.user
}

func (s sshDashboardSession) Write(data []byte) (int, error) {
	return s.writer.Write(data)
}

func loadAuthorizedKeys(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	keys := map[string]bool{}
	remaining := data
	for len(strings.TrimSpace(string(remaining))) > 0 {
		key, _, _, rest, err := ssh.ParseAuthorizedKey(remaining)
		if err != nil {
			return nil, err
		}
		keys[string(key.Marshal())] = true
		remaining = rest
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("dashboard authorized_keys is empty")
	}
	return keys, nil
}

func loadOrCreateHostSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := writeHostKey(path); err != nil {
			return nil, err
		}
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

func writeHostKey(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	data := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return os.WriteFile(path, data, 0o600)
}
