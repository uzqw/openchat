package api

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"openchat/internal/opencli"
	"openchat/internal/provider"
	"openchat/internal/service"
)

// Config is the whole backend configuration, read from environment
// variables and validated fail-closed (docs/deployment-operations.md §4):
// non-loopback listeners must carry Basic Auth credentials, a trusted Host
// and trusted Origin(s), and the dev no-auth switch only works on loopback.
type Config struct {
	// service wiring
	DataDir        string
	ExecPath       string
	Profile        string
	Site           *opencli.Site // OPENCLI_SITE adapter (default gemini)
	QueueCapacity  int
	AskTimeout     time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
	NtfyURL        string // OPENCLI_NTFY_URL; reply-done ntfy publish URL (empty = off)

	// provider cache wiring
	ProbeTimeout time.Duration
	CacheTTL     time.Duration
	ExtraEnv     []string // appended after the child env allowlist (fake scenarios in tests)

	// HTTP / security
	ListenAddr     string
	BasicAuthUser  string
	BasicAuthPass  string
	TrustedHost    string
	TrustedOrigins []string
	DevNoAuth      bool
	MaxBodyBytes   int64
	WebDir         string // built frontend dir; "" = do not serve static files

	// Chrome watchdog: relaunch the visible Chrome (OpenCLI Browser Bridge
	// host) when its CDP endpoint goes silent (docs/deployment-operations.md §3.6).
	ChromeWatchdog   bool          // OPENCLI_CHROME_WATCHDOG (default on)
	ChromeCDPAddr    string        // OPENCLI_CHROME_CDP_ADDR (default 127.0.0.1:9225)
	ChromeDisplay    string        // OPENCLI_CHROME_DISPLAY (default :3)
	ChromeCheckEvery time.Duration // OPENCLI_CHROME_CHECK_INTERVAL (default 30s)
	ChromeLaunchCmd  []string      // OPENCLI_CHROME_LAUNCH_CMD (default /usr/local/bin/box-chrome)
}

// Defaults applied when the corresponding env var is absent.
const (
	defaultListenAddr = "127.0.0.1:8090"
	defaultTimeoutSec = 60 // 60s, fail fast instead of wedging queue for 5m
	defaultQueueCap   = 1
	defaultMaxBody    = 128 << 10 // body ceiling above MaxPromptBytes

	defaultChromeCDPAddr = "127.0.0.1:9225"
	defaultChromeDisplay = ":3"
	defaultChromeCheckS  = 30
	defaultChromeLaunch  = "/usr/local/bin/box-chrome"
)

// LoadEnv builds a validated Config from an environment lookup. Any
// fail-closed violation returns an error that must abort startup.
func LoadEnv(getenv func(string) string) (*Config, error) {
	site, err := opencli.ByName(getenv("OPENCLI_SITE"))
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		DataDir:          getenv("PB_DATA_DIR"),
		ExecPath:         getenv("OPENCLI_PATH"),
		Profile:          getenv("OPENCLI_PROFILE"),
		Site:             site,
		ListenAddr:       envOr(getenv, "OPENCLI_LISTEN_ADDR", defaultListenAddr),
		BasicAuthUser:    getenv("BASIC_AUTH_USER"),
		BasicAuthPass:    getenv("BASIC_AUTH_PASS"),
		TrustedHost:      getenv("OPENCLI_TRUSTED_HOST"),
		QueueCapacity:    envInt(getenv, "OPENCLI_QUEUE_CAPACITY", defaultQueueCap),
		AskTimeout:       envDur(getenv, "OPENCLI_TIMEOUT_SECONDS", defaultTimeoutSec),
		MaxStdoutBytes:   envInt(getenv, "OPENCLI_MAX_STDOUT_BYTES", 0),
		MaxStderrBytes:   envInt(getenv, "OPENCLI_MAX_STDERR_BYTES", 0),
		NtfyURL:          getenv("OPENCLI_NTFY_URL"),
		ProbeTimeout:     envDur(getenv, "OPENCLI_PROBE_TIMEOUT_SECONDS", int(provider.DefaultProbeTimeout.Seconds())),
		CacheTTL:         envDur(getenv, "OPENCLI_CACHE_TTL_SECONDS", int(provider.DefaultTTL.Seconds())),
		MaxBodyBytes:     defaultMaxBody,
		DevNoAuth:        envBool(getenv, "OPENCLI_DEV_NO_AUTH"),
		WebDir:           envOr(getenv, "OPENCLI_WEB_DIR", "web/dist"),
		ChromeWatchdog:   !envBool(getenv, "OPENCLI_CHROME_WATCHDOG_DISABLE"),
		ChromeCDPAddr:    envOr(getenv, "OPENCLI_CHROME_CDP_ADDR", defaultChromeCDPAddr),
		ChromeDisplay:    envOr(getenv, "OPENCLI_CHROME_DISPLAY", defaultChromeDisplay),
		ChromeCheckEvery: envDur(getenv, "OPENCLI_CHROME_CHECK_INTERVAL", defaultChromeCheckS),
		ChromeLaunchCmd:  splitList(envOr(getenv, "OPENCLI_CHROME_LAUNCH_CMD", defaultChromeLaunch)),
	}
	for _, o := range splitList(getenv("OPENCLI_TRUSTED_ORIGIN")) {
		cfg.TrustedOrigins = append(cfg.TrustedOrigins, o)
	}

	if cfg.Profile == "" {
		return nil, errors.New("OPENCLI_PROFILE is required (dedicated OpenCLI profile)")
	}
	if cfg.DataDir == "" {
		return nil, errors.New("PB_DATA_DIR is required (an explicit data directory; never an implicit ./pb_data)")
	}
	if cfg.QueueCapacity <= 0 {
		return nil, errors.New("OPENCLI_QUEUE_CAPACITY must be positive")
	}

	loopback := isLoopbackAddr(cfg.ListenAddr)
	if !loopback {
		switch {
		case cfg.BasicAuthUser == "" || cfg.BasicAuthPass == "":
			return nil, fmt.Errorf("non-loopback listen addr %q requires BASIC_AUTH_USER and BASIC_AUTH_PASS", cfg.ListenAddr)
		case cfg.TrustedHost == "":
			return nil, fmt.Errorf("non-loopback listen addr %q requires OPENCLI_TRUSTED_HOST", cfg.ListenAddr)
		case len(cfg.TrustedOrigins) == 0:
			return nil, fmt.Errorf("non-loopback listen addr %q requires OPENCLI_TRUSTED_ORIGIN", cfg.ListenAddr)
		}
	}
	if cfg.DevNoAuth && !loopback {
		return nil, fmt.Errorf("OPENCLI_DEV_NO_AUTH only works on a loopback listen address (got %q)", cfg.ListenAddr)
	}
	return cfg, nil
}

// isLoopbackAddr reports whether listenAddr binds only the loopback
// interface. A bare ":port" (all interfaces) is not loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback()
	}
	return false
}

func envOr(getenv func(string) string, k, def string) string {
	if v := getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(getenv func(string) string, k string, def int) int {
	v, err := strconv.Atoi(getenv(k))
	if err != nil {
		return def
	}
	return v
}

func envDur(getenv func(string) string, k string, defSec int) time.Duration {
	sec := envInt(getenv, k, defSec)
	return time.Duration(sec) * time.Second
}

func envBool(getenv func(string) string, k string) bool {
	return getenv(k) == "1" || strings.EqualFold(getenv(k), "true")
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ServiceConfig maps the config onto the service layer input.
func (c *Config) ServiceConfig() service.Config {
	return service.Config{
		DataDir:        c.DataDir,
		ExecPath:       c.ExecPath,
		Profile:        c.Profile,
		Site:           c.Site,
		ExtraEnv:       c.ExtraEnv,
		QueueCapacity:  c.QueueCapacity,
		AskTimeout:     c.AskTimeout,
		MaxStdoutBytes: c.MaxStdoutBytes,
		MaxStderrBytes: c.MaxStderrBytes,
		NtfyURL:        c.NtfyURL,
	}
}

// ProviderConfig maps the config onto the provider layer input.
func (c *Config) ProviderConfig() provider.Config {
	return provider.Config{
		ExecPath:       c.ExecPath,
		Profile:        c.Profile,
		Site:           c.Site,
		ExtraEnv:       c.ExtraEnv,
		ProbeTimeout:   c.ProbeTimeout,
		MaxStdoutBytes: c.MaxStdoutBytes,
		MaxStderrBytes: c.MaxStderrBytes,
		TTL:            c.CacheTTL,
	}
}
