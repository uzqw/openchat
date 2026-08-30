// Package runner executes Gemini ask tasks through the locked opencli
// contract. Outcomes are normalized and persisted by the store; every
// non-successful ask that had already started is unknown_outcome, never a
// fake success.
package runner

import (
	"context"
	"strings"
	"time"

	"openchat/internal/opencli"
	"openchat/internal/queue"
	"openchat/internal/store"
)

// Config holds the runner knobs (filled from environment by later legs).
type Config struct {
	ExecPath       string // opencli executable; tests point at the fake
	Profile        string // OPENCLI_PROFILE, passed explicitly to every command
	AskTimeout     time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
}

// DefaultAskTimeout is the kill ceiling for a single ask. It doubles as
// the --timeout sent to opencli; zero would leave a hung child wedging the
// serialized worker forever, so a sane default is required.
const DefaultAskTimeout = 5 * time.Minute

// Runner executes Gemini asks serially (one queue worker).
type Runner struct {
	store *store.Store
	cfg   Config
}

// New builds a runner with defaults applied.
func New(s *store.Store, cfg Config) *Runner {
	if cfg.ExecPath == "" {
		cfg.ExecPath = "opencli"
	}
	if cfg.AskTimeout <= 0 {
		cfg.AskTimeout = DefaultAskTimeout
	}
	if cfg.MaxStdoutBytes <= 0 {
		cfg.MaxStdoutBytes = opencli.DefaultMaxStdoutBytes
	}
	if cfg.MaxStderrBytes <= 0 {
		cfg.MaxStderrBytes = opencli.DefaultMaxStderrBytes
	}
	return &Runner{store: s, cfg: cfg}
}

// AskOperation is the queue operation that runs one task: it CASes
// pending→running, executes the locked argv vector, maps the outcome and
// persists it (archiving the conversation and quarantining Gemini on any
// unknown outcome).
func (r *Runner) AskOperation(taskID string) queue.Operation {
	return queue.Operation{
		ID:  "ask:" + taskID,
		Ask: true,
		Run: r.runAsk(taskID),
	}
}

func (r *Runner) runAsk(taskID string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		ok, err := r.store.TaskCAS(ctx, taskID, store.TaskPending, store.TaskRunning)
		if err != nil {
			return err
		}
		if !ok {
			// canceled before execution (or recovered) — nothing to do
			return nil
		}

		task, turn, conv, err := r.store.LoadTaskContext(ctx, taskID)
		if err != nil {
			return err
		}

		first, err := r.store.IsFirstTurn(ctx, turn.ID)
		if err != nil {
			return err
		}

		exec := opencli.Execer{
			Path:           r.cfg.ExecPath,
			Timeout:        r.cfg.AskTimeout,
			MaxStdoutBytes: r.cfg.MaxStdoutBytes,
			MaxStderrBytes: r.cfg.MaxStderrBytes,
		}
		args := opencli.AskArgs(r.cfg.Profile, opencli.AskOpts{
			Prompt:   turn.Prompt,
			New:      first, // a new conversation's first turn opens a fresh site session
			Model:    task.RequestedModel,
			Thinking: task.Thinking,
			Timeout:  r.cfg.AskTimeout,
		})

		start := time.Now()
		res := exec.Run(ctx, args...)
		latency := time.Since(start).Milliseconds()

		outcome, reason := opencli.AskOutcomeOf(res)
		code, message := errorCodeOf(outcome, reason)
		switch outcome {
		case opencli.OutcomeSuccess:
			parsed, err := opencli.ParseAsk(res.Stdout)
			if err != nil {
				// outcome said success but the response is unusable:
				// never fake a success, treat as unknown
				return r.finishUnknown(ctx, taskID, conv.ID, store.ErrorCodeInvalidOutput,
					"Gemini response could not be parsed", latency)
			}
			return r.store.CompleteTask(ctx, taskID, store.TaskSucceeded, "", parsed.Response, "", latency)
		case opencli.OutcomeAuthRequired:
			hasSuccess, err := r.store.ConversationHasSuccessfulTask(ctx, conv.ID)
			if err != nil {
				return err
			}
			// a follow-up turn that loses auth archives the conversation;
			// a first-turn auth_required stays retryable after login
			if hasSuccess {
				if err := r.store.ArchiveConversation(ctx, conv.ID); err != nil {
					return err
				}
			}
			return r.store.CompleteTask(ctx, taskID, store.TaskAuthRequired, code, "", message, latency)
		case opencli.OutcomeFailed:
			return r.store.CompleteTask(ctx, taskID, store.TaskFailed, code, "", message, latency)
		default: // unknown_outcome
			return r.finishUnknown(ctx, taskID, conv.ID, code, message, latency)
		}
	}
}

// finishUnknown persists the unknown task and archives the conversation;
// quarantine is derived from the DB (any unacknowledged unknown).
func (r *Runner) finishUnknown(ctx context.Context, taskID, convID, code, message string, latency int64) error {
	if err := r.store.CompleteTask(ctx, taskID, store.TaskUnknownOutcome, code, "", message, latency); err != nil {
		return err
	}
	return r.store.ArchiveConversation(ctx, convID)
}

// errorCodeOf maps the locked opencli outcome reason to stable codes and
// user-safe messages (raw stderr is never stored or shown).
func errorCodeOf(outcome opencli.Outcome, reason string) (code, message string) {
	switch {
	case outcome == opencli.OutcomeAuthRequired:
		return store.ErrorCodeAuthRequired, "Gemini login required"
	case outcome == opencli.OutcomeFailed && reason == "spawn":
		return store.ErrorCodeSpawn, "Gemini process failed to start"
	case reason == "sentinel":
		return store.ErrorCodeNoResponse, "Gemini returned no response"
	case strings.HasPrefix(reason, "overflow"):
		return store.ErrorCodeOutputOverflow, "Gemini output exceeded the capture limit"
	case reason == "timeout":
		return store.ErrorCodeTimeout, "Gemini request timed out"
	case reason == "canceled":
		return store.ErrorCodeTerminated, "Gemini process was terminated"
	case reason == "bad_json":
		return store.ErrorCodeInvalidOutput, "Gemini returned an invalid response"
	default: // nonzero/unknown exit codes
		return store.ErrorCodeBadExit, "Gemini exited unexpectedly"
	}
}
