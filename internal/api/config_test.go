package api

import (
	"strings"
	"testing"
)

// env is a tiny map-backed getenv for LoadEnv tests.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// baseEnv is a minimal valid loopback-dev configuration.
func baseEnv() map[string]string {
	return map[string]string{
		"PB_DATA_DIR":            "/tmp/test-data",
		"OPENCLI_PROFILE":        "gemini-relay",
		"OPENCLI_LISTEN_ADDR":    "127.0.0.1:8090",
		"OPENCLI_DEV_NO_AUTH":    "1",
		"OPENCLI_QUEUE_CAPACITY": "2",
	}
}

func TestLoadEnvLoopbackDevOK(t *testing.T) {
	cfg, err := LoadEnv(env(baseEnv()))
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if !cfg.DevNoAuth || !isLoopbackAddr(cfg.ListenAddr) {
		t.Fatalf("expected loopback dev config, got %+v", cfg)
	}
	if cfg.QueueCapacity != 2 {
		t.Fatalf("QueueCapacity = %d, want 2", cfg.QueueCapacity)
	}
	if cfg.AskTimeout == 0 || cfg.MaxBodyBytes == 0 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.Site == nil || cfg.Site.Name != "gemini" {
		t.Fatalf("default site must be gemini, got %v", cfg.Site)
	}
}

func TestLoadEnvSite(t *testing.T) {
	m := baseEnv()
	m["OPENCLI_SITE"] = "grok"
	cfg, err := LoadEnv(env(m))
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if cfg.Site.Name != "grok" {
		t.Fatalf("site = %q, want grok", cfg.Site.Name)
	}
	if cfg.ServiceConfig().Site != cfg.Site || cfg.ProviderConfig().Site != cfg.Site {
		t.Fatalf("site must flow into service and provider configs")
	}
	if _, err := LoadEnv(env(map[string]string{"OPENCLI_SITE": "claude"})); err == nil {
		t.Fatal("unsupported OPENCLI_SITE must fail closed")
	}
}

func TestLoadEnvRequiresProfile(t *testing.T) {
	m := baseEnv()
	delete(m, "OPENCLI_PROFILE")
	if _, err := LoadEnv(env(m)); err == nil || !strings.Contains(err.Error(), "OPENCLI_PROFILE") {
		t.Fatalf("expected OPENCLI_PROFILE error, got %v", err)
	}
}

func TestLoadEnvRequiresDataDir(t *testing.T) {
	m := baseEnv()
	delete(m, "PB_DATA_DIR")
	if _, err := LoadEnv(env(m)); err == nil || !strings.Contains(err.Error(), "PB_DATA_DIR") {
		t.Fatalf("expected PB_DATA_DIR error, got %v", err)
	}
}

// non-loopback listeners must fail closed without credentials, host and origin.
func TestLoadEnvFailClosedNonLoopback(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"nothing":    {"OPENCLI_LISTEN_ADDR": "0.0.0.0:8090", "PB_DATA_DIR": "/d", "OPENCLI_PROFILE": "p"},
		"no-cred":    {"OPENCLI_LISTEN_ADDR": "0.0.0.0:8090", "PB_DATA_DIR": "/d", "OPENCLI_PROFILE": "p", "OPENCLI_TRUSTED_HOST": "h", "OPENCLI_TRUSTED_ORIGIN": "https://h"},
		"no-host":    {"OPENCLI_LISTEN_ADDR": "0.0.0.0:8090", "PB_DATA_DIR": "/d", "OPENCLI_PROFILE": "p", "BASIC_AUTH_USER": "u", "BASIC_AUTH_PASS": "p", "OPENCLI_TRUSTED_ORIGIN": "https://h"},
		"no-origin":  {"OPENCLI_LISTEN_ADDR": "0.0.0.0:8090", "PB_DATA_DIR": "/d", "OPENCLI_PROFILE": "p", "BASIC_AUTH_USER": "u", "BASIC_AUTH_PASS": "p", "OPENCLI_TRUSTED_HOST": "h"},
		"empty-cred": {"OPENCLI_LISTEN_ADDR": "0.0.0.0:8090", "PB_DATA_DIR": "/d", "OPENCLI_PROFILE": "p", "BASIC_AUTH_USER": "", "BASIC_AUTH_PASS": "", "OPENCLI_TRUSTED_HOST": "h", "OPENCLI_TRUSTED_ORIGIN": "https://h"},
		// bare ":port" binds all interfaces — also non-loopback
		"wildcard": {"OPENCLI_LISTEN_ADDR": ":8090", "PB_DATA_DIR": "/d", "OPENCLI_PROFILE": "p"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadEnv(env(m)); err == nil {
				t.Fatalf("expected fail-closed error")
			}
		})
	}
}

func TestLoadEnvNonLoopbackWithAllConfigOK(t *testing.T) {
	m := map[string]string{
		"OPENCLI_LISTEN_ADDR":    "0.0.0.0:8090",
		"PB_DATA_DIR":            "/d",
		"OPENCLI_PROFILE":        "p",
		"BASIC_AUTH_USER":        "u",
		"BASIC_AUTH_PASS":        "secret",
		"OPENCLI_TRUSTED_HOST":   "gemini.example.com",
		"OPENCLI_TRUSTED_ORIGIN": "https://gemini.example.com, https://alt.example.com",
	}
	cfg, err := LoadEnv(env(m))
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if len(cfg.TrustedOrigins) != 2 {
		t.Fatalf("TrustedOrigins = %v, want 2 entries", cfg.TrustedOrigins)
	}
	if cfg.DevNoAuth {
		t.Fatalf("DevNoAuth must stay off")
	}
}

func TestLoadEnvDevNoAuthOnlyLoopback(t *testing.T) {
	m := map[string]string{
		"OPENCLI_LISTEN_ADDR": "0.0.0.0:8090",
		"PB_DATA_DIR":         "/d",
		"OPENCLI_PROFILE":     "p",
		"OPENCLI_DEV_NO_AUTH": "1",
	}
	if _, err := LoadEnv(env(m)); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback-only error, got %v", err)
	}
}

func TestLoadEnvLoopbackNoAuthStillValid(t *testing.T) {
	// loopback without dev-no-auth but also without credentials is valid
	// (the auth middleware then denies everything — fail closed).
	m := baseEnv()
	delete(m, "OPENCLI_DEV_NO_AUTH")
	if _, err := LoadEnv(env(m)); err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
}

func TestLoadEnvBadQueueCapacity(t *testing.T) {
	m := baseEnv()
	m["OPENCLI_QUEUE_CAPACITY"] = "0"
	if _, err := LoadEnv(env(m)); err == nil {
		t.Fatalf("expected capacity error")
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8090": true,
		"localhost:8090": true,
		"[::1]:8090":     true,
		"0.0.0.0:8090":   false,
		":8090":          false,
		"10.0.0.4:8090":  false,
	} {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
