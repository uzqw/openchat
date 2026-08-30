package provider

import (
	"errors"
	"os"
	"path/filepath"

	"openchat/internal/opencli"
)

// Write-guard errors: Gemini write operations (ask, retry, login) fail
// closed while any of these conditions hold (docs/deployment-operations.md §4: a local adapter
// override, an installed OpenCLI plugin, or a version mismatch must never
// run a real write against the user's Gemini account).
var (
	ErrAdapterOverride = errors.New("Gemini adapter was overridden locally")
	ErrPluginInstalled = errors.New("an OpenCLI plugin is installed")
	ErrVersionMismatch = errors.New("OpenCLI version does not match the locked version")
)

// WriteBlocked reports whether Gemini write operations must fail closed
// right now, or nil when writes are allowed. It checks the dedicated
// service account HOME for the `~/.opencli/clis/gemini` local override and
// any installed OpenCLI plugin, plus the probed version against the locked
// contract. An unknown version (not yet probed) does not block — the
// version probe runs first at startup. If HOME is unreadable the
// filesystem checks are skipped (the child would fail on its own).
func (p *Provider) WriteBlocked() error {
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".opencli", "clis", "gemini")); err == nil {
			return ErrAdapterOverride
		}
		if entries, err := os.ReadDir(filepath.Join(home, ".opencli", "plugins")); err == nil && len(entries) > 0 {
			return ErrPluginInstalled
		}
	}
	if v := p.cache.probedVersion(); v != "" && v != opencli.LockedVersion {
		return ErrVersionMismatch
	}
	return nil
}

// BlockedCodeOf maps a write-guard error to its stable API code.
func BlockedCodeOf(err error) string {
	switch {
	case errors.Is(err, ErrAdapterOverride):
		return "adapter_override"
	case errors.Is(err, ErrPluginInstalled):
		return "plugin_installed"
	case errors.Is(err, ErrVersionMismatch):
		return "version_mismatch"
	}
	return "write_blocked"
}

func (c *Cache) probedVersion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}
