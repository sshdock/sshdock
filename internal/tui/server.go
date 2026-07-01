package tui

import (
	"context"
	"fmt"
)

type Session interface {
	User() string
}

type SessionHandler interface {
	HandleSession(ctx context.Context, session Session) error
}

type SessionHandlerFunc func(ctx context.Context, session Session) error

func (f SessionHandlerFunc) HandleSession(ctx context.Context, session Session) error {
	return f(ctx, session)
}

type ServerConfig struct {
	ListenAddr    string
	DashboardUser string
	Handler       SessionHandler
}

type Server struct {
	listenAddr    string
	dashboardUser string
	handler       SessionHandler
}

func NewServer(config ServerConfig) *Server {
	return &Server{
		listenAddr:    config.ListenAddr,
		dashboardUser: config.DashboardUser,
		handler:       config.Handler,
	}
}

func (s *Server) ListenAddr() string {
	return s.listenAddr
}

func (s *Server) DashboardUser() string {
	return s.dashboardUser
}

func (s *Server) AcceptSession(ctx context.Context, session Session) error {
	if session.User() != s.dashboardUser {
		return fmt.Errorf("unauthorized dashboard user %q", session.User())
	}
	if s.handler == nil {
		return fmt.Errorf("dashboard session handler is not configured")
	}

	return s.handler.HandleSession(ctx, session)
}
