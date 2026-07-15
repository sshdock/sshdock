package tui

import (
	"context"
	"errors"
	"testing"
)

func TestNewServerUsesConfig(t *testing.T) {
	handler := SessionHandlerFunc(func(context.Context, Session) error { return nil })
	server := NewServer(ServerConfig{
		ListenAddr:   "127.0.0.1:2222",
		OperatorUser: "dashboard",
		Handler:      handler,
	})

	if server.ListenAddr() != "127.0.0.1:2222" {
		t.Fatalf("ListenAddr = %q", server.ListenAddr())
	}
	if server.OperatorUser() != "dashboard" {
		t.Fatalf("OperatorUser = %q", server.OperatorUser())
	}
}

func TestServerAcceptSessionCallsHandlerForOperatorUser(t *testing.T) {
	ctx := context.Background()
	session := fakeSession{user: "dashboard"}
	var called bool
	server := NewServer(ServerConfig{
		ListenAddr:   ":2222",
		OperatorUser: "dashboard",
		Handler: SessionHandlerFunc(func(_ context.Context, got Session) error {
			called = true
			if got.User() != "dashboard" {
				t.Fatalf("session user = %q", got.User())
			}
			return nil
		}),
	})

	if err := server.AcceptSession(ctx, session); err != nil {
		t.Fatalf("AcceptSession: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestServerRejectsNonOperatorUser(t *testing.T) {
	ctx := context.Background()
	server := NewServer(ServerConfig{
		ListenAddr:   ":2222",
		OperatorUser: "dashboard",
		Handler:      SessionHandlerFunc(func(context.Context, Session) error { return nil }),
	})

	err := server.AcceptSession(ctx, fakeSession{user: "root"})
	if err == nil {
		t.Fatal("AcceptSession error = nil, want error")
	}
}

func TestServerReturnsHandlerError(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("handler failed")
	server := NewServer(ServerConfig{
		ListenAddr:   ":2222",
		OperatorUser: "dashboard",
		Handler: SessionHandlerFunc(func(context.Context, Session) error {
			return failure
		}),
	})

	err := server.AcceptSession(ctx, fakeSession{user: "dashboard"})
	if !errors.Is(err, failure) {
		t.Fatalf("AcceptSession error = %v, want %v", err, failure)
	}
}

type fakeSession struct {
	user string
}

func (f fakeSession) Write(data []byte) (int, error) {
	return len(data), nil
}

func (f fakeSession) User() string {
	return f.user
}
