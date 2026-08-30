package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openchat/internal/api"
	"openchat/internal/provider"
	"openchat/internal/service"
)

var fakePath string

// TestMain builds the fake opencli once; every test runs against it, so no
// real Gemini account or Chrome is ever touched (hard relay rule).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fake-opencli-api-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake build dir:", err)
		os.Exit(1)
	}
	fakePath = filepath.Join(dir, "opencli")
	build := exec.Command("go", "build", "-o", fakePath, "openchat/internal/opencli/fakeopencli")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "build fake opencli:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// ---- test harness -----------------------------------------------------------

type envOpt func(*api.Config)

// testEnv wraps a started service + provider + httptest server.
type testEnv struct {
	t    *testing.T
	srv  *httptest.Server
	svc  *service.Service
	prov *provider.Provider
}

func withAuth(user, pass string) envOpt {
	return func(c *api.Config) {
		c.DevNoAuth = false
		c.BasicAuthUser = user
		c.BasicAuthPass = pass
	}
}

func withExecPath(p string) envOpt {
	return func(c *api.Config) { c.ExecPath = p }
}

func withCapacity(n int) envOpt {
	return func(c *api.Config) { c.QueueCapacity = n }
}

// withScenario wires a FAKE_OPENCLI_SCENARIO_FILE so non-ask commands
// (version/doctor/models/status/whoami/login) behave per test. Values are
// raw JSON objects, e.g. {"models": {"stdout": "..."}}.
func withScenario(t *testing.T, scenarios map[string]string) envOpt {
	return func(c *api.Config) {
		var b strings.Builder
		b.WriteByte('{')
		first := true
		for k, v := range scenarios {
			if !first {
				b.WriteByte(',')
			}
			first = false
			fmt.Fprintf(&b, "%q:%s", k, v)
		}
		b.WriteByte('}')
		f := filepath.Join(t.TempDir(), "scenario.json")
		if err := os.WriteFile(f, []byte(b.String()), 0o600); err != nil {
			t.Fatalf("scenario file: %v", err)
		}
		c.ExtraEnv = []string{"FAKE_OPENCLI_SCENARIO_FILE=" + f}
	}
}

func newEnv(t *testing.T, opts ...envOpt) *testEnv {
	t.Helper()
	cfg := api.Config{
		DataDir:        t.TempDir(), // temp data dir; production pb_data is never touched
		ExecPath:       fakePath,
		Profile:        "test-profile",
		QueueCapacity:  1,
		AskTimeout:     10 * time.Second,
		MaxStdoutBytes: 64 << 10,
		MaxStderrBytes: 16 << 10,
		DevNoAuth:      true,
		MaxBodyBytes:   128 << 10,
	}
	for _, o := range opts {
		o(&cfg)
	}
	svc, err := service.New(cfg.ServiceConfig())
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	prov := provider.New(svc.St, svc.Queue, cfg.ProviderConfig())
	// the fail-closed write guard (adapter override / plugin / version) is
	// wired like production; it passes by default in tests.
	svc.SetWriteGuard(prov.WriteBlocked)
	if err := svc.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	svc.Start()
	t.Cleanup(svc.Close)

	handler := api.New(svc, prov, &cfg).Handler()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testEnv{t: t, srv: srv, svc: svc, prov: prov}
}

// do performs one request without touching the test logger (safe for
// goroutines); returns status and body or an error.
func (e *testEnv) do(method, path string, body any, headers map[string]string) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, data, nil
}

// doRaw sends a raw string body with the given headers (no JSON marshal
// or Content-Type default); safe for goroutines.
func (e *testEnv) doRaw(method, path, rawBody string, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(rawBody))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, data, nil
}

// req performs one request, failing the test on transport errors and
// asserting the expected status.
func (e *testEnv) req(method, path string, body any, headers map[string]string, want int) []byte {
	e.t.Helper()
	status, data, err := e.do(method, path, body, headers)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	if status != want {
		e.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, status, want, data)
	}
	return data
}

// apiErr decodes the unified error envelope.
type apiErr struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeErr(t *testing.T, data []byte) apiErr {
	t.Helper()
	var ae apiErr
	if err := json.Unmarshal(data, &ae); err != nil {
		t.Fatalf("envelope decode: %v; body %s", err, data)
	}
	return ae
}

func basic(user, pass string) map[string]string {
	return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}
}

// ---- response shapes ----------------------------------------------------------

type convResp struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	RemoteID string `json:"remote_id"`
	Created  string `json:"created"`
}

type taskResp struct {
	ID             string `json:"id"`
	Turn           string `json:"turn"`
	RetryOf        string `json:"retry_of"`
	RequestedModel string `json:"requested_model"`
	Thinking       string `json:"thinking"`
	Status         string `json:"status"`
	Result         string `json:"result"`
	ErrorCode      string `json:"error_code"`
	LatencyMS      int64  `json:"latency_ms"`
}

type turnResp struct {
	ID             string     `json:"id"`
	Conversation   string     `json:"conversation"`
	Prompt         string     `json:"prompt"`
	IdempotencyKey string     `json:"idempotency_key"`
	Tasks          []taskResp `json:"tasks"`
	CurrentTask    *taskResp  `json:"current_task"`
}

type providerResp struct {
	Version      string   `json:"version"`
	Bridge       string   `json:"bridge"`
	Models       []string `json:"models"`
	LoggedIn     bool     `json:"logged_in"`
	LoginOp      string   `json:"login_operation"`
	LoginMsg     string   `json:"login_message"`
	Quarantined  bool     `json:"quarantined"`
	RefreshedAt  string   `json:"refreshed_at"`
	WriteBlocked string   `json:"write_blocked"`
}

// ---- helpers that talk to the API ---------------------------------------------

func (e *testEnv) createConversation() convResp {
	e.t.Helper()
	data := e.req(http.MethodPost, "/api/conversations", nil, nil, http.StatusCreated)
	var c convResp
	if err := json.Unmarshal(data, &c); err != nil {
		e.t.Fatalf("decode conversation: %v; body %s", err, data)
	}
	return c
}

func (e *testEnv) createTurn(convID, prompt, key string) turnResp {
	e.t.Helper()
	data := e.req(http.MethodPost, "/api/conversations/"+convID+"/turns",
		map[string]any{"prompt": prompt},
		map[string]string{"Idempotency-Key": key}, http.StatusAccepted)
	var t turnResp
	if err := json.Unmarshal(data, &t); err != nil {
		e.t.Fatalf("decode turn: %v; body %s", err, data)
	}
	return t
}

func (e *testEnv) getTurn(turnID string) turnResp {
	e.t.Helper()
	data := e.req(http.MethodGet, "/api/turns/"+turnID, nil, nil, http.StatusOK)
	var t turnResp
	if err := json.Unmarshal(data, &t); err != nil {
		e.t.Fatalf("decode turn: %v; body %s", err, data)
	}
	return t
}

func (e *testEnv) getProvider() providerResp {
	e.t.Helper()
	data := e.req(http.MethodGet, "/api/providers/gemini", nil, nil, http.StatusOK)
	var p providerResp
	if err := json.Unmarshal(data, &p); err != nil {
		e.t.Fatalf("decode provider: %v; body %s", err, data)
	}
	return p
}

// waitTurnStatus polls GET /api/turns/{id} until current_task reaches want.
func (e *testEnv) waitTurnStatus(turnID, want string, timeout time.Duration) turnResp {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		t := e.getTurn(turnID)
		if t.CurrentTask != nil && t.CurrentTask.Status == want {
			return t
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for task status %q (last: %+v)", want, t.CurrentTask)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitProvider polls the provider endpoint until cond matches.
func (e *testEnv) waitProvider(cond func(providerResp) bool, what string, timeout time.Duration) providerResp {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		p := e.getProvider()
		if cond(p) {
			return p
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for provider state %s (last: %+v)", what, p)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ---- health, auth, admin routes ---------------------------------------------

func TestHealthPublicAndBasicAuth(t *testing.T) {
	env := newEnv(t, withAuth("admin", "s3cret"))

	// health is the only public route
	data := env.req(http.MethodGet, "/api/health", nil, nil, http.StatusOK)
	if !strings.Contains(string(data), `"status":"ok"`) {
		t.Fatalf("health body: %s", data)
	}

	// everything else needs credentials
	data = env.req(http.MethodGet, "/api/conversations", nil, nil, http.StatusUnauthorized)
	if code := decodeErr(t, data).Error.Code; code != "unauthorized" {
		t.Fatalf("envelope code %q, want unauthorized", code)
	}

	// wrong password also 401
	env.req(http.MethodGet, "/api/conversations", nil, basic("admin", "wrong"), http.StatusUnauthorized)

	// correct credentials pass
	env.req(http.MethodGet, "/api/conversations", nil, basic("admin", "s3cret"), http.StatusOK)
}

func TestBasicAuthDeniesEverythingWithUnsetCreds(t *testing.T) {
	// loopback without dev-no-auth and without credentials: auth is on and
	// nobody can ever authenticate (fail closed)
	env := newEnv(t, withAuth("", ""))
	data := env.req(http.MethodGet, "/api/conversations", nil, basic("anything", "atall"), http.StatusUnauthorized)
	if code := decodeErr(t, data).Error.Code; code != "unauthorized" {
		t.Fatalf("expected unauthorized, got %+v", decodeErr(t, data))
	}
	data = env.req(http.MethodGet, "/api/conversations", nil, basic("", ""), http.StatusUnauthorized)
	if code := decodeErr(t, data).Error.Code; code != "unauthorized" {
		t.Fatalf("empty credentials must never authenticate")
	}
}

func TestAdminRoutesAreAbsent(t *testing.T) {
	env := newEnv(t) // dev no-auth
	for _, path := range []string{"/_/", "/_/collections", "/api/collections", "/api/settings", "/api/admins"} {
		env.req(http.MethodGet, path, nil, nil, http.StatusNotFound)
	}
}

func TestHealthNeverExecsOpenCLI(t *testing.T) {
	// a nonexistent opencli path would fail any probe; health must still
	// answer because it only touches the backend and SQLite
	env := newEnv(t, withExecPath("/no/such/opencli"))
	env.req(http.MethodGet, "/api/health", nil, nil, http.StatusOK)
}

func TestTrustedHostAndOrigin(t *testing.T) {
	env := newEnv(t,
		withAuth("admin", "s3cret"),
		func(c *api.Config) {
			c.TrustedHost = "gemini.test"
			c.TrustedOrigins = []string{"https://gemini.test"}
		},
	)
	good := map[string]string{"Host": "gemini.test", "Origin": "https://gemini.test"}
	for k, v := range basic("admin", "s3cret") {
		good[k] = v
	}

	// wrong host on a read: rejected
	env.req(http.MethodGet, "/api/conversations", nil,
		map[string]string{"Host": "evil.test", "Authorization": good["Authorization"]}, http.StatusForbidden)

	// write with the correct host but no Origin: rejected (fail closed)
	h := map[string]string{"Host": "gemini.test", "Authorization": good["Authorization"]}
	env.req(http.MethodPost, "/api/conversations", nil, h, http.StatusForbidden)

	// wrong origin: rejected
	h2 := map[string]string{"Host": "gemini.test", "Origin": "https://evil.test", "Authorization": good["Authorization"]}
	env.req(http.MethodPost, "/api/conversations", nil, h2, http.StatusForbidden)

	// the correct combination works
	env.req(http.MethodPost, "/api/conversations", nil, good, http.StatusCreated)
}

// ---- conversations ----------------------------------------------------------

func TestConversationCreateAndList(t *testing.T) {
	env := newEnv(t)
	c1 := env.createConversation()
	if c1.Status != "active" {
		t.Fatalf("new conversation status %q, want active", c1.Status)
	}
	time.Sleep(10 * time.Millisecond) // distinct created timestamps for a stable order
	c2 := env.createConversation()
	if c2.Status != "active" {
		t.Fatalf("second conversation status %q, want active", c2.Status)
	}

	data := env.req(http.MethodGet, "/api/conversations", nil, nil, http.StatusOK)
	var page struct {
		Items      []convResp `json:"items"`
		TotalItems int        `json:"totalItems"`
		TotalPages int        `json:"totalPages"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if page.TotalItems != 2 || page.TotalPages != 1 {
		t.Fatalf("totalItems=%d totalPages=%d, want 2/1", page.TotalItems, page.TotalPages)
	}
	if len(page.Items) != 2 || page.Items[0].ID != c2.ID || page.Items[1].ID != c1.ID {
		t.Fatalf("items not newest-first: %+v", page.Items)
	}
	if page.Items[1].Status != "archived" {
		t.Fatalf("first conversation should be archived: %+v", page.Items[1])
	}
}

func TestConversationPaginationInvalid(t *testing.T) {
	env := newEnv(t)
	for _, q := range []string{"page=0", "page=abc", "perPage=0", "perPage=999"} {
		data := env.req(http.MethodGet, "/api/conversations?"+q, nil, nil, http.StatusBadRequest)
		if code := decodeErr(t, data).Error.Code; code != "invalid_request" {
			t.Fatalf("query %q: expected invalid_request, got %+v", q, decodeErr(t, data))
		}
	}
}

func TestCreateConversationBlockedWhileBusyOrQuarantined(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()

	// a pending/running task blocks a new conversation
	t1 := env.createTurn(c.ID, `[FAKE:delay:1500][FAKE:stdout:{"response":"ok"}]`, "k1")
	_, data, _ := env.do(http.MethodPost, "/api/conversations", nil, nil)
	if code := decodeErr(t, data).Error.Code; code != "conversation_busy" {
		t.Fatalf("expected conversation_busy while pending, got %+v", decodeErr(t, data))
	}
	env.waitTurnStatus(t1.ID, "succeeded", 10*time.Second) // let the ask finish

	// a quarantine also blocks new conversations
	c2 := env.createConversation()
	u := env.createTurn(c2.ID, "[FAKE:sentinel]", "k2")
	env.waitTurnStatus(u.ID, "unknown_outcome", 10*time.Second)
	_, data, _ = env.do(http.MethodPost, "/api/conversations", nil, nil)
	if code := decodeErr(t, data).Error.Code; code != "conversation_busy" {
		t.Fatalf("expected conversation_busy while quarantined, got %+v", decodeErr(t, data))
	}

	// after acknowledging, creation works again
	env.req(http.MethodPost, "/api/tasks/"+u.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	env.createConversation()
}

func TestResumeConversationEndpoint(t *testing.T) {
	env := newEnv(t)
	a := env.createConversation()
	// capture a remote id the way the runner would (direct store write)
	if err := env.svc.St.SetConversationRemoteID(context.Background(), a.ID, "aaaa1111aaaa1111"); err != nil {
		t.Fatalf("set remote id: %v", err)
	}
	b := env.createConversation() // archives A

	// resume A: 200, A active, B archived
	data := env.req(http.MethodPost, "/api/conversations/"+a.ID+"/resume", nil, nil, http.StatusOK)
	var c convResp
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("decode resume: %v; body %s", err, data)
	}
	if c.Status != "active" || c.ID != a.ID {
		t.Fatalf("resumed conversation: %+v", c)
	}
	if c.RemoteID != "aaaa1111aaaa1111" {
		t.Fatalf("resume response must carry remote_id, got %q", c.RemoteID)
	}
	data = env.req(http.MethodGet, "/api/conversations/"+b.ID, nil, nil, http.StatusOK)
	var detail struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Status != "archived" {
		t.Fatalf("B must be archived after resume, got %s", detail.Status)
	}
}

func TestResumeConversationRefusals(t *testing.T) {
	env := newEnv(t)
	a := env.createConversation()
	if err := env.svc.St.SetConversationRemoteID(context.Background(), a.ID, "aaaa1111aaaa1111"); err != nil {
		t.Fatalf("set remote id: %v", err)
	}
	b := env.createConversation() // archives A

	// busy: a pending task on the active conversation blocks resume
	t1 := env.createTurn(b.ID, `[FAKE:delay:1500][FAKE:stdout:{"response":"ok"}]`, "k1")
	_, data, _ := env.do(http.MethodPost, "/api/conversations/"+a.ID+"/resume", nil, nil)
	if code := decodeErr(t, data).Error.Code; code != "conversation_busy" {
		t.Fatalf("resume while busy: want conversation_busy, got %+v", decodeErr(t, data))
	}
	env.waitTurnStatus(t1.ID, "succeeded", 10*time.Second)

	// not resumable: a conversation without a remote id
	c := env.createConversation() // archives B, C is active
	_, data, _ = env.do(http.MethodPost, "/api/conversations/"+b.ID+"/resume", nil, nil)
	if code := decodeErr(t, data).Error.Code; code != "conversation_not_resumable" {
		t.Fatalf("resume without remote id: want conversation_not_resumable, got %+v", decodeErr(t, data))
	}

	// missing conversation
	_, data, _ = env.do(http.MethodPost, "/api/conversations/nope/resume", nil, nil)
	if code := decodeErr(t, data).Error.Code; code != "not_found" {
		t.Fatalf("resume missing: want not_found, got %+v", decodeErr(t, data))
	}
	_ = c
}

func TestGetConversationDetailOrdering(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()
	t1 := env.createTurn(c.ID, "[FAKE:echo-args]", "k1")
	env.waitTurnStatus(t1.ID, "succeeded", 10*time.Second)
	t2 := env.createTurn(c.ID, "[FAKE:echo-args]", "k2")
	env.waitTurnStatus(t2.ID, "succeeded", 10*time.Second)

	data := env.req(http.MethodGet, "/api/conversations/"+c.ID, nil, nil, http.StatusOK)
	var d struct {
		ID     string     `json:"id"`
		Status string     `json:"status"`
		Turns  []turnResp `json:"turns"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("decode detail: %v; body %s", err, data)
	}
	if len(d.Turns) != 2 || d.Turns[0].ID != t1.ID || d.Turns[1].ID != t2.ID {
		t.Fatalf("turns not in creation order: %+v", d.Turns)
	}
	if len(d.Turns[0].Tasks) != 1 || d.Turns[0].Tasks[0].Status != "succeeded" {
		t.Fatalf("task not embedded: %+v", d.Turns[0])
	}

	data = env.req(http.MethodGet, "/api/conversations/nope", nil, nil, http.StatusNotFound)
	if code := decodeErr(t, data).Error.Code; code != "not_found" {
		t.Fatalf("missing conversation: expected not_found, got %+v", decodeErr(t, data))
	}
}

// ---- turns -----------------------------------------------------------------

func TestCreateTurnValidation(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()
	base := func(m map[string]any) map[string]any {
		out := map[string]any{"prompt": "hello"}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	tests := []struct {
		name    string
		body    map[string]any
		key     string
		rawJSON bool
	}{
		{"missing-key-header", base(nil), "", false},
		{"oversize-key", base(nil), strings.Repeat("k", 201), false},
		{"unknown-field", base(map[string]any{"bogus": 1}), "k", false},
		{"bad-thinking", base(map[string]any{"thinking": "turbo"}), "k", false},
		{"missing-prompt", base(map[string]any{"prompt": ""}), "k", false},
		{"oversize-prompt", base(map[string]any{"prompt": strings.Repeat("x", 200<<10)}), "k", false},
		{"prompt-over-db-cap", base(map[string]any{"prompt": strings.Repeat("x", 100_001)}), "k", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{}
			if tc.key != "" {
				headers["Idempotency-Key"] = tc.key
			}
			data := env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns", tc.body, headers, http.StatusBadRequest)
			if code := decodeErr(t, data).Error.Code; code != "invalid_request" {
				t.Fatalf("envelope code %q, want invalid_request", code)
			}
		})
	}

	// malformed JSON
	status, data, err := env.doRaw(http.MethodPost, "/api/conversations/"+c.ID+"/turns", "{not json",
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "k"})
	if err != nil || status != http.StatusBadRequest {
		t.Fatalf("malformed json: status %d err %v", status, err)
	}
	if code := decodeErr(t, data).Error.Code; code != "invalid_request" {
		t.Fatalf("malformed json code %q", code)
	}

	// a non-JSON body is rejected (JSON only)
	status, data, err = env.doRaw(http.MethodPost, "/api/conversations/"+c.ID+"/turns", "prompt=hello",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Idempotency-Key": "k"})
	if err != nil || status != http.StatusBadRequest {
		t.Fatalf("non-json: status %d err %v", status, err)
	}
}

func TestCreateTurnIdempotencyReplay(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()
	body := map[string]any{"prompt": "[FAKE:sentinel]", "model": "2.5-flash", "thinking": "standard"}
	headers := map[string]string{"Idempotency-Key": "k1"}

	data := env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns", body, headers, http.StatusAccepted)
	var first turnResp
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	// the sentinel turn goes unknown → archive: replay must still return
	// the original turn because idempotency precedes state checks
	env.waitTurnStatus(first.ID, "unknown_outcome", 10*time.Second)

	data = env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns", body, headers, http.StatusAccepted)
	var replay turnResp
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replay.ID != first.ID || len(replay.Tasks) != 1 {
		t.Fatalf("replay must return the original turn with no new task: %+v vs %+v", replay, first)
	}

	// same key, different body → 409
	data = env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns",
		map[string]any{"prompt": "different"}, headers, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "idempotency_conflict" {
		t.Fatalf("envelope code %q, want idempotency_conflict", code)
	}

	// the same key on another conversation is a fresh turn (after ack to
	// lift the quarantine)
	env.req(http.MethodPost, "/api/tasks/"+first.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	c2 := env.createConversation()
	env.createTurn(c2.ID, "[FAKE:echo-args]", "k1")
}

func TestCreateTurnGuards(t *testing.T) {
	// hold the worker with a slow login op so the ask stays queued as
	// pending and the turn guards are deterministic
	env := newEnv(t, withScenario(t, map[string]string{"login": `{"delay_ms":1500}`}))
	env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusAccepted)

	c2 := env.createConversation()
	p := env.createTurn(c2.ID, "[FAKE:echo-args]", "k1")

	// unfinished previous turn blocks the next one
	data := env.req(http.MethodPost, "/api/conversations/"+c2.ID+"/turns",
		map[string]any{"prompt": "next"}, map[string]string{"Idempotency-Key": "k2"}, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "turn_unfinished" {
		t.Fatalf("unfinished: expected turn_unfinished, got %+v", decodeErr(t, data))
	}

	// cancel while still queued → canceled, then the next turn is blocked
	env.req(http.MethodPost, "/api/tasks/"+p.CurrentTask.ID+"/cancel", nil, nil, http.StatusOK)
	data = env.req(http.MethodPost, "/api/conversations/"+c2.ID+"/turns",
		map[string]any{"prompt": "next"}, map[string]string{"Idempotency-Key": "k2"}, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "previous_turn_not_succeeded" {
		t.Fatalf("prev-not-succeeded: expected previous_turn_not_succeeded, got %+v", decodeErr(t, data))
	}

	// archived conversation rejects new turns (sentinel → unknown → archive)
	c := env.createConversation()
	u := env.createTurn(c.ID, "[FAKE:sentinel]", "k1")
	env.waitTurnStatus(u.ID, "unknown_outcome", 10*time.Second)
	data = env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns",
		map[string]any{"prompt": "again"}, map[string]string{"Idempotency-Key": "k2"}, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "conversation_archived" {
		t.Fatalf("archived: expected conversation_archived, got %+v", decodeErr(t, data))
	}
}

func TestQueueFullBackstopLeavesNoRows(t *testing.T) {
	env := newEnv(t, withCapacity(1))
	c := env.createConversation()

	// 4 racing submissions on a fresh conversation: the capacity and the
	// turn guards guarantee exactly one 202 and one DB row
	var wg sync.WaitGroup
	statuses := make([]int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			status, _, err := env.do(http.MethodPost, "/api/conversations/"+c.ID+"/turns",
				map[string]any{"prompt": fmt.Sprintf("[FAKE:delay:100][FAKE:stdout:{\"response\":\"ok%d\"}]", n)},
				map[string]string{"Idempotency-Key": fmt.Sprintf("k%d", n)})
			if err == nil {
				statuses[n] = status
			}
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, s := range statuses {
		switch s {
		case http.StatusAccepted:
			ok++
		case http.StatusTooManyRequests, http.StatusConflict:
			// 429 (capacity backstop) or 409 (turn guard) — both fine
		default:
			t.Fatalf("unexpected status %d", s)
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one submission must succeed, got %d (statuses %v)", ok, statuses)
	}

	var turns int64
	if err := env.svc.St.DB().NewQuery(
		`SELECT COUNT(*) FROM {{turns}} WHERE [[conversation]] = {:c}`,
	).Bind(map[string]any{"c": c.ID}).Row(&turns); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if turns != 1 {
		t.Fatalf("exactly one turn must exist, got %d", turns)
	}
}

// ---- the required end-to-end integration check -------------------------------

func TestTurnFlowIntegration(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()

	// echo-args makes the fake answer with the exact argv it received
	turn := env.createTurn(c.ID, "[FAKE:echo-args]", "k1")
	if turn.CurrentTask == nil ||
		(turn.CurrentTask.Status != "pending" && turn.CurrentTask.Status != "running") {
		t.Fatalf("task should start pending (or already running): %+v", turn.CurrentTask)
	}

	// poll the turn endpoint to terminal state
	end := env.waitTurnStatus(turn.ID, "succeeded", 10*time.Second)
	task := end.CurrentTask
	if !strings.Contains(task.Result, "test-profile") || !strings.Contains(task.Result, "gemini\nask") {
		t.Fatalf("result should contain the argv echo, got %q", task.Result)
	}
	if len(end.Tasks) != 1 || end.Tasks[0].ID != task.ID {
		t.Fatalf("current_task must be the single task: %+v", end)
	}
}

// ---- retry / cancel / acknowledge -------------------------------------------

func TestRetryCancelledTask(t *testing.T) {
	// hold the worker with a slow login so the ask stays queued (pending)
	env := newEnv(t, withScenario(t, map[string]string{"login": `{"delay_ms":1500}`}))
	env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusAccepted)
	c := env.createConversation()
	turn := env.createTurn(c.ID, "[FAKE:echo-args]", "k1")

	data := env.req(http.MethodPost, "/api/tasks/"+turn.CurrentTask.ID+"/cancel", nil, nil, http.StatusOK)
	var canceled taskResp
	if err := json.Unmarshal(data, &canceled); err != nil || canceled.Status != "canceled" {
		t.Fatalf("cancel should return the canceled task: %+v / %s", canceled, data)
	}

	// retry is allowed for canceled tasks on an active conversation
	data = env.req(http.MethodPost, "/api/tasks/"+canceled.ID+"/retry", nil, nil, http.StatusAccepted)
	var retry taskResp
	if err := json.Unmarshal(data, &retry); err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	if retry.RetryOf != canceled.ID || retry.Status != "pending" {
		t.Fatalf("retry must link retry_of and start pending: %+v", retry)
	}

	// both tasks are visible on the turn, newest last, retry is current
	end := env.waitTurnStatus(turn.ID, "succeeded", 10*time.Second)
	if len(end.Tasks) != 2 || end.Tasks[1].ID != retry.ID || end.CurrentTask.ID != retry.ID {
		t.Fatalf("turn tasks/current wrong: %+v", end)
	}

	// succeeded and unknown tasks are not retryable
	data = env.req(http.MethodPost, "/api/tasks/"+retry.ID+"/retry", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "task_not_retryable" {
		t.Fatalf("retry succeeded: expected task_not_retryable, got %+v", decodeErr(t, data))
	}

	// missing task → 404
	data = env.req(http.MethodPost, "/api/tasks/nope/retry", nil, nil, http.StatusNotFound)
	if code := decodeErr(t, data).Error.Code; code != "not_found" {
		t.Fatalf("retry missing: expected not_found, got %+v", decodeErr(t, data))
	}
}

func TestCancelTerminalAndMissing(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()
	turn := env.createTurn(c.ID, "[FAKE:echo-args]", "k1")
	end := env.waitTurnStatus(turn.ID, "succeeded", 10*time.Second)

	data := env.req(http.MethodPost, "/api/tasks/"+end.CurrentTask.ID+"/cancel", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "task_not_pending" {
		t.Fatalf("cancel succeeded task: expected task_not_pending, got %+v", decodeErr(t, data))
	}
	data = env.req(http.MethodPost, "/api/tasks/nope/cancel", nil, nil, http.StatusNotFound)
	if code := decodeErr(t, data).Error.Code; code != "not_found" {
		t.Fatalf("cancel missing: expected not_found, got %+v", decodeErr(t, data))
	}
}

func TestAcknowledgeUnknownLiftsQuarantine(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()
	turn := env.createTurn(c.ID, "[FAKE:sentinel]", "k1")
	env.waitTurnStatus(turn.ID, "unknown_outcome", 10*time.Second)

	if p := env.getProvider(); !p.Quarantined {
		t.Fatalf("provider must be quarantined after an unknown outcome: %+v", p)
	}

	// acknowledge → 204, quarantine lifted
	env.req(http.MethodPost, "/api/tasks/"+turn.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	if p := env.getProvider(); p.Quarantined {
		t.Fatalf("provider should not be quarantined after ack: %+v", p)
	}

	// idempotent re-ack
	env.req(http.MethodPost, "/api/tasks/"+turn.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)

	// acknowledging a succeeded task is a conflict
	c2 := env.createConversation()
	t2 := env.createTurn(c2.ID, "[FAKE:echo-args]", "k1")
	end := env.waitTurnStatus(t2.ID, "succeeded", 10*time.Second)
	data := env.req(http.MethodPost, "/api/tasks/"+end.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "task_not_unknown" {
		t.Fatalf("ack succeeded: expected task_not_unknown, got %+v", decodeErr(t, data))
	}
}

// ---- provider cache, login, refresh -----------------------------------------

func TestProviderLoginFlow(t *testing.T) {
	env := newEnv(t, withScenario(t, map[string]string{"login": `{"delay_ms":400}`}))

	if p := env.getProvider(); p.LoginOp != "idle" {
		t.Fatalf("initial login op should be idle: %+v", p)
	}

	env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusAccepted)

	// a queued/running duplicate is a conflict (the fake login takes 400ms)
	data := env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "login_in_progress" {
		t.Fatalf("duplicate login: expected login_in_progress, got %+v", decodeErr(t, data))
	}

	// eventually succeeded
	env.waitProvider(func(p providerResp) bool { return p.LoginOp == "succeeded" }, "login succeeded", 5*time.Second)

	// a new login can replace the terminal state
	env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusAccepted)
}

func TestLoginBlocked(t *testing.T) {
	// quarantined Gemini blocks login
	env := newEnv(t, withScenario(t, map[string]string{"login": `{"delay_ms":400}`}))
	c := env.createConversation()
	u := env.createTurn(c.ID, "[FAKE:sentinel]", "k1")
	env.waitTurnStatus(u.ID, "unknown_outcome", 10*time.Second)
	data := env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "login_blocked" {
		t.Fatalf("quarantined login: expected login_blocked, got %+v", decodeErr(t, data))
	}

	// an active conversation with a successful turn blocks login
	env.req(http.MethodPost, "/api/tasks/"+u.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	c2 := env.createConversation()
	s := env.createTurn(c2.ID, "[FAKE:echo-args]", "k1")
	env.waitTurnStatus(s.ID, "succeeded", 10*time.Second)
	data = env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "login_blocked" {
		t.Fatalf("active-success login: expected login_blocked, got %+v", decodeErr(t, data))
	}
}

func TestLoginParksDuringQuarantine(t *testing.T) {
	env := newEnv(t, withScenario(t, map[string]string{"login": `{"delay_ms":400}`}))

	// op1: a slow ask that ends in unknown (sentinel); op2: login queued
	// while the ask is still running. Login must never execute inside the
	// quarantine — it parks until the user acknowledges.
	c := env.createConversation()
	turn := env.createTurn(c.ID, "[FAKE:delay:1500][FAKE:sentinel]", "k1")
	env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusAccepted)

	env.waitTurnStatus(turn.ID, "unknown_outcome", 10*time.Second)

	// during quarantine the login stays queued — nothing has run
	p := env.getProvider()
	if !p.Quarantined || p.LoginOp != "queued" {
		t.Fatalf("login must be parked (queued) during quarantine: %+v", p)
	}

	// acknowledging releases the park and the login runs to completion
	env.req(http.MethodPost, "/api/tasks/"+turn.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	env.waitProvider(func(p providerResp) bool { return p.LoginOp == "succeeded" }, "login after ack", 5*time.Second)
}

func TestProviderRefreshPopulatesCache(t *testing.T) {
	env := newEnv(t, withScenario(t, map[string]string{
		"version": `{"stdout":"1.8.7"}`,
		"doctor":  `{"stdout":"bridge 1.0.23 ok"}`,
		"models":  `{"stdout":"{\"models\":[\"2.5-flash\",\"2.5-pro\"]}"}`,
		"status":  `{"stdout":"{\"logged_in\":true}"}`,
		"whoami":  `{"stdout":"{\"logged_in\":true}"}`,
	}))

	env.prov.MaybeRefresh()
	env.waitProvider(func(p providerResp) bool {
		return p.Version == "1.8.7" && len(p.Models) == 2 && p.LoggedIn && p.RefreshedAt != ""
	}, "cache populated", 10*time.Second)
	if p := env.getProvider(); p.Bridge != "bridge 1.0.23 ok" {
		t.Fatalf("bridge %q", p.Bridge)
	}
}

func TestProviderRefreshEndpoint(t *testing.T) {
	env := newEnv(t, withScenario(t, map[string]string{
		"version": `{"stdout":"1.8.7","delay_ms":400}`,
		"models":  `{"stdout":"{\"models\":[\"2.5-flash\"]}"}`,
	}))

	// the on-demand endpoint enqueues a probe and populates the cache
	env.req(http.MethodPost, "/api/providers/gemini/refresh", nil, nil, http.StatusAccepted)

	// a second refresh while one is queued/running is a conflict
	env.req(http.MethodPost, "/api/providers/gemini/refresh", nil, nil, http.StatusConflict)

	env.waitProvider(func(p providerResp) bool {
		return p.Version == "1.8.7" && len(p.Models) == 1 && p.RefreshedAt != ""
	}, "refresh via endpoint", 10*time.Second)
}

func TestProviderRefreshBlocked(t *testing.T) {
	env := newEnv(t, withScenario(t, map[string]string{"version": `{"stdout":"1.8.7"}`}))

	// quarantined Gemini blocks the on-demand refresh
	c := env.createConversation()
	u := env.createTurn(c.ID, "[FAKE:sentinel]", "k1")
	env.waitTurnStatus(u.ID, "unknown_outcome", 10*time.Second)
	data := env.req(http.MethodPost, "/api/providers/gemini/refresh", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "refresh_blocked" {
		t.Fatalf("quarantined refresh: expected refresh_blocked, got %+v", decodeErr(t, data))
	}

	// an active conversation with a successful turn also blocks refresh
	env.req(http.MethodPost, "/api/tasks/"+u.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	c2 := env.createConversation()
	s := env.createTurn(c2.ID, "[FAKE:echo-args]", "k1")
	env.waitTurnStatus(s.ID, "succeeded", 10*time.Second)
	data = env.req(http.MethodPost, "/api/providers/gemini/refresh", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "refresh_blocked" {
		t.Fatalf("active-success refresh: expected refresh_blocked, got %+v", decodeErr(t, data))
	}
}

func TestProviderRefreshSkippedWhileQuarantinedOrAfterSuccess(t *testing.T) {
	env := newEnv(t, withScenario(t, map[string]string{"version": `{"stdout":"1.8.7"}`}))

	// quarantine blocks the refresh
	c := env.createConversation()
	u := env.createTurn(c.ID, "[FAKE:sentinel]", "k1")
	env.waitTurnStatus(u.ID, "unknown_outcome", 10*time.Second)
	env.prov.MaybeRefresh()
	time.Sleep(300 * time.Millisecond) // give a wrongly-enqueued op time to run
	if p := env.getProvider(); p.Version != "" {
		t.Fatalf("refresh must not run during quarantine, got %+v", p)
	}

	// an active conversation with a successful turn also blocks the refresh
	// (whoami/models/doctor would touch the shared tab)
	env.req(http.MethodPost, "/api/tasks/"+u.CurrentTask.ID+"/acknowledge-unknown", nil, nil, http.StatusNoContent)
	c2 := env.createConversation()
	s := env.createTurn(c2.ID, "[FAKE:echo-args]", "k1")
	env.waitTurnStatus(s.ID, "succeeded", 10*time.Second)
	env.prov.MaybeRefresh()
	time.Sleep(300 * time.Millisecond)
	if p := env.getProvider(); p.Version != "" {
		t.Fatalf("refresh must not run after a successful turn, got %+v", p)
	}
}

// ---- write guard (adapter override / plugin / version mismatch) ----------

func mkHomeOverride(t *testing.T, rel string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, filepath.Dir(rel)), 0o700); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, rel), []byte("x"), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	t.Setenv("HOME", home)
}

func TestWriteGuardAdapterOverride(t *testing.T) {
	env := newEnv(t)

	// a normal turn first — replay must still work after the guard closes
	c := env.createConversation()
	first := env.createTurn(c.ID, "[FAKE:echo-args]", "k1")
	env.waitTurnStatus(first.ID, "succeeded", 10*time.Second)

	// local adapter override: writes fail closed from now on
	mkHomeOverride(t, ".opencli/clis/gemini")

	data := env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns",
		map[string]any{"prompt": "hello again"}, map[string]string{"Idempotency-Key": "k2"}, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "adapter_override" {
		t.Fatalf("override: expected adapter_override, got %+v", decodeErr(t, data))
	}

	// login is a write too
	data = env.req(http.MethodPost, "/api/providers/gemini/login", nil, nil, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "adapter_override" {
		t.Fatalf("login under override: expected adapter_override, got %+v", decodeErr(t, data))
	}

	// the GET provider endpoint surfaces the blocked reason
	if p := env.getProvider(); p.WriteBlocked != "adapter_override" {
		t.Fatalf("snapshot write_blocked = %q, want adapter_override", p.WriteBlocked)
	}

	// replay of an already-created turn still returns the original turn
	data = env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns",
		map[string]any{"prompt": "[FAKE:echo-args]"}, map[string]string{"Idempotency-Key": "k1"}, http.StatusAccepted)
	var replay turnResp
	if err := json.Unmarshal(data, &replay); err != nil || replay.ID != first.ID {
		t.Fatalf("replay under override must return the original turn: %+v / %s", replay, data)
	}
}

func TestWriteGuardPluginInstalled(t *testing.T) {
	env := newEnv(t)
	c := env.createConversation()
	mkHomeOverride(t, ".opencli/plugins/something")

	data := env.req(http.MethodPost, "/api/conversations/"+c.ID+"/turns",
		map[string]any{"prompt": "hello"}, map[string]string{"Idempotency-Key": "k1"}, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "plugin_installed" {
		t.Fatalf("plugin: expected plugin_installed, got %+v", decodeErr(t, data))
	}
}

func TestWriteGuardVersionMismatch(t *testing.T) {
	// before the version probe runs, writes are allowed (unknown version)
	env := newEnv(t)
	c := env.createConversation()
	env.createTurn(c.ID, "[FAKE:echo-args]", "k1")

	// a fresh deployment with a mismatched opencli: the startup probe
	// reports the bad version and writes fail closed afterwards.
	// (the refresh must run before any successful turn exists, because the
	// provider refresh is paused while an active conversation has success)
	env2 := newEnv(t, withScenario(t, map[string]string{"version": `{"stdout":"9.9.9"}`}))
	c2 := env2.createConversation()
	env2.prov.MaybeRefresh()
	env2.waitProvider(func(p providerResp) bool { return p.Version == "9.9.9" }, "mismatched version", 10*time.Second)

	data := env2.req(http.MethodPost, "/api/conversations/"+c2.ID+"/turns",
		map[string]any{"prompt": "hello"}, map[string]string{"Idempotency-Key": "k2"}, http.StatusConflict)
	if code := decodeErr(t, data).Error.Code; code != "version_mismatch" {
		t.Fatalf("version mismatch: expected version_mismatch, got %+v", decodeErr(t, data))
	}
	if p := env2.getProvider(); p.WriteBlocked != "version_mismatch" {
		t.Fatalf("snapshot write_blocked = %q, want version_mismatch", p.WriteBlocked)
	}
}
