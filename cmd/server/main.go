// Command openchat-server runs the Gemini v1 backend: PocketBase data
// layer, the FIFO operation queue worker, the business REST API and the
// provider cache refresher. Configuration is read from the environment
// and validated fail-closed (see internal/api/config.go).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"openchat/internal/api"
	"openchat/internal/opencli"
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

	provs := map[string]*provider.Provider{}
	for _, site := range opencli.Sites {
		pc := cfg.ProviderConfig()
		pc.Site = site
		provs[site.Name] = provider.New(svc.St, svc.Queue, pc)
	}
	// provider writes fail closed while the conversation's site adapter is
	// overridden locally, plugins are installed, or the probed version
	// mismatches the contract (docs/deployment-operations.md §4).
	svc.SetWriteGuard(func(siteName string) error {
		p, ok := provs[siteName]
		if !ok {
			return fmt.Errorf("unsupported provider site %q", siteName)
		}
		return p.WriteBlocked()
	})
	handler := api.New(svc, provs, cfg).Handler()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Chrome watchdog: relaunch the visible Chrome (OpenCLI Browser Bridge
	// host) whenever its CDP endpoint goes silent, so opencli stays usable
	// after Chrome exits (docs/deployment-operations.md §3.6).
	if cfg.ChromeWatchdog {
		go watchChrome(ctx, cfg)
	}
	// one-shot startup probe per site (version/doctor/status/whoami, plus
	// gemini models) for the write guards and initial status; afterwards
	// probes only run on demand via POST /api/providers/{site}/refresh —
	// never on a background timer. Registry order keeps the sequence stable.
	for _, site := range opencli.Sites {
		if p, ok := provs[site.Name]; ok {
			p.MaybeRefresh()
		}
	}

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

// watchChrome keeps the visible Chrome alive for the server's lifetime:
// every interval it probes the CDP endpoint and relaunches Chrome via the
// configured launch command when it goes silent. The launch command is
// expected to detach (box-chrome setsid -f) and exit on its own.
func watchChrome(ctx context.Context, cfg *api.Config) {
	client := &http.Client{Timeout: time.Second}
	for {
		if !chromeUp(client, cfg.ChromeCDPAddr) {
			log.Printf("chrome watchdog: CDP %s silent, launching %v", cfg.ChromeCDPAddr, cfg.ChromeLaunchCmd)
			cmd := exec.Command(cfg.ChromeLaunchCmd[0], cfg.ChromeLaunchCmd[1:]...)
			cmd.Env = append(os.Environ(), "DISPLAY="+cfg.ChromeDisplay)
			if err := cmd.Start(); err != nil {
				log.Printf("chrome watchdog: launch failed: %v", err)
			} else {
				go cmd.Wait() // reap the launcher once it exits
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.ChromeCheckEvery):
		}
	}
}

// chromeUp reports whether the Chrome DevTools endpoint responds.
func chromeUp(client *http.Client, addr string) bool {
	resp, err := client.Get("http://" + addr + "/json/version")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
