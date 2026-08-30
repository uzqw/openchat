// Command openchat-server runs the Gemini v1 backend: PocketBase data
// layer, the FIFO operation queue worker, the business REST API and the
// provider cache refresher. Configuration is read from the environment
// and validated fail-closed (see internal/api/config.go).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"openchat/internal/api"
	"openchat/internal/provider"
	"openchat/internal/service"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := api.LoadEnv(os.Getenv)
	if err != nil {
		return err
	}

	// data dir and its backups must be owner-only
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(cfg.DataDir, 0o700)

	svc, err := service.New(cfg.ServiceConfig())
	if err != nil {
		return err
	}
	defer svc.Close()

	// startup recovery runs before the worker: pending→canceled,
	// running→unknown (quarantine), active→archived. Never auto-redispatch.
	if err := svc.Recover(); err != nil {
		return err
	}
	svc.Start()

	prov := provider.New(svc.St, svc.Queue, cfg.ProviderConfig())
	// Gemini writes fail closed while the local adapter is overridden or
	// plugins are installed, or the probed version mismatches the contract.
	svc.SetWriteGuard(prov.WriteBlocked)
	handler := api.New(svc, prov, cfg).Handler()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// one-shot startup probe (version/doctor/status/whoami/models) for the
	// write guard and initial status; afterwards probes only run on demand
	// via POST /api/providers/gemini/refresh — never on a background timer.
	prov.MaybeRefresh()

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: handler}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("openchat-server listening on %s (profile %q)", cfg.ListenAddr, cfg.Profile)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}
	return nil
}
