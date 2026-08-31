// Package api exposes the v1 business REST API (docs/domain-api.md §4) on
// top of the service, with the security boundary from
// docs/deployment-operations.md §2: global Basic Auth (constant-time
// comparison, /api/health the only public route), trusted Host/Origin
// validation on writes, JSON-only write bodies, a unified error envelope,
// and no PocketBase admin/`/_/` routes at all (they are simply not bound).
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"openchat/internal/provider"
	"openchat/internal/service"
)

// API wires the business service and the per-site provider caches to HTTP.
type API struct {
	svc   *service.Service
	provs map[string]*provider.Provider // keyed by site name ("gemini" / "grok")
	cfg   *Config
}

// New builds the API layer. The service must already be recovered and
// started before the handler is used.
func New(svc *service.Service, provs map[string]*provider.Provider, cfg *Config) *API {
	return &API{svc: svc, provs: provs, cfg: cfg}
}

// Handler returns the routed, secured http.Handler.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("POST /api/conversations", a.handleCreateConversation)
	mux.HandleFunc("GET /api/conversations", a.handleListConversations)
	mux.HandleFunc("GET /api/conversations/{id}", a.handleGetConversation)
	mux.HandleFunc("POST /api/conversations/{id}/resume", a.handleResumeConversation)
	mux.HandleFunc("POST /api/conversations/{id}/turns", a.handleCreateTurn)
	mux.HandleFunc("GET /api/turns/{id}", a.handleGetTurn)
	mux.HandleFunc("POST /api/tasks/{id}/retry", a.handleRetryTask)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", a.handleCancelTask)
	mux.HandleFunc("POST /api/tasks/{id}/acknowledge-unknown", a.handleAcknowledgeUnknown)
	// provider endpoints are per-site: each site has its own cache/login/refresh
	mux.HandleFunc("GET /api/providers", a.handleGetProviders)
	mux.HandleFunc("POST /api/providers/{site}/login", a.handleLogin)
	mux.HandleFunc("POST /api/providers/{site}/refresh", a.handleRefresh)
	if a.cfg.WebDir != "" {
		// the built frontend lives under the same global Basic Auth
		// boundary; unknown /api* paths stay a JSON 404 (see static.go)
		mux.Handle("/", staticHandler(a.cfg.WebDir))
	}
	return a.secure(mux)
}

// secure applies the global boundary: host check, Basic Auth (health
// whitelisted), and write-side Origin + JSON content-type checks. A
// panic anywhere in the chain still produces the JSON envelope.
func (a *API) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()

		if a.cfg.TrustedHost != "" && !hostMatches(r.Host, a.cfg.TrustedHost) {
			writeErr(w, http.StatusForbidden, "forbidden", "untrusted host")
			return
		}

		if !a.cfg.DevNoAuth && r.URL.Path != "/api/health" {
			if user, pass, ok := r.BasicAuth(); !ok || !validCreds(user, pass, a.cfg) {
				w.Header().Set("WWW-Authenticate", `Basic realm="Gemini"`)
				writeErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
		}

		if isWrite(r.Method) {
			if len(a.cfg.TrustedOrigins) > 0 && !originAllowed(r.Header.Get("Origin"), a.cfg.TrustedOrigins) {
				writeErr(w, http.StatusForbidden, "forbidden", "untrusted origin")
				return
			}
			if r.ContentLength > 0 && !isJSON(r.Header.Get("Content-Type")) {
				writeErr(w, http.StatusBadRequest, "invalid_request", "write requests must be JSON")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// validCreds performs constant-time comparison on both username and
// password. Unset credentials can never authenticate (fail closed).
func validCreds(user, pass string, cfg *Config) bool {
	if cfg.BasicAuthUser == "" || cfg.BasicAuthPass == "" {
		return false
	}
	u := subtle.ConstantTimeCompare([]byte(user), []byte(cfg.BasicAuthUser))
	p := subtle.ConstantTimeCompare([]byte(pass), []byte(cfg.BasicAuthPass))
	return u&p == 1
}

// hostMatches compares the request Host with the trusted host, ignoring
// ports and case (browsers and curl include the port in Host, the trusted
// value may or may not).
func hostMatches(reqHost, trusted string) bool {
	return stripPort(reqHost) == stripPort(trusted)
}

func stripPort(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(hostport)
}

// originAllowed requires an exact match (scheme, host and port) against
// one of the trusted origins. Missing Origin headers are rejected when a
// trusted list is configured (fail closed, never trusts forwarded headers).
func originAllowed(origin string, trusted []string) bool {
	if origin == "" {
		return false
	}
	for _, t := range trusted {
		if strings.EqualFold(origin, t) {
			return true
		}
	}
	return false
}

func isWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func isJSON(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return ct == "application/json" || strings.HasPrefix(ct, "application/json;")
}

// ---- envelope + body helpers ----------------------------------------------

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errBadRequest wraps handler-level input failures (headers, JSON body).
var errBadRequest = errors.New("bad request")

// decodeBody reads a strict JSON body: unknown fields, trailing data,
// malformed JSON and over-limit bodies all fail (400 in the caller).
func (a *API) decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	body := http.MaxBytesReader(w, r.Body, a.cfg.MaxBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: unexpected data after JSON body", errBadRequest)
	}
	return nil
}
