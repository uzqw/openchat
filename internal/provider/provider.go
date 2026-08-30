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

// Typed errors for the API layer.
var (
	ErrLoginInProgress = errors.New("a Gemini login is already queued or running")
	ErrLoginBlocked    = errors.New("Gemini login is not allowed right now")
)

// Default refresh knobs when the config leaves them zero.
const (
	DefaultTTL          = 2 * time.Minute // cache considered stale after this
	DefaultInterval     = time.Minute     // background refresher loop period
	DefaultProbeTimeout = 2 * time.Minute // kill ceiling for one probe command
	DefaultProbeStdout  = 256 << 10
	DefaultProbeStderr  = 64 << 10
)

// Config wires the provider operations.
type Config struct {
	ExecPath       string
	Profile        string
	ExtraEnv       []string // appended after the child env allowlist (fake scenarios in tests)
	ProbeTimeout   time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
	TTL            time.Duration // cache staleness
	Interval       time.Duration // refresher loop period
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
	Models      []string  `json:"models"`
	LoggedIn    bool      `json:"logged_in"`
	LoginOp     string    `json:"login_operation"`
	LoginMsg    string    `json:"login_message,omitempty"`
	Quarantined bool      `json:"quarantined"`
	RefreshedAt time.Time `json:"refreshed_at,omitempty"`
	// WriteBlocked carries the stable code of the write-guard reason
	// (adapter_override | plugin_installed | version_mismatch) when Gemini
	// writes must fail closed, or empty when writes are allowed.
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
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
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
		Models:       append([]string(nil), c.models...),
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

// MaybeRefresh enqueues one background refresh when every gate is open:
// not quarantined, no active conversation with a successful turn, cache
// stale and queue idle. The refresh operation re-checks the gates at
// execution time, so a late quarantine or success never runs probes.
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
	if !p.queue.Idle() {
		return
	}
	p.setRefreshing(true)
	p.queue.Enqueue(p.refreshOperation())
}

// RunRefresher drives the background refresh loop until ctx is canceled.
// An immediate attempt happens on start (startup phase is the documented
// window for doctor/status/whoami/models).
func (p *Provider) RunRefresher(ctx context.Context) {
	p.MaybeRefresh()
	t := time.NewTicker(p.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.MaybeRefresh()
		}
	}
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
		ID:  "login:gemini",
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
				MaxStdoutBytes: p.cfg.MaxStdoutBytes,
				MaxStderrBytes: p.cfg.MaxStderrBytes,
			}
			res := exec.Run(ctx, opencli.LoginArgs(p.cfg.Profile)...)
			switch {
			case !res.Started:
				p.cache.setLoginOp(LoginOpFailed, "Gemini login could not start")
			case res.ExitCode == 0:
				p.cache.setLoginOp(LoginOpSucceeded, "")
			default:
				p.cache.setLoginOp(LoginOpFailed,
					fmt.Sprintf("Gemini login was not completed (exit %d)", res.ExitCode))
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
		ID:  "refresh:gemini",
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
			if r := exec.Run(ctx, opencli.VersionArgs()...); r.Started && r.ExitCode == 0 {
				p.cache.setVersion(strings.TrimSpace(r.Stdout))
			}
			if r := exec.Run(ctx, opencli.DoctorArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				p.cache.setBridge(strings.TrimSpace(r.Stdout))
			} else {
				p.cache.setBridge("") // clear a stale bridge on a failed probe
			}
			if r := exec.Run(ctx, opencli.StatusArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				if v, known := parseLoggedIn(r.Stdout); known {
					p.cache.setLoggedIn(v)
				}
			}
			if r := exec.Run(ctx, opencli.WhoamiArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				if v, known := parseLoggedIn(r.Stdout); known {
					p.cache.setLoggedIn(v)
				}
			}
			if r := exec.Run(ctx, opencli.ModelsArgs(p.cfg.Profile)...); r.Started && r.ExitCode == 0 {
				p.cache.setModels(parseModels(r.Stdout))
			}
			p.cache.markRefreshed()
			return nil
		},
	}
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
// Only explicit boolean fields are recognized — anything else is unknown
// (fail closed: never claim logged in).
func parseLoggedIn(out string) (value, known bool) {
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

// parseModels extracts the model id list from gemini models JSON. Any
// unrecognized shape yields nil, which disables model selection (the
// platform never maintains an imagined static model table).
func parseModels(out string) []string {
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
