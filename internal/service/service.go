// Package service composes the store, queue and runner into the operations
// the v1 REST API will expose: conversation/turn creation, retry, cancel,
// acknowledge, quarantine, and startup recovery. It is the seam for later
// legs — the HTTP layer maps the typed errors to status codes.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"openchat/internal/opencli"
	"openchat/internal/queue"
	"openchat/internal/runner"
	"openchat/internal/store"
)

// Input bounds (request validation happens before any task creation).
const (
	MaxPromptBytes = 100 << 10 // 100 KiB, mirrors the turns.prompt field
	MaxModelLen    = 255
)

// ErrValidation wraps request-validation failures so the API layer can map
// them to 400 without string matching on the message.
var ErrValidation = errors.New("invalid request")

// Config wires the whole backend. Later legs read it from environment
// variables and fail closed when required pieces are missing.
type Config struct {
	DataDir        string
	ExecPath       string
	Profile        string // OPENCLI_PROFILE — required
	QueueCapacity  int
	AskTimeout     time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
}

// Service is the composed backend.
type Service struct {
	St     *store.Store
	Queue  *queue.Queue
	Runner *runner.Runner

	writeGuard func() error
	cancel     context.CancelFunc
	done       chan struct{}
}

// New builds the service. It refuses to start without a profile (the
// contract requires an explicit OPENCLI_PROFILE on every command).
func New(cfg Config) (*Service, error) {
	if cfg.Profile == "" {
		return nil, errors.New("service: OPENCLI_PROFILE is required")
	}
	st, err := store.New(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	capacity := cfg.QueueCapacity
	if capacity <= 0 {
		capacity = 1
	}
	rn := runner.New(st, runner.Config{
		ExecPath:       cfg.ExecPath,
		Profile:        cfg.Profile,
		AskTimeout:     cfg.AskTimeout,
		MaxStdoutBytes: cfg.MaxStdoutBytes,
		MaxStderrBytes: cfg.MaxStderrBytes,
	})
	return &Service{
		St:     st,
		Queue:  queue.New(capacity),
		Runner: rn,
	}, nil
}

// Start launches the queue worker.
func (s *Service) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		s.Queue.Run(ctx, func(op queue.Operation, err error) {
			log.Printf("queue operation %s failed: %v", op.ID, err)
		})
	}()
}

// Close stops the worker and releases database resources.
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.Queue.Close() // broadcast wakes the worker out of cond.Wait
	if s.done != nil {
		<-s.done
	}
	_ = s.St.Close()
}

// Recover must run once at startup, before the worker starts: pending
// tasks become canceled, running tasks become unknown (quarantining
// Gemini), and every active conversation is archived.
func (s *Service) Recover() error {
	return s.St.Recover(context.Background())
}

// IsQuarantined reports the derived quarantine state.
func (s *Service) IsQuarantined(ctx context.Context) (bool, error) {
	return s.St.IsQuarantined(ctx)
}

// CreateConversation archives the previous active conversation and creates
// a new one; refused while a task is pending/running or Gemini is
// quarantined.
func (s *Service) CreateConversation(ctx context.Context) (*store.Conversation, error) {
	return s.St.CreateConversation(ctx)
}

// SetWriteGuard installs the fail-closed write guard (prompts §7: a local
// adapter override, an installed plugin or a version mismatch must never
// run a real write). It is consulted before any Gemini write task is
// created or retried — after the idempotency replay check, before queue
// capacity. Install it once at startup, before the worker handles asks.
func (s *Service) SetWriteGuard(fn func() error) { s.writeGuard = fn }

// CreateTurn validates the request, resolves idempotency replays before
// any state or capacity check, reserves a queue slot, creates turn + first
// task in one transaction and enqueues the ask. A replay returns the
// original turn and task without touching the queue.
func (s *Service) CreateTurn(ctx context.Context, req store.TurnRequest) (*store.Turn, *store.Task, error) {
	if err := ValidateTurnRequest(req); err != nil {
		return nil, nil, err
	}
	replayTurn, replayTask, err := s.St.PreflightCreateTurn(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if replayTurn != nil {
		return replayTurn, replayTask, nil
	}
	if s.writeGuard != nil {
		if err := s.writeGuard(); err != nil {
			return nil, nil, err
		}
	}
	if err := s.Queue.ReserveAsk(); err != nil {
		return nil, nil, err // 429, and no database rows exist
	}
	turn, task, err := s.St.CommitCreateTurn(ctx, req)
	if err != nil {
		s.Queue.ReleaseAsk()
		return nil, nil, err
	}
	s.Queue.Enqueue(s.Runner.AskOperation(task.ID))
	return turn, task, nil
}

// RetryTask creates a new pending task retrying an original failed /
// auth_required / canceled task; the conversation must still be active.
func (s *Service) RetryTask(ctx context.Context, taskID string) (*store.Task, error) {
	orig, err := s.St.TaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !store.IsRetryable(orig.Status) {
		return nil, store.ErrTaskNotRetryable
	}
	// conversation-active is re-checked inside the transaction
	if s.writeGuard != nil {
		if err := s.writeGuard(); err != nil {
			return nil, err
		}
	}
	if err := s.Queue.ReserveAsk(); err != nil {
		return nil, err
	}
	task, err := s.St.CommitRetryTask(ctx, taskID)
	if err != nil {
		s.Queue.ReleaseAsk()
		return nil, err
	}
	s.Queue.Enqueue(s.Runner.AskOperation(task.ID))
	return task, nil
}

// CancelTask cancels a pending task (queued, not yet executed). Running and
// terminal tasks return ErrTaskNotPending (409): the process is never
// killed and relabeled as canceled.
func (s *Service) CancelTask(ctx context.Context, taskID string) error {
	ok, err := s.St.TaskCAS(ctx, taskID, store.TaskPending, store.TaskCanceled)
	if err != nil {
		return err
	}
	if !ok {
		if _, err := s.St.TaskByID(ctx, taskID); err != nil {
			return err
		}
		return store.ErrTaskNotPending
	}
	s.Queue.RemovePending("ask:" + taskID) // no-op if the worker already popped it
	return nil
}

// AcknowledgeUnknown stamps the acknowledgment; when no unacknowledged
// unknown remains, quarantine clears and quarantineLifted is true.
func (s *Service) AcknowledgeUnknown(ctx context.Context, taskID string) (quarantineLifted bool, err error) {
	ok, err := s.St.AcknowledgeUnknownTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	if !ok {
		t, err := s.St.TaskByID(ctx, taskID)
		if err != nil {
			return false, err
		}
		if t.Status != store.TaskUnknownOutcome {
			return false, store.ErrTaskNotUnknown
		}
		// already acknowledged — idempotent success
	}
	q, err := s.St.IsQuarantined(ctx)
	if err != nil {
		return false, err
	}
	return !q, nil
}

// ValidateTurnRequest enforces the input bounds before any task creation.
func ValidateTurnRequest(req store.TurnRequest) error {
	if req.Prompt == "" {
		return fmt.Errorf("%w: prompt is required", ErrValidation)
	}
	if req.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency key is required", ErrValidation)
	}
	if len(req.Prompt) > MaxPromptBytes || !utf8.ValidString(req.Prompt) {
		return fmt.Errorf("%w: prompt exceeds the size limit or is not valid UTF-8", ErrValidation)
	}
	if len(req.Model) > MaxModelLen {
		return fmt.Errorf("%w: model exceeds the length limit", ErrValidation)
	}
	if err := (opencli.AskOpts{Thinking: req.Thinking}).Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}
