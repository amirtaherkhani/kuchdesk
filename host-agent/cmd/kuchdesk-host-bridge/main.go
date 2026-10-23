package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/hostbridge"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/serviceaccess"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "host bridge stopped:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		return err
	}
	if _, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey); err != nil {
		return errors.New("TLS certificate or private key unavailable")
	}
	api, err := hostbridge.New(doctor.Service{}, cfg.Token)
	if err != nil {
		return err
	}
	if cfg.ProjectRoot != "" {
		api.ServiceAccess = func(ctx context.Context) (map[string]any, error) {
			return serviceaccess.Read(ctx, cfg.ProjectRoot)
		}
	}
	server := &http.Server{
		Addr: cfg.Listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 35 * time.Second, WriteTimeout: 65 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if cfg.ForwardingRecovery {
		go serviceaccess.RunReconciler(ctx, cfg.ProjectRoot)
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
