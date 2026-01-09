// Package dns provides CoreDNS-based DNS server
package dns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"

	_ "github.com/cldmnky/oooi/internal/dns/plugin"
)

type Server struct {
	corefilePath string
	instance     *caddy.Instance
	cancel       context.CancelFunc
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

	internalCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	done := make(chan struct{})
	go func() {
		<-internalCtx.Done()
		if s.instance != nil {
			s.instance.ShutdownCallbacks()
			s.instance.Stop()
		}
		close(done)
	}()

	s.instance.Wait()
	<-done
	return ctx.Err()
}

func (s *Server) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}

	if s.instance != nil {
		if errs := s.instance.ShutdownCallbacks(); len(errs) > 0 {
			return fmt.Errorf("shutdown callbacks failed: %v", errs)
		}
		if err := s.instance.Stop(); err != nil {
			return fmt.Errorf("failed to stop instance: %w", err)
		}
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
