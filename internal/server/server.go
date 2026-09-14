package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/version"
)

type Server struct {
	httpServer *http.Server
	errCh      chan error
}

func New(addr string, engine *gin.Engine) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           engine,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

func (s *Server) Run() error {
	if _, err := s.Start(); err != nil {
		return err
	}
	return s.Wait()
}

func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	addr := ln.Addr().String()
	printBanner(addr)

	s.errCh = make(chan error, 1)
	go func() {
		err := s.httpServer.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			s.errCh <- fmt.Errorf("serve: %w", err)
			return
		}
		s.errCh <- nil
	}()
	return addr, nil
}

func (s *Server) Wait() error {
	if s.errCh == nil {
		return nil
	}
	return <-s.errCh
}

func printBanner(addr string) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("  Hello Coder %s started successfully\n", version.Short())
	fmt.Printf("  Local:   %s\n", LocalURL(addr))
	fmt.Printf("  Listen:  http://%s\n", addr)
	fmt.Println("========================================")
	fmt.Println()
}

func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:10240/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
