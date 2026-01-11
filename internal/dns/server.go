// Package dns provides CoreDNS-based DNS server
package dns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"

	_ "github.com/cldmnky/oooi/internal/dns/plugin"
)

type Server struct {
	corefilePath string
	instance     *caddy.Instance
	mu           sync.Mutex
	stopped      bool
}

func NewServer(corefilePath string) (*Server, error) {
	if _, err := os.Stat(corefilePath); err != nil {
		return nil, fmt.Errorf("corefile not found at %s: %w", corefilePath, err)
	}
	return &Server{corefilePath: corefilePath}, nil
}

func (s *Server) Start(ctx context.Context) error {
	caddy.Quiet = true
	caddy.DefaultConfigFile = s.corefilePath
	dnsserver.Quiet = true

	corefile, err := s.loadCorefile()
	if err != nil {
		return fmt.Errorf("failed to load corefile: %w", err)
	}

	instance, err := caddy.Start(corefile)
	if err != nil {
		return fmt.Errorf("failed to start coredns: %w", err)
	}
	s.instance = instance

	// Channel to signal when shutdown is complete
	shutdownComplete := make(chan struct{})

	// Start shutdown handler in background
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.instance != nil && !s.stopped {
			s.instance.ShutdownCallbacks()
			if err := s.instance.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to stop coredns instance: %v\n", err)
			}
			s.stopped = true
		}
		close(shutdownComplete)
	}()

	// Wait for either natural shutdown or context cancellation
	select {
	case <-shutdownComplete:
		// Context was cancelled, shutdown initiated
		return ctx.Err()
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() {
			s.instance.Wait()
			close(done)
		}()
		return done
	}():
		// Instance stopped naturally
		return nil
	}
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return nil // Already stopped
	}

	if s.instance != nil {
		if errs := s.instance.ShutdownCallbacks(); len(errs) > 0 {
			return fmt.Errorf("shutdown callbacks failed: %v", errs)
		}
		if err := s.instance.Stop(); err != nil {
			return fmt.Errorf("failed to stop instance: %w", err)
		}
		s.stopped = true
	}
	return nil
}

func (s *Server) loadCorefile() (caddy.Input, error) {
	contents, err := os.ReadFile(filepath.Clean(s.corefilePath))
	if err != nil {
		return nil, fmt.Errorf("failed to read corefile: %w", err)
	}
	return caddy.CaddyfileInput{
		Contents:       contents,
		Filepath:       s.corefilePath,
		ServerTypeName: "dns",
	}, nil
}
