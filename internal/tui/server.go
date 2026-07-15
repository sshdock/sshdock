package tui

import (
	"context"
	"fmt"
	"io"
)

type Session interface {
	io.Writer
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
	ListenAddr   string
	OperatorUser string
	Handler      SessionHandler
}

type Server struct {
	listenAddr   string
	operatorUser string
	handler      SessionHandler
}

func NewServer(config ServerConfig) *Server {
	return &Server{
		listenAddr:   config.ListenAddr,
		operatorUser: config.OperatorUser,
		handler:      config.Handler,
	}
}

func (s *Server) ListenAddr() string {
	return s.listenAddr
}

func (s *Server) OperatorUser() string {
	return s.operatorUser
}

func (s *Server) AcceptSession(ctx context.Context, session Session) error {
	if session.User() != s.operatorUser {
		return fmt.Errorf("unauthorized operator user %q", session.User())
	}
	if s.handler == nil {
		return fmt.Errorf("operator session handler is not configured")
	}

	return s.handler.HandleSession(ctx, session)
}
