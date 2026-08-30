// Package store owns the PocketBase data layer for the Gemini v1 app:
// schema migrations, the conversation/turn/task model, atomic task state
// transitions (CAS), startup recovery and the quarantine derivation.
// Every test builds its own app on a temp data dir; the production
// pb_data directory is never touched by tests.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	_ "openchat/internal/store/migrations" // registers the app migrations
)

const (
	CollectionConversations = "conversations"
	CollectionTurns         = "turns"
	CollectionTasks         = "tasks"
)

const (
	ConvActive   = "active"
	ConvArchived = "archived"
)

// Task statuses (the locked state machine pending→running→terminal).
const (
	TaskPending        = "pending"
	TaskRunning        = "running"
	TaskSucceeded      = "succeeded"
	TaskFailed         = "failed"
	TaskAuthRequired   = "auth_required"
	TaskUnknownOutcome = "unknown_outcome"
	TaskCanceled       = "canceled"
)

const (
	ThinkingStandard = "standard"
	ThinkingExtended = "extended"
)

// Retryable task statuses (documented in the prompt: failed, auth_required
// and canceled can be manually retried; succeeded and unknown_outcome cannot).
var retryableStatuses = map[string]bool{
	TaskFailed:       true,
	TaskAuthRequired: true,
	TaskCanceled:     true,
}

// IsRetryable reports whether a status can be manually retried.
func IsRetryable(status string) bool { return retryableStatuses[status] }

// Stable error codes persisted on tasks (surfaced as the API error envelope
// in a later leg). Messages are user-safe and never contain stderr, cookies,
// profile content or filesystem paths.
const (
	ErrorCodeSpawn          = "spawn_failed"
	ErrorCodeAuthRequired   = "auth_required"
	ErrorCodeNoResponse     = "no_response"
	ErrorCodeTimeout        = "timeout"
	ErrorCodeTerminated     = "terminated"
	ErrorCodeInvalidOutput  = "invalid_response"
	ErrorCodeOutputOverflow = "output_overflow"
	ErrorCodeBadExit        = "gemini_error"
	ErrorCodeRecovery       = "restart_recovery"
	ErrorCodeResumeFailed   = "resume_failed"
)

// Typed errors; the API leg maps each to an HTTP status.
var (
	ErrConversationNotFound     = errors.New("conversation not found")
	ErrConversationArchived     = errors.New("conversation is archived and read-only")
	ErrConversationBusy         = errors.New("conversation busy or Gemini quarantined")
	ErrTurnUnfinished           = errors.New("previous turn still pending")
	ErrPrevTurnNotSucceeded     = errors.New("previous turn must succeed before asking again")
	ErrIdempotencyConflict      = errors.New("idempotency key reused with a different request body")
	ErrTaskNotFound             = errors.New("task not found")
	ErrTurnNotFound             = errors.New("turn not found")
	ErrTaskNotPending           = errors.New("task is not pending")
	ErrTaskNotRetryable         = errors.New("task cannot be retried")
	ErrTaskNotUnknown           = errors.New("task is not an unknown outcome")
	ErrConversationNotResumable = errors.New("conversation has no saved Gemini remote session")
)

// Conversation is the typed view over a conversations record.
type Conversation struct {
	ID            string
	Title         string
	Status        string
	RemoteID      string // Gemini web conversation id; empty = not resumable
	ResumePending bool   // the next turn must navigate to RemoteID first
	Created       time.Time
}

// Turn is the typed view over a turns record.
type Turn struct {
	ID             string
	ConversationID string
	Prompt         string
	IdempotencyKey string
	Created        time.Time
}

// Task is the typed view over a tasks record. Result is the single source
// of truth for an answer; nothing is mirrored anywhere else.
type Task struct {
	ID                    string
	TurnID                string
	RetryOf               string
	RequestedModel        string
	ResolvedModel         string
	Thinking              string
	Status                string
	Result                string
	ErrorCode             string
	ErrorMessage          string
	UnknownAcknowledgedAt *time.Time
	LatencyMS             int64
	Created               time.Time
}

// TurnRequest is one validated user submission that becomes a turn and its
// first task.
type TurnRequest struct {
	ConversationID string
	Prompt         string
	IdempotencyKey string
	Model          string
	Thinking       string
}

// Store wraps the bootstrapped PocketBase app.
type Store struct {
	app core.App
}

// New bootstraps a PocketBase app on dataDir and applies the app
// migrations. dataDir is always explicit; callers (tests, config) decide
// where it points, never the production pb_data by accident.
func New(dataDir string) (*Store, error) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := app.RunAppMigrations(); err != nil {
		return nil, fmt.Errorf("app migrations: %w", err)
	}
	s := &Store{app: app}
	// sanity: the schema we depend on must exist
	for _, name := range []string{CollectionConversations, CollectionTurns, CollectionTasks} {
		if _, err := app.FindCollectionByNameOrId(name); err != nil {
			return nil, fmt.Errorf("collection %s missing: %w", name, err)
		}
	}
	return s, nil
}

// App exposes the PocketBase app (API routing, later legs).
func (s *Store) App() core.App { return s.app }

// DB returns the app database builder for raw queries.
func (s *Store) DB() dbx.Builder { return s.app.DB() }

// Close releases the PocketBase resources (db connections, cron, cache).
func (s *Store) Close() error { return s.app.ResetBootstrapState() }

// isUniqueErr reports a sqlite unique-constraint violation, including
// PocketBase's record-level validation message for unique indexes.
func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_CONSTRAINT") ||
		strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "Value must be unique") // PocketBase record validation
}

// isNoRows reports a missing row.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// nowString returns the PocketBase datetime layout for the current time.
func nowString() string { return types.NowDateTime().String() }

// ---- record mapping -------------------------------------------------------

func conversationFromRecord(r *core.Record) *Conversation {
	return &Conversation{
		ID:            r.Id,
		Title:         r.GetString("title"),
		Status:        r.GetString("status"),
		RemoteID:      r.GetString("remote_id"),
		ResumePending: r.GetBool("resume_pending"),
		Created:       r.GetDateTime("created").Time(),
	}
}

func turnFromRecord(r *core.Record) *Turn {
	return &Turn{
		ID:             r.Id,
		ConversationID: r.GetString("conversation"),
		Prompt:         r.GetString("prompt"),
		IdempotencyKey: r.GetString("idempotency_key"),
		Created:        r.GetDateTime("created").Time(),
	}
}

func taskFromRecord(r *core.Record) *Task {
	t := &Task{
		ID:             r.Id,
		TurnID:         r.GetString("turn"),
		RetryOf:        r.GetString("retry_of"),
		RequestedModel: r.GetString("requested_model"),
		ResolvedModel:  r.GetString("resolved_model"),
		Thinking:       r.GetString("thinking"),
		Status:         r.GetString("status"),
		Result:         r.GetString("result"),
		ErrorCode:      r.GetString("error_code"),
		ErrorMessage:   r.GetString("error_message"),
		LatencyMS:      int64(r.GetInt("latency_ms")),
		Created:        r.GetDateTime("created").Time(),
	}
	if dt := r.GetDateTime("unknown_acknowledged_at"); !dt.IsZero() {
		v := dt.Time()
		t.UnknownAcknowledgedAt = &v
	}
	return t
}

func (s *Store) conversationByID(ctx context.Context, id string) (*Conversation, error) {
	rec, err := s.app.FindRecordById(CollectionConversations, id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return conversationFromRecord(rec), nil
}

func (s *Store) turnByID(ctx context.Context, id string) (*Turn, error) {
	rec, err := s.app.FindRecordById(CollectionTurns, id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrTurnNotFound
		}
		return nil, err
	}
	return turnFromRecord(rec), nil
}

func (s *Store) taskByID(ctx context.Context, id string) (*Task, error) {
	rec, err := s.app.FindRecordById(CollectionTasks, id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return taskFromRecord(rec), nil
}
