// Package provider holds the Gemini runtime cache (version, doctor/Bridge,
// login state, models, login operation state) and the serialized
// non-ask operations behind it: explicit user login and the background
// refresh. Everything is concrete Gemini handling — there is no provider
// registry or multi-provider abstraction. Refresh and login only run
// while Gemini is not quarantined and no active conversation has a
// successful turn; the GET API reads the cache so UI polling never
// enqueues commands.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"openchat/internal/opencli"
	"openchat/internal/queue"
	"openchat/internal/store"
)

// Login operation states (documented in docs/domain-api.md §2.4).
const (
	LoginOpIdle      = "idle"      // never enqueued since last terminal state
	LoginOpQueued    = "queued"    // enqueued, not yet executed
	LoginOpRunning   = "running"   // executing the foreground login
	LoginOpSucceeded = "succeeded" // exit 0
	LoginOpFailed    = "failed"    // did not complete (sanitized message)
)

// Typed errors for the API layer (site-neutral: the same gates apply to
// every provider adapter).
var (
	ErrLoginInProgress   = errors.New("a login is already queued or running")
	ErrLoginBlocked      = errors.New("login is not allowed right now")
	ErrRefreshInProgress = errors.New("a refresh is already queued or running")
	ErrRefreshBlocked    = errors.New("refresh is not allowed right now")
)

// Default refresh knobs when the config leaves them zero.
const (
	DefaultTTL          = 2 * time.Minute // cache considered stale after this
	DefaultProbeTimeout = 2 * time.Minute // kill ceiling for one probe command
	DefaultProbeStdout  = 256 << 10
	DefaultProbeStderr  = 64 << 10
)

// Config wires the provider operations.
type Config struct {
	ExecPath       string
	Profile        string
	Site           *opencli.Site // OPENCLI_SITE adapter (default gemini)
	ExtraEnv       []string      // appended after the child env allowlist (fake scenarios in tests)
	ProbeTimeout   time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
	TTL            time.Duration // cache staleness
}

// Cache is the goroutine-safe runtime state; the API reads it via
// Snapshot and the login operation writes it.
type Cache struct {
	mu sync.RWMutex

	version   string
	bridge    string
	models    []string
	loggedIn  bool
	loginOp   string
	loginMsg  string
	refreshed time.Time
}

// Snapshot is one immutable view for the API response.
type Snapshot struct {
	Version     string    `json:"version"`
	Bridge      string    `json:"bridge"`
	Site        string    `json:"site"`               // opencli adapter name ("gemini")
	ModelPick   bool      `json:"model_pick"`         // ask accepts --model
	Thinking    bool      `json:"thinking_supported"` // ask accepts --thinking
	Models      []string  `json:"models"`
	LoggedIn    bool      `json:"logged_in"`
	LoginOp     string    `json:"login_operation"`
	LoginMsg    string    `json:"login_message,omitempty"`
	Quarantined bool      `json:"quarantined"`
	RefreshedAt time.Time `json:"refreshed_at,omitempty"`
	// WriteBlocked carries the stable code of the write-guard reason
	// (adapter_override | plugin_installed | version_mismatch) when writes
	// must fail closed, or empty when writes are allowed.
	WriteBlocked string `json:"write_blocked,omitempty"`
}

// Provider coordinates the cache, the queue and the store checks that gate
// non-ask operations.
type Provider struct {
	cache      *Cache
	store      *store.Store
	queue      *queue.Queue
	cfg        Config
	mu         sync.Mutex
	refreshing bool
}

// New builds a provider with defaults applied.
func New(st *store.Store, q *queue.Queue, cfg Config) *Provider {
	if cfg.Site == nil {
		cfg.Site = opencli.SiteGemini
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = DefaultProbeTimeout
	}
	if cfg.MaxStdoutBytes <= 0 {
		cfg.MaxStdoutBytes = DefaultProbeStdout
	}
	if cfg.MaxStderrBytes <= 0 {
		cfg.MaxStderrBytes = DefaultProbeStderr
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	return &Provider{
		cache: &Cache{loginOp: LoginOpIdle},
		store: st,
		queue: q,
		cfg:   cfg,
	}
}

// Snapshot returns the cached state with the derived quarantine flag.
func (p *Provider) Snapshot(ctx context.Context) (Snapshot, error) {
	q, err := p.store.IsQuarantined(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	// write-guard reasons come from the filesystem and the cached version;
	// keep them out of the cache lock (WriteBlocked takes its own RLock)
	blocked := ""
	if err := p.WriteBlocked(); err != nil {
		blocked = BlockedCodeOf(err)
	}
	c := p.cache
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Snapshot{
		Version:      c.version,
		Bridge:       c.bridge,
		Site:         p.cfg.Site.Name,
		ModelPick:    p.cfg.Site.ModelPick,
		Thinking:     p.cfg.Site.Thinking,
		Models:       append([]string{}, c.models...),
		LoggedIn:     c.loggedIn,
		LoginOp:      c.loginOp,
		LoginMsg:     c.loginMsg,
		Quarantined:  q,
		RefreshedAt:  c.refreshed,
		WriteBlocked: blocked,
	}, nil
}

// RequestLogin enqueues a foreground Gemini login. Allowed only while
// Gemini is not quarantined, the write guard is open (adapter override /
// plugin / version mismatch fail closed), no active conversation has a
// successful turn, and a second login while one is queued/running is a
// conflict. A new queued login replaces any terminal state.
func (p *Provider) RequestLogin(ctx context.Context) error {
	if q, err := p.store.IsQuarantined(ctx); err != nil {
		return err
	} else if q {
		return ErrLoginBlocked
	}
	if err := p.WriteBlocked(); err != nil {
		return err
	}
	if has, err := p.activeHasSuccess(ctx); err != nil {
		return err
	} else if has {
		return ErrLoginBlocked
	}
	if op := p.cache.loginOpState(); op == LoginOpQueued || op == LoginOpRunning {
		return ErrLoginInProgress
	}
	p.cache.setLoginOp(LoginOpQueued, "")
	p.queue.Enqueue(p.loginOperation())
	return nil
}

// MaybeRefresh enqueues one probe refresh when every gate is open: not
// quarantined, no active conversation with a successful turn, cache stale.
// It is called once per site at startup (the documented window for
// doctor/status/whoami/models) and by tests; the refresh operation
// re-checks the gates at execution time, so a late quarantine or success
// never runs probes. There is deliberately no queue-idle gate here: with
// one site per Provider the per-provider refreshing flag already prevents
// duplicates, and dropping that check guarantees every site's startup
// probe is actually enqueued instead of racing the worker on Idle().
func (p *Provider) MaybeRefresh() {
	if p.isRefreshing() {
		return
	}
	if q, err := p.store.IsQuarantined(context.Background()); err != nil || q {
		return
	}
	if has, err := p.activeHasSuccess(context.Background()); err != nil || has {
		return
	}
	if !p.cache.stale(p.cfg.TTL) {
		return
	}
	p.setRefreshing(true)
	p.queue.Enqueue(p.refreshOperation())
}

// RequestRefresh enqueues one on-demand probe refresh (the "检测在线"
// button). It is refused while Gemini is quarantined, an active
// conversation already has a successful turn, or a refresh is already
// queued/running — the same gates as login, because the probes touch the
// shared OpenCLI tab. Unlike MaybeRefresh it always probes, even when the
// cache is fresh: the user asked for it.
func (p *Provider) RequestRefresh(ctx context.Context) error {
	if p.isRefreshing() {
		return ErrRefreshInProgress
	}
	if q, err := p.store.IsQuarantined(ctx); err != nil {
		return err
	} else if q {
		return ErrRefreshBlocked
	}
	if has, err := p.activeHasSuccess(ctx); err != nil {
		return err
	} else if has {
		return ErrRefreshBlocked
	}
	p.setRefreshing(true)
	p.queue.Enqueue(p.refreshOperation())
	return nil
}

func (p *Provider) activeHasSuccess(ctx context.Context) (bool, error) {
	conv, err := p.store.ActiveConversation(ctx)
	if err != nil {
		return false, err
	}
	if conv == nil {
		return false, nil
	}
	return p.store.ConversationHasSuccessfulTask(ctx, conv.ID)
}

// blocked reports whether non-ask operations must not run right now
// (quarantine, or an active conversation that already has a success).
func (p *Provider) blocked(ctx context.Context) bool {
	if q, err := p.store.IsQuarantined(ctx); err != nil || q {
		return true
	}
	if has, err := p.activeHasSuccess(ctx); err != nil || has {
		return true
	}
	return false
}

// loginOperation is the queue op for an explicit user login. It parks
// while Gemini is quarantined (all operations are paused then) instead of
// running inside the quarantine, and re-checks the active-success rule
// before executing.
func (p *Provider) loginOperation() queue.Operation {
	return queue.Operation{
		ID:  "login:" + p.cfg.Site.Name,
		Ask: false,
		Run: func(ctx context.Context) error {
			for {
				if q, err := p.store.IsQuarantined(ctx); err != nil {
					return err
				} else if !q {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
			if has, err := p.activeHasSuccess(ctx); err != nil {
				return err
			} else if has {
				p.cache.setLoginOp(LoginOpFailed, "login blocked: the active conversation already has a successful turn")
				return nil
			}
			p.cache.setLoginOp(LoginOpRunning, "")
			exec := opencli.Execer{
				Path:           p.cfg.ExecPath,
				ExtraEnv:       p.cfg.ExtraEnv,
				Timeout:        p.cfg.ProbeTimeout, // kill ceiling: a hung login must free the FIFO queue
				MaxStdoutBytes: p.cfg.MaxStdoutBytes,
				MaxStderrBytes: p.cfg.MaxStderrBytes,
			}
			res := exec.Run(ctx, p.cfg.Site.LoginArgs(p.cfg.Profile)...)
			switch {
			case !res.Started:
				p.cache.setLoginOp(LoginOpFailed, p.cfg.Site.Label+" login could not start")
			case res.ExitCode == 0:
				p.cache.setLoginOp(LoginOpSucceeded, "")
			case res.TimedOut:
				p.cache.setLoginOp(LoginOpFailed, p.cfg.Site.Label+" login timed out (already logged in?)")
			default:
				p.cache.setLoginOp(LoginOpFailed,
					fmt.Sprintf(p.cfg.Site.Label+" login was not completed (exit %d)", res.ExitCode))
			}
			return nil
		},
	}
}

// refreshOperation is the queue op for the background probe refresh. It
// skips itself if a gate closed between enqueue and execution (quarantine
// or active success), so nothing ever probes inside a quarantine.
func (p *Provider) refreshOperation() queue.Operation {
	return queue.Operation{
		ID:  "refresh:" + p.cfg.Site.Name,
		Ask: false,
		Run: func(ctx context.Context) error {
			defer p.setRefreshing(false)
			if p.blocked(ctx) {
				return nil
			}
			exec := opencli.Execer{
				Path:           p.cfg.ExecPath,
				ExtraEnv:       p.cfg.ExtraEnv,
				Timeout:        p.cfg.ProbeTimeout,
				MaxStdoutBytes: p.cfg.MaxStdoutBytes,
				MaxStderrBytes: p.cfg.MaxStderrBytes,
			}
			if r := probeWithRetry(ctx, exec, opencli.VersionArgs()...); r.Started && r.ExitCode == 0 {
				p.cache.setVersion(strings.TrimSpace(r.Stdout))
			}
			if r := probeWithRetry(ctx, exec, opencli.DoctorArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				p.cache.setBridge(strings.TrimSpace(r.Stdout))
			} else {
				p.cache.setBridge("") // clear a stale bridge on a failed probe
			}
			if r := probeWithRetry(ctx, exec, p.cfg.Site.StatusArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				if v, known := parseLoggedIn(r.Stdout); known {
					p.cache.setLoggedIn(v)
				}
			}
			if r := probeWithRetry(ctx, exec, p.cfg.Site.WhoamiArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				if v, known := parseLoggedIn(r.Stdout); known {
					p.cache.setLoggedIn(v)
				}
			}
			if p.cfg.Site.ModelsCmd {
				if r := probeWithRetry(ctx, exec, p.cfg.Site.ModelsArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
					p.cache.setModels(parseModels(r.Stdout))
				}
			}
			p.cache.markRefreshed()
			return nil
		},
	}
}

// probeWithRetry runs one probe command and, when it did not succeed (opencli's
// daemon occasionally rejects a command that starts right after the previous
// one released the shared tab), waits briefly and retries once before giving
// up. Startup probes run back to back on the shared tab, so this absorbs the
// transient rejection without hiding a real failure (which stays failed).
func probeWithRetry(ctx context.Context, exec opencli.Execer, args ...string) opencli.Result {
	r := exec.Run(ctx, args...)
	if r.Started && r.ExitCode == 0 {
		return r
	}
	select {
	case <-ctx.Done():
		return r
	case <-time.After(3 * time.Second):
	}
	return exec.Run(ctx, args...)
}

func (p *Provider) isRefreshing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshing
}

func (p *Provider) setRefreshing(v bool) {
	p.mu.Lock()
	p.refreshing = v
	p.mu.Unlock()
}

// parseLoggedIn extracts the login state from gemini status/whoami JSON.
// Real v1.8.7 status output is a top-level array with a capitalized
// "Login" string ("Yes"/"No"); whoami output is a bare object with
// boolean fields. Only recognized values are trusted — anything else is
// unknown (fail closed: never claim logged in).
func parseLoggedIn(out string) (value, known bool) {
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err == nil {
		for _, m := range arr {
			if s, ok := m["Login"].(string); ok {
				switch s {
				case "Yes":
					return true, true
				case "No":
					return false, true
				}
				return false, false
			}
		}
		return false, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return false, false
	}
	for _, k := range []string{"logged_in", "loggedIn", "is_logged_in"} {
		if b, ok := m[k].(bool); ok {
			return b, true
		}
	}
	return false, false
}

// parseModels extracts the model id list from gemini models JSON. Real
// v1.8.7 output is a top-level array of {"model": "..."} objects; the
// legacy documented shape is a bare object with a "models" array of
// strings or {"id": "..."} objects. Any unrecognized shape yields nil,
// which disables model selection (the platform never maintains an
// imagined static model table).
func parseModels(out string) []string {
	var arr []struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(out), &arr); err == nil {
		models := make([]string, 0, len(arr))
		for _, o := range arr {
			if o.Model != "" {
				models = append(models, o.Model)
			}
		}
		return models
	}
	var m struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return nil
	}
	var models []string
	for _, raw := range m.Models {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			models = append(models, s)
			continue
		}
		var o struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &o) == nil && o.ID != "" {
			models = append(models, o.ID)
		}
	}
	return models
}

// ---- cache accessors -------------------------------------------------------

func (c *Cache) loginOpState() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loginOp
}

func (c *Cache) setLoginOp(op, msg string) {
	c.mu.Lock()
	c.loginOp = op
	c.loginMsg = msg
	c.mu.Unlock()
}

func (c *Cache) stale(ttl time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.refreshed) > ttl
}

func (c *Cache) setVersion(v string) {
	c.mu.Lock()
	c.version = v
	c.mu.Unlock()
}

func (c *Cache) setBridge(v string) {
	c.mu.Lock()
	c.bridge = v
	c.mu.Unlock()
}

func (c *Cache) setLoggedIn(v bool) {
	c.mu.Lock()
	c.loggedIn = v
	c.mu.Unlock()
}

func (c *Cache) setModels(v []string) {
	c.mu.Lock()
	c.models = v
	c.mu.Unlock()
}

func (c *Cache) markRefreshed() {
	c.mu.Lock()
	c.refreshed = time.Now()
	c.mu.Unlock()
}
