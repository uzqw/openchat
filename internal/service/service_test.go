package service_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openchat/internal/queue"
	"openchat/internal/service"
	"openchat/internal/store"
)

var fakePath string

// TestMain builds the fake opencli once; every test runs against it, so no
// real Gemini account or Chrome is ever touched (hard relay rule).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fake-opencli-")
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

type svcOpt func(*service.Config)

func buildService(t *testing.T, opts ...svcOpt) *service.Service {
	t.Helper()
	cfg := service.Config{
		DataDir:        t.TempDir(), // temp data dir; production pb_data is never touched
		ExecPath:       fakePath,
		Profile:        "test-profile",
		QueueCapacity:  1,
		AskTimeout:     5 * time.Second,
		MaxStdoutBytes: 64 << 10,
		MaxStderrBytes: 16 << 10,
	}
	for _, o := range opts {
		o(&cfg)
	}
	svc, err := service.New(cfg)
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	return svc
}

// newTestService builds, starts and cleans up a service.
func newTestService(t *testing.T, opts ...svcOpt) *service.Service {
	t.Helper()
	svc := buildService(t, opts...)
	svc.Start()
	t.Cleanup(svc.Close)
	return svc
}

func withNoStart(t *testing.T, svc *service.Service) *service.Service {
	t.Cleanup(svc.Close)
	return svc
}

// withAskTimeout overrides the ask timeout (and thus --timeout).
func withAskTimeout(d time.Duration) svcOpt {
	return func(c *service.Config) { c.AskTimeout = d }
}

func withMaxStdout(n int) svcOpt {
	return func(c *service.Config) { c.MaxStdoutBytes = n }
}

func withExecPath(p string) svcOpt {
	return func(c *service.Config) { c.ExecPath = p }
}

func withCapacity(n int) svcOpt {
	return func(c *service.Config) { c.QueueCapacity = n }
}

var bctx = context.Background()

func createConversation(t *testing.T, svc *service.Service) *store.Conversation {
	t.Helper()
	conv, err := svc.CreateConversation(bctx)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return conv
}

func createTurn(t *testing.T, svc *service.Service, convID, prompt, key string, model, thinking string) (*store.Turn, *store.Task) {
	t.Helper()
	turn, task, err := svc.CreateTurn(bctx, store.TurnRequest{
		ConversationID: convID, Prompt: prompt, IdempotencyKey: key, Model: model, Thinking: thinking,
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	return turn, task
}

// waitStatus polls until the task reaches want or the deadline passes.
func waitStatus(t *testing.T, svc *service.Service, taskID, want string, timeout time.Duration) *store.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *store.Task
	for {
		task, err := svc.St.TaskByID(bctx, taskID)
		if err != nil {
			t.Fatalf("TaskByID(%s): %v", taskID, err)
		}
		last = task
		if task.Status == want {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s: status %s, want %s (code %s msg %q)", taskID, last.Status, want, last.ErrorCode, last.ErrorMessage)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTurnLifecycleSucceeds(t *testing.T) {
	svc := newTestService(t)
	conv := createConversation(t, svc)
	key := "turn-1"
	_, task := createTurn(t, svc, conv.ID, `[FAKE:delay:150][FAKE:stdout:{"response":"你好，Gemini"}]`, key, "", "")

	// pending → running → succeeded, exactly like the integration check
	if got := waitStatus(t, svc, task.ID, store.TaskRunning, 2*time.Second); got.Result != "" {
		t.Fatalf("running task must not carry a result")
	}
	done := waitStatus(t, svc, task.ID, store.TaskSucceeded, 5*time.Second)
	if done.Result != "你好，Gemini" {
		t.Fatalf("result = %q, want the fake answer", done.Result)
	}
	if done.ErrorCode != "" || done.LatencyMS < 0 {
		t.Fatalf("unexpected task fields: code=%q latency=%d", done.ErrorCode, done.LatencyMS)
	}
	conv, err := svc.St.ConversationByID(bctx, conv.ID)
	if err != nil {
		t.Fatalf("conversation lookup: %v", err)
	}
	if conv.Status != store.ConvActive {
		t.Fatalf("conversation must stay active after a success, got %s", conv.Status)
	}
	if !strings.HasPrefix(conv.Title, "[FAKE:") {
		t.Fatalf("title should be the truncated first prompt, got %q", conv.Title)
	}
}

func TestArgvContractEndToEnd(t *testing.T) {
	svc := newTestService(t)
	conv := createConversation(t, svc)
	_, task := createTurn(t, svc, conv.ID, `[FAKE:echo-args]`, "k1", "2.5-flash", store.ThinkingStandard)
	done := waitStatus(t, svc, task.ID, store.TaskSucceeded, 5*time.Second)

	// the echo-args marker surfaces the exact argv as the answer
	for _, want := range []string{
		"--profile\ntest-profile",
		"gemini\nask",
		"--new\ntrue", // first turn opens a fresh site session
		"--model\n2.5-flash",
		"--thinking\nstandard",
		"--timeout\n5",
		"--format\njson",
	} {
		if !strings.Contains(done.Result, want) {
			t.Fatalf("argv missing %q in:\n%s", want, done.Result)
		}
	}

	// follow-up turn: no --new, same persistent session
	_, task2 := createTurn(t, svc, conv.ID, `[FAKE:echo-args]`, "k2", "", "")
	done2 := waitStatus(t, svc, task2.ID, store.TaskSucceeded, 5*time.Second)
	if strings.Contains(done2.Result, "--new") {
		t.Fatalf("follow-up turn must not pass --new:\n%s", done2.Result)
	}
}

func TestAuthRequiredAfterSuccessArchives(t *testing.T) {
	svc := newTestService(t)
	conv := createConversation(t, svc)
	_, t1 := createTurn(t, svc, conv.ID, `[FAKE:stdout:{"response":"ok"}]`, "k1", "", "")
	waitStatus(t, svc, t1.ID, store.TaskSucceeded, 5*time.Second)

	_, t2 := createTurn(t, svc, conv.ID, `[FAKE:exit:77]`, "k2", "", "")
	done := waitStatus(t, svc, t2.ID, store.TaskAuthRequired, 5*time.Second)
	if done.ErrorCode != store.ErrorCodeAuthRequired {
		t.Fatalf("error_code = %q", done.ErrorCode)
	}
	conv, _ = svc.St.ConversationByID(bctx, conv.ID)
	if conv.Status != store.ConvArchived {
		t.Fatalf("auth_required after a success must archive the conversation, got %s", conv.Status)
	}
	// archived conversations cannot retry, and nothing may be quarantined
	if _, err := svc.RetryTask(bctx, t2.ID); !errors.Is(err, store.ErrConversationArchived) {
		t.Fatalf("retry on archived conversation: want ErrConversationArchived, got %v", err)
	}
}

func TestAuthRequiredFirstTurnRetryable(t *testing.T) {
	svc := newTestService(t)
	conv := createConversation(t, svc)
	onceFile := filepath.Join(t.TempDir(), "once")
	prompt := `[FAKE:exit-once:77:` + onceFile + `][FAKE:echo-args]`
	_, t1 := createTurn(t, svc, conv.ID, prompt, "k1", "2.5-flash", store.ThinkingExtended)

	done := waitStatus(t, svc, t1.ID, store.TaskAuthRequired, 5*time.Second)
	if done.ErrorCode != store.ErrorCodeAuthRequired {
		t.Fatalf("error_code = %q, want auth_required", done.ErrorCode)
	}
	conv, _ = svc.St.ConversationByID(bctx, conv.ID)
	if conv.Status != store.ConvActive {
		t.Fatalf("first-turn auth_required must keep the conversation active, got %s", conv.Status)
	}

	// retry: copies model/thinking, links retry_of, and still uses --new true
	retry, err := svc.RetryTask(bctx, t1.ID)
	if err != nil {
		t.Fatalf("RetryTask: %v", err)
	}
	if retry.RetryOf != t1.ID || retry.RequestedModel != "2.5-flash" || retry.Thinking != store.ThinkingExtended {
		t.Fatalf("retry must copy the original parameters: %+v", retry)
	}
	waitStatus(t, svc, retry.ID, store.TaskSucceeded, 5*time.Second)
	retry, _ = svc.St.TaskByID(bctx, retry.ID)
	if !strings.Contains(retry.Result, "--new\ntrue") {
		t.Fatalf("first-turn retry must keep --new true:\n%s", retry.Result)
	}
}

func TestSentinelArchivesAndQuarantines(t *testing.T) {
	svc := newTestService(t)
	conv := createConversation(t, svc)
	_, task := createTurn(t, svc, conv.ID, `[FAKE:sentinel]`, "k1", "", "")
	done := waitStatus(t, svc, task.ID, store.TaskUnknownOutcome, 5*time.Second)
	if done.ErrorCode != store.ErrorCodeNoResponse {
		t.Fatalf("error_code = %q, want no_response", done.ErrorCode)
	}
	if done.Result != "" {
		t.Fatalf("sentinel text must never be saved as a result: %q", done.Result)
	}
	if q, _ := svc.IsQuarantined(bctx); !q {
		t.Fatal("Gemini must be quarantined")
	}
	if _, err := svc.CreateConversation(bctx); !errors.Is(err, store.ErrConversationBusy) {
		t.Fatalf("new conversation while quarantined: want ErrConversationBusy, got %v", err)
	}
	lifted, err := svc.AcknowledgeUnknown(bctx, task.ID)
	if err != nil || !lifted {
		t.Fatalf("acknowledge: lifted=%v err=%v", lifted, err)
	}
	if q, _ := svc.IsQuarantined(bctx); q {
		t.Fatal("quarantine must clear after the last unknown is acknowledged")
	}
	if _, err := svc.CreateConversation(bctx); err != nil {
		t.Fatalf("create conversation after acknowledge: %v", err)
	}
}

func TestTimeoutUnknownAndQuarantine(t *testing.T) {
	svc := newTestService(t, withAskTimeout(300*time.Millisecond))
	conv := createConversation(t, svc)
	_, task := createTurn(t, svc, conv.ID, `[FAKE:timeout]`, "k1", "", "")
	done := waitStatus(t, svc, task.ID, store.TaskUnknownOutcome, 5*time.Second)
	if done.ErrorCode != store.ErrorCodeTimeout {
		t.Fatalf("error_code = %q, want timeout", done.ErrorCode)
	}
	if q, _ := svc.IsQuarantined(bctx); !q {
		t.Fatal("timeout must quarantine Gemini")
	}
	conv, _ = svc.St.ConversationByID(bctx, conv.ID)
	if conv.Status != store.ConvArchived {
		t.Fatalf("unknown must archive the conversation, got %s", conv.Status)
	}
}

func TestOutputOverflowUnknown(t *testing.T) {
	svc := newTestService(t, withMaxStdout(1024))
	conv := createConversation(t, svc)
	_, task := createTurn(t, svc, conv.ID, `[FAKE:bytes:65536]`, "k1", "", "")
	done := waitStatus(t, svc, task.ID, store.TaskUnknownOutcome, 5*time.Second)
	if done.ErrorCode != store.ErrorCodeOutputOverflow {
		t.Fatalf("error_code = %q, want output_overflow", done.ErrorCode)
	}
	// a truncated stdout must never be stored as a success
	if done.Result != "" {
		t.Fatalf("captured %d bytes stored as result", len(done.Result))
	}
	if q, _ := svc.IsQuarantined(bctx); !q {
		t.Fatal("overflow must quarantine Gemini")
	}
}

func TestBadJSONAndExitCodesAreUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt string
		code   string
	}{
		{"invalid-json", `[FAKE:stdout:not-json]`, store.ErrorCodeInvalidOutput},
		{"exit-66", `[FAKE:exit:66]`, store.ErrorCodeBadExit},
		{"exit-2", `[FAKE:exit:2]`, store.ErrorCodeBadExit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			conv := createConversation(t, svc)
			_, task := createTurn(t, svc, conv.ID, tc.prompt, "k1", "", "")
			done := waitStatus(t, svc, task.ID, store.TaskUnknownOutcome, 5*time.Second)
			if done.ErrorCode != tc.code {
				t.Fatalf("error_code = %q, want %q", done.ErrorCode, tc.code)
			}
		})
	}
}

func TestSpawnFailureIsFailed(t *testing.T) {
	svc := newTestService(t, withExecPath("/no/such/opencli"))
	conv := createConversation(t, svc)
	_, task := createTurn(t, svc, conv.ID, `[FAKE:stdout:{"response":"never"}]`, "k1", "", "")
	done := waitStatus(t, svc, task.ID, store.TaskFailed, 5*time.Second)
	if done.ErrorCode != store.ErrorCodeSpawn {
		t.Fatalf("error_code = %q, want spawn_failed", done.ErrorCode)
	}
	// failure is a local proof, not an unknown: conversation stays active
	conv, _ = svc.St.ConversationByID(bctx, conv.ID)
	if conv.Status != store.ConvActive {
		t.Fatalf("failed must not archive the conversation, got %s", conv.Status)
	}
	// ... and it is retryable
	if _, err := svc.RetryTask(bctx, task.ID); err != nil {
		t.Fatalf("failed task must be retryable: %v", err)
	}
}

func TestCancelQueuedAndRunning(t *testing.T) {
	t.Run("queued-cancel", func(t *testing.T) {
		svc := withNoStart(t, buildService(t)) // worker stopped: the task stays queued as pending
		conv := createConversation(t, svc)
		_, task := createTurn(t, svc, conv.ID, `[FAKE:delay:50][FAKE:stdout:{"response":"no"}]`, "k1", "", "")
		if task.Status != store.TaskPending {
			t.Fatalf("task must be pending, got %s", task.Status)
		}
		if err := svc.CancelTask(bctx, task.ID); err != nil {
			t.Fatalf("CancelTask: %v", err)
		}
		task, _ = svc.St.TaskByID(bctx, task.ID)
		if task.Status != store.TaskCanceled {
			t.Fatalf("task must be canceled, got %s", task.Status)
		}
		// canceled is terminal: a new conversation is allowed again
		if _, err := svc.CreateConversation(bctx); err != nil {
			t.Fatalf("create conversation after cancel: %v", err)
		}
		// double cancel and cancel of a non-pending task → 409 semantics
		if err := svc.CancelTask(bctx, task.ID); !errors.Is(err, store.ErrTaskNotPending) {
			t.Fatalf("double cancel: want ErrTaskNotPending, got %v", err)
		}
	})

	t.Run("running-cancel-409", func(t *testing.T) {
		svc := newTestService(t)
		conv := createConversation(t, svc)
		_, task := createTurn(t, svc, conv.ID, `[FAKE:delay:2000][FAKE:stdout:{"response":"ok"}]`, "k1", "", "")
		waitStatus(t, svc, task.ID, store.TaskRunning, 2*time.Second)
		if err := svc.CancelTask(bctx, task.ID); !errors.Is(err, store.ErrTaskNotPending) {
			t.Fatalf("running cancel: want ErrTaskNotPending, got %v", err)
		}
	})
}

func TestIdempotencyReplayPrecedesCapacity(t *testing.T) {
	svc := newTestService(t)
	conv := createConversation(t, svc)
	turn1, task1 := createTurn(t, svc, conv.ID, `[FAKE:delay:800][FAKE:stdout:{"response":"ok"}]`, "K", "m1", "")

	// the queue is full (slot held by the slow running ask) yet the replay
	// must be served from the database, not rejected with 429
	waitStatus(t, svc, task1.ID, store.TaskRunning, 2*time.Second)
	turn2, task2, err := svc.CreateTurn(bctx, store.TurnRequest{
		ConversationID: conv.ID, Prompt: `[FAKE:delay:800][FAKE:stdout:{"response":"ok"}]`, IdempotencyKey: "K", Model: "m1",
	})
	if err != nil {
		t.Fatalf("replay while queue full must return the original turn, got %v", err)
	}
	if turn2.ID != turn1.ID || task2.ID != task1.ID {
		t.Fatalf("replay returned a different turn/task: %s/%s vs %s/%s", turn2.ID, task2.ID, turn1.ID, task1.ID)
	}

	// same key with a different body → conflict
	_, _, err = svc.CreateTurn(bctx, store.TurnRequest{
		ConversationID: conv.ID, Prompt: "different", IdempotencyKey: "K",
	})
	if !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("different body with same key: want ErrIdempotencyConflict, got %v", err)
	}

	// the same key in another conversation is reusable
	conv2 := createConversation2(t, svc)
	_, _, err = svc.CreateTurn(bctx, store.TurnRequest{
		ConversationID: conv2.ID, Prompt: "again", IdempotencyKey: "K",
	})
	if err != nil {
		t.Fatalf("idempotency key must be reusable across conversations: %v", err)
	}
}

// createConversation2 waits for the previous slow ask to finish so the
// conversation guard allows a second conversation.
func createConversation2(t *testing.T, svc *service.Service) *store.Conversation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conv, err := svc.CreateConversation(bctx)
		if err == nil {
			return conv
		}
		if !errors.Is(err, store.ErrConversationBusy) {
			t.Fatalf("create conversation: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the conversation guard to clear")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQueueFullLeavesNoRows(t *testing.T) {
	svc := newTestService(t, withCapacity(1))
	conv := createConversation(t, svc)

	// two racing submissions on the same conversation: exactly one may
	// reserve the single slot; the loser gets 429 and must not create
	// any database rows
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, _, err := svc.CreateTurn(bctx, store.TurnRequest{
				ConversationID: conv.ID,
				Prompt:         fmt.Sprintf("[FAKE:delay:100][FAKE:stdout:{\"response\":\"ok%d\"}]", n),
				IdempotencyKey: fmt.Sprintf("k%d", n),
			})
			errs[n] = err
		}(i)
	}
	close(start)
	wg.Wait()

	okCount := 0
	for _, err := range errs {
		if err == nil {
			okCount++
		} else if !errors.Is(err, queue.ErrFull) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if okCount != 1 {
		t.Fatalf("exactly one submission must succeed, got %d", okCount)
	}
	// verify the DB has exactly the one winning turn+task
	var turns int64
	if err := svc.St.DB().NewQuery(
		`SELECT COUNT(*) FROM {{turns}} WHERE [[conversation]] = {:c}`,
	).Bind(map[string]any{"c": conv.ID}).Row(&turns); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if turns != 1 {
		t.Fatalf("exactly one turn must exist, got %d", turns)
	}
}

func TestStartupRecoveryThroughService(t *testing.T) {
	t.Run("pending-canceled", func(t *testing.T) {
		svc := withNoStart(t, buildService(t))
		conv := createConversation(t, svc)
		_, task := createTurn(t, svc, conv.ID, `[FAKE:stdout:{"response":"x"}]`, "k1", "", "")
		if err := svc.Recover(); err != nil {
			t.Fatalf("recover: %v", err)
		}
		task, _ = svc.St.TaskByID(bctx, task.ID)
		if task.Status != store.TaskCanceled {
			t.Fatalf("pending must become canceled, got %s", task.Status)
		}
	})

	t.Run("running-unknown-no-redispatch", func(t *testing.T) {
		svc := withNoStart(t, buildService(t))
		conv := createConversation(t, svc)
		_, task := createTurn(t, svc, conv.ID, `[FAKE:stdout:{"response":"y"}]`, "k1", "", "")
		if ok, err := svc.St.TaskCAS(bctx, task.ID, store.TaskPending, store.TaskRunning); err != nil || !ok {
			t.Fatalf("CAS to running: ok=%v err=%v", ok, err)
		}
		if err := svc.Recover(); err != nil {
			t.Fatalf("recover: %v", err)
		}
		task, _ = svc.St.TaskByID(bctx, task.ID)
		if task.Status != store.TaskUnknownOutcome {
			t.Fatalf("running must become unknown_outcome, got %s", task.Status)
		}
		// start the worker: the recovered task must never be re-dispatched
		svc.Start()
		time.Sleep(150 * time.Millisecond) // give any (wrong) redispatch time to run
		task, _ = svc.St.TaskByID(bctx, task.ID)
		if task.Status != store.TaskUnknownOutcome {
			t.Fatalf("recovered task must never be re-dispatched, got %s", task.Status)
		}
		if _, err := svc.CreateConversation(bctx); !errors.Is(err, store.ErrConversationBusy) {
			t.Fatalf("want ErrConversationBusy after recovery quarantine, got %v", err)
		}
	})
}

func TestServiceRequiresProfile(t *testing.T) {
	if _, err := service.New(service.Config{DataDir: t.TempDir(), QueueCapacity: 1}); err == nil {
		t.Fatal("service.New must fail without a profile")
	}
}
