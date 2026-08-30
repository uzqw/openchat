package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"openchat/internal/store"
)

var ctx = context.Background()

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(t.TempDir()) // temp data dir only; production pb_data is never touched
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// createTurn drives the preflight+commit path (the queue layer is tested
// at the service level).
func createTurn(t *testing.T, s *store.Store, convID, prompt, key string) (*store.Turn, *store.Task) {
	t.Helper()
	req := store.TurnRequest{ConversationID: convID, Prompt: prompt, IdempotencyKey: key}
	if turn, task, err := s.PreflightCreateTurn(ctx, req); err != nil {
		t.Fatalf("PreflightCreateTurn: %v", err)
	} else if turn != nil {
		return turn, task
	}
	turn, task, err := s.CommitCreateTurn(ctx, req)
	if err != nil {
		t.Fatalf("CommitCreateTurn: %v", err)
	}
	return turn, task
}

func TestMigrationsCreateSchemaAndIndexes(t *testing.T) {
	s := newStore(t)

	// collections exist and are fail-closed (nil rules = deny all)
	for _, name := range []string{"conversations", "turns", "tasks"} {
		col, err := s.App().FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatalf("collection %s missing: %v", name, err)
		}
		for _, rule := range []*string{col.ListRule, col.ViewRule, col.CreateRule, col.UpdateRule, col.DeleteRule} {
			if rule != nil {
				t.Fatalf("collection %s must be fail-closed (nil rule), got %q", name, *rule)
			}
		}
	}

	// both unique indexes exist
	var sqlText string
	err := s.DB().NewQuery(
		`SELECT [[sql]] FROM sqlite_master WHERE [[type]] = 'index' AND [[name]] = {:n}`,
	).Bind(dbx.Params{"n": "idx_conversations_single_active"}).Row(&sqlText)
	if err != nil {
		t.Fatalf("single-active index missing: %v", err)
	}
	if !strings.Contains(sqlText, "UNIQUE") || !strings.Contains(sqlText, "WHERE") {
		t.Fatalf("single-active index must be a partial unique index: %s", sqlText)
	}
	err = s.DB().NewQuery(
		`SELECT [[sql]] FROM sqlite_master WHERE [[type]] = 'index' AND [[name]] = {:n}`,
	).Bind(dbx.Params{"n": "idx_turns_conversation_idemkey"}).Row(&sqlText)
	if err != nil {
		t.Fatalf("conversation+idempotency unique index missing: %v", err)
	}
	if !strings.Contains(sqlText, "UNIQUE") {
		t.Fatalf("idempotency index must be unique: %s", sqlText)
	}
}

func TestPartialUniqueIndexEnforcesSingleActive(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreateConversation(ctx); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	// bypass the app guard entirely: a raw insert of a second active row
	// must be rejected by the partial unique index itself
	_, err := s.DB().NewQuery(
		`INSERT INTO {{conversations}} ([[id]], [[status]], [[created]], [[updated]])
		 VALUES ({:id}, {:status}, {:now}, {:now})`,
	).Bind(dbx.Params{
		"id":     "aaaaaaaaaaaaaaa",
		"status": store.ConvActive,
		"now":    time.Now().UTC().Format("2006-01-02 15:04:05.000Z"),
	}).Execute()
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("second active conversation must be rejected by the index, got %v", err)
	}
}

func TestCompositeIdempotencyIndexBlocksDuplicate(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	req := store.TurnRequest{ConversationID: conv.ID, Prompt: "hello", IdempotencyKey: "k1"}
	if _, _, err := s.PreflightCreateTurn(ctx, req); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	t1, ta1, err := s.CommitCreateTurn(ctx, req)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}

	// a raw duplicate (conversation, idempotency_key) is rejected by the
	// unique index itself — the database-level backstop that prevents a
	// second Gemini write task under racing clients
	_, err = s.DB().NewQuery(
		`INSERT INTO {{turns}} ([[id]], [[conversation]], [[prompt]], [[idempotency_key]], [[created]], [[updated]])
		 VALUES ({:id}, {:conv}, {:prompt}, {:key}, {:now}, {:now})`,
	).Bind(dbx.Params{
		"id": "bbbbbbbbbbbbbbb", "conv": conv.ID, "prompt": "hello",
		"key": "k1", "now": time.Now().UTC().Format("2006-01-02 15:04:05.000Z"),
	}).Execute()
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("duplicate (conversation, idempotency_key) must be rejected, got %v", err)
	}

	// once the original turn's task succeeds, a replay of the same key is
	// allowed and resolved by the store back to the original turn/task,
	// never creating a second Gemini write task
	if ok, err := s.TaskCAS(ctx, ta1.ID, store.TaskPending, store.TaskSucceeded); err != nil || !ok {
		t.Fatalf("CAS to succeeded: ok=%v err=%v", ok, err)
	}
	t2, ta2, err := s.CommitCreateTurn(ctx, req)
	if err != nil {
		t.Fatalf("racing commit must resolve, got %v", err)
	}
	if t2.ID != t1.ID || ta2.ID != ta1.ID {
		t.Fatalf("replay must return the original turn/task: %s/%s vs %s/%s", t2.ID, ta2.ID, t1.ID, ta1.ID)
	}
	rows, err := s.TasksOfTurn(ctx, t1.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("exactly one task must exist for the turn, got %d (%v)", len(rows), err)
	}
}

func TestCreateConversationGuards(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	createTurn(t, s, conv.ID, "q1", "k1") // leaves a pending task

	// pending task blocks a new conversation
	if _, err := s.CreateConversation(ctx); !errors.Is(err, store.ErrConversationBusy) {
		t.Fatalf("want ErrConversationBusy while task pending, got %v", err)
	}
}

func TestSetConversationRemoteID(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if err := s.SetConversationRemoteID(ctx, conv.ID, "b8368a89d4242e5f"); err != nil {
		t.Fatalf("set remote id: %v", err)
	}
	got, err := s.ConversationByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.RemoteID != "b8368a89d4242e5f" {
		t.Fatalf("remote_id = %q", got.RemoteID)
	}
	// empty is a no-op (never wipes a captured id)
	if err := s.SetConversationRemoteID(ctx, conv.ID, ""); err != nil {
		t.Fatalf("set empty remote id: %v", err)
	}
	got, _ = s.ConversationByID(ctx, conv.ID)
	if got.RemoteID != "b8368a89d4242e5f" {
		t.Fatalf("empty set must not wipe remote_id, got %q", got.RemoteID)
	}
}

func TestResumeConversationSwitchesActive(t *testing.T) {
	s := newStore(t)
	a, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := s.SetConversationRemoteID(ctx, a.ID, "aaaa1111aaaa1111"); err != nil {
		t.Fatalf("set remote id: %v", err)
	}
	b, err := s.CreateConversation(ctx) // archives A
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	resumed, err := s.ResumeConversation(ctx, a.ID)
	if err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if resumed.Status != store.ConvActive {
		t.Fatalf("resumed conversation must be active, got %s", resumed.Status)
	}
	a2, _ := s.ConversationByID(ctx, a.ID)
	b2, _ := s.ConversationByID(ctx, b.ID)
	if a2.Status != store.ConvActive || b2.Status != store.ConvArchived {
		t.Fatalf("after resume: A=%s B=%s, want active/archived", a2.Status, b2.Status)
	}
	// exactly one active row survives (the partial unique index)
	var n int64
	if err := s.DB().NewQuery(
		`SELECT COUNT(*) FROM {{conversations}} WHERE [[status]] = {:active}`,
	).Bind(dbx.Params{"active": store.ConvActive}).Row(&n); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if n != 1 {
		t.Fatalf("exactly one active conversation must exist, got %d", n)
	}
}

func TestResumeConversationRefusesWithoutRemoteID(t *testing.T) {
	s := newStore(t)
	a, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := s.CreateConversation(ctx); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if _, err := s.ResumeConversation(ctx, a.ID); !errors.Is(err, store.ErrConversationNotResumable) {
		t.Fatalf("resume without remote id: want ErrConversationNotResumable, got %v", err)
	}
}

func TestResumeConversationRefusesWhileBusy(t *testing.T) {
	s := newStore(t)
	a, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := s.SetConversationRemoteID(ctx, a.ID, "aaaa1111aaaa1111"); err != nil {
		t.Fatalf("set remote id: %v", err)
	}
	b, err := s.CreateConversation(ctx) // archives A, B is active
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	createTurn(t, s, b.ID, "q1", "k1") // pending task on the active conversation
	if _, err := s.ResumeConversation(ctx, a.ID); !errors.Is(err, store.ErrConversationBusy) {
		t.Fatalf("resume while busy: want ErrConversationBusy, got %v", err)
	}
}

func TestResumeConversationNoopWhenActive(t *testing.T) {
	s := newStore(t)
	a, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := s.SetConversationRemoteID(ctx, a.ID, "aaaa1111aaaa1111"); err != nil {
		t.Fatalf("set remote id: %v", err)
	}
	resumed, err := s.ResumeConversation(ctx, a.ID)
	if err != nil {
		t.Fatalf("resume active: %v", err)
	}
	if resumed.ID != a.ID || resumed.Status != store.ConvActive {
		t.Fatalf("resuming the active conversation must be a no-op, got %+v", resumed)
	}
}

func TestResumeConversationNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.ResumeConversation(ctx, "nope"); !errors.Is(err, store.ErrConversationNotFound) {
		t.Fatalf("resume missing: want ErrConversationNotFound, got %v", err)
	}
}

func TestCreateTurnGuards(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, task := createTurn(t, s, conv.ID, "q1", "k1")

	// unfinished turn blocks the next turn
	req := store.TurnRequest{ConversationID: conv.ID, Prompt: "q2", IdempotencyKey: "k2"}
	if _, _, err := s.PreflightCreateTurn(ctx, req); !errors.Is(err, store.ErrTurnUnfinished) {
		t.Fatalf("want ErrTurnUnfinished, got %v", err)
	}

	// a terminal-but-failed current task also blocks: retry is the only path
	if ok, err := s.TaskCAS(ctx, task.ID, store.TaskPending, store.TaskFailed); err != nil || !ok {
		t.Fatalf("CAS to failed: ok=%v err=%v", ok, err)
	}
	if _, _, err := s.PreflightCreateTurn(ctx, req); !errors.Is(err, store.ErrPrevTurnNotSucceeded) {
		t.Fatalf("want ErrPrevTurnNotSucceeded, got %v", err)
	}

	// success unblocks the follow-up turn
	if ok, err := s.TaskCAS(ctx, task.ID, store.TaskFailed, store.TaskSucceeded); err != nil || !ok {
		t.Fatalf("CAS to succeeded: ok=%v err=%v", ok, err)
	}
	if _, _, err := s.PreflightCreateTurn(ctx, req); err != nil {
		t.Fatalf("follow-up preflight: %v", err)
	}

	// archived conversation rejects new turns
	if err := s.ArchiveConversation(ctx, conv.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, _, err := s.PreflightCreateTurn(ctx, req); !errors.Is(err, store.ErrConversationArchived) {
		t.Fatalf("want ErrConversationArchived, got %v", err)
	}
}

func TestRecoverPendingAndActive(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, task := createTurn(t, s, conv.ID, "q1", "k1") // pending

	if err := s.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	task, err = s.TaskByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("task lookup: %v", err)
	}
	if task.Status != store.TaskCanceled {
		t.Fatalf("pending after recovery must be canceled, got %s", task.Status)
	}
	conv, err = s.ConversationByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("conversation lookup: %v", err)
	}
	if conv.Status != store.ConvArchived {
		t.Fatalf("active after recovery must be archived, got %s", conv.Status)
	}
	// canceled is not an unknown, so nothing is quarantined
	if q, err := s.IsQuarantined(ctx); err != nil || q {
		t.Fatalf("want quarantine=false, got %v (%v)", q, err)
	}
}

func TestRecoverRunningUnknownAndQuarantine(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, task := createTurn(t, s, conv.ID, "q1", "k1")
	if ok, err := s.TaskCAS(ctx, task.ID, store.TaskPending, store.TaskRunning); err != nil || !ok {
		t.Fatalf("CAS to running: ok=%v err=%v", ok, err)
	}

	if err := s.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	task, err = s.TaskByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("task lookup: %v", err)
	}
	if task.Status != store.TaskUnknownOutcome || task.ErrorCode != store.ErrorCodeRecovery {
		t.Fatalf("running after recovery must be unknown_outcome (%s/%s)", task.Status, task.ErrorCode)
	}
	// the unacknowledged unknown restores the quarantine
	if q, err := s.IsQuarantined(ctx); err != nil || !q {
		t.Fatalf("want quarantine=true after recovery, got %v (%v)", q, err)
	}
	// ... and new conversations stay blocked until acknowledged
	if _, err := s.CreateConversation(ctx); !errors.Is(err, store.ErrConversationBusy) {
		t.Fatalf("want ErrConversationBusy while quarantined, got %v", err)
	}

	if ok, err := s.AcknowledgeUnknownTask(ctx, task.ID); err != nil || !ok {
		t.Fatalf("acknowledge: ok=%v err=%v", ok, err)
	}
	if q, err := s.IsQuarantined(ctx); err != nil || q {
		t.Fatalf("want quarantine=false after acknowledge, got %v (%v)", q, err)
	}
	if _, err := s.CreateConversation(ctx); err != nil {
		t.Fatalf("create conversation after acknowledge: %v", err)
	}
}

func TestRetryCopiesParameters(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	req := store.TurnRequest{ConversationID: conv.ID, Prompt: "q1", IdempotencyKey: "k1", Model: "2.5-flash", Thinking: store.ThinkingExtended}
	if _, _, err := s.PreflightCreateTurn(ctx, req); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_, task, err := s.CommitCreateTurn(ctx, req)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if ok, _ := s.TaskCAS(ctx, task.ID, store.TaskPending, store.TaskAuthRequired); !ok {
		t.Fatalf("CAS to auth_required")
	}

	retry, err := s.CommitRetryTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("commit retry: %v", err)
	}
	if retry.RetryOf != task.ID {
		t.Fatalf("retry_of = %q, want %q", retry.RetryOf, task.ID)
	}
	if retry.RequestedModel != "2.5-flash" || retry.Thinking != store.ThinkingExtended {
		t.Fatalf("retry must copy model/thinking: %q/%q", retry.RequestedModel, retry.Thinking)
	}
	if retry.Status != store.TaskPending {
		t.Fatalf("retry status = %s, want pending", retry.Status)
	}
	// succeeded and unknown_outcome cannot be retried (each on its own retry)
	retryS, err := s.CommitRetryTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("commit retry 2: %v", err)
	}
	if ok, _ := s.TaskCAS(ctx, retryS.ID, store.TaskPending, store.TaskSucceeded); !ok {
		t.Fatalf("CAS retry to succeeded")
	}
	if _, err := s.CommitRetryTask(ctx, retryS.ID); !errors.Is(err, store.ErrTaskNotRetryable) {
		t.Fatalf("retry of succeeded must fail with ErrTaskNotRetryable, got %v", err)
	}
	if ok, _ := s.TaskCAS(ctx, retry.ID, store.TaskPending, store.TaskUnknownOutcome); !ok {
		t.Fatalf("CAS retry to unknown_outcome")
	}
	if _, err := s.CommitRetryTask(ctx, retry.ID); !errors.Is(err, store.ErrTaskNotRetryable) {
		t.Fatalf("retry of unknown_outcome must fail with ErrTaskNotRetryable, got %v", err)
	}
}

func TestIsFirstTurn(t *testing.T) {
	s := newStore(t)
	conv, err := s.CreateConversation(ctx)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	turn1, _ := createTurn(t, s, conv.ID, "q1", "k1")
	first, err := s.IsFirstTurn(ctx, turn1.ID)
	if err != nil || !first {
		t.Fatalf("turn1 must be first: %v (%v)", first, err)
	}
	tasks, err := s.TasksOfTurn(ctx, turn1.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks of turn1: %v", err)
	}
	if ok, err := s.TaskCAS(ctx, tasks[0].ID, store.TaskPending, store.TaskSucceeded); err != nil || !ok {
		t.Fatalf("CAS turn1 to succeeded: %v", err)
	}
	turn2, _ := createTurn(t, s, conv.ID, "q2", "k2")
	first, err = s.IsFirstTurn(ctx, turn2.ID)
	if err != nil || first {
		t.Fatalf("turn2 must not be first: %v (%v)", first, err)
	}
}
