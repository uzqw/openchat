// Package runner executes Gemini ask tasks through the locked opencli
// contract. Outcomes are normalized and persisted by the store; every
// non-successful ask that had already started is unknown_outcome, never a
// fake success.
package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"openchat/internal/opencli"
	"openchat/internal/queue"
	"openchat/internal/store"
)

// Config holds the runner knobs (filled from environment by later legs).
type Config struct {
	ExecPath       string        // opencli executable; tests point at the fake
	Profile        string        // OPENCLI_PROFILE, passed explicitly to every command
	Site           *opencli.Site // OPENCLI_SITE adapter (default gemini)
	ExtraEnv       []string      // appended after the child env allowlist (fake scenarios in tests)
	AskTimeout     time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
}

// DefaultAskTimeout is the kill ceiling for a single ask. It doubles as
// the --timeout sent to opencli; zero would leave a hung child wedging the
// serialized worker forever, so a sane default is required.
const DefaultAskTimeout = 60 * time.Second

// auxTimeout is the kill ceiling for the resume/capture helper calls
// (site detail / status). They navigate or read the shared tab and must
// never wedge the queue as long as a real ask legitimately can.
const auxTimeout = 30 * time.Second

// Runner executes site asks serially (one queue worker).
type Runner struct {
	store   *store.Store
	cfg     Config
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// New builds a runner with defaults applied.
func New(s *store.Store, cfg Config) *Runner {
	if cfg.ExecPath == "" {
		cfg.ExecPath = "opencli"
	}
	if cfg.Site == nil {
		cfg.Site = opencli.SiteGemini
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
	return &Runner{store: s, cfg: cfg, cancels: make(map[string]context.CancelFunc)}
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

// CancelRunning attempts to kill a running ask's subprocess via context cancel.
func (r *Runner) CancelRunning(taskID string) {
	r.mu.Lock()
	if c, ok := r.cancels[taskID]; ok {
		c()
	}
	r.mu.Unlock()
}

// errResumeAborted marks a resume that already terminalized the task as
// failed; the ask must not run (the tab is not on the target conversation).
var errResumeAborted = errors.New("resume aborted, task terminalized")

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
		// per-task cancellable context so CancelTask can kill the subprocess
		taskCtx, cancel := context.WithCancel(ctx)
		r.mu.Lock()
		r.cancels[taskID] = cancel
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			delete(r.cancels, taskID)
			r.mu.Unlock()
			cancel()
		}()
		ctx = taskCtx

		task, turn, conv, err := r.store.LoadTaskContext(ctx, taskID)
		if err != nil {
			return err
		}

		first, err := r.store.IsFirstTurn(ctx, turn.ID)
		if err != nil {
			return err
		}
		// ResumeConversation marks the next turn explicitly. Checking the
		// conversation's first-ever turn is wrong here: an old conversation
		// already has turns when it is resumed.
		resume := conv.ResumePending && conv.RemoteID != ""
		// Each conversation runs on the site it was created on; a resumed
		// legacy conversation falls back to the configured default site.
		site := r.siteOf(conv)

		exec := opencli.Execer{
			Path:           r.cfg.ExecPath,
			ExtraEnv:       r.cfg.ExtraEnv,
			Timeout:        r.cfg.AskTimeout,
			MaxStdoutBytes: r.cfg.MaxStdoutBytes,
			MaxStderrBytes: r.cfg.MaxStderrBytes,
		}

		if resume {
			if err := r.resumeConversation(ctx, exec, site, conv.RemoteID, taskID); err != nil {
				if errors.Is(err, errResumeAborted) {
					return nil // task already terminalized as failed
				}
				return err
			}
		}

		args := site.AskArgs(r.cfg.Profile, opencli.AskOpts{
			Prompt:   turn.Prompt,
			New:      first && !resume, // a new conversation's first turn opens a fresh site session
			Model:    task.RequestedModel,
			Thinking: task.Thinking,
			Timeout:  r.cfg.AskTimeout,
		})

		start := time.Now()
		res := exec.Run(ctx, args...)
		latency := time.Since(start).Milliseconds()

		// If the task was externally canceled (running→canceled via CancelTask),
		// do not overwrite the canceled status or archive the conversation.
		if cur, err := r.store.TaskByID(context.Background(), taskID); err == nil && cur.Status == store.TaskCanceled {
			return nil
		}

		outcome, reason := site.AskOutcomeOf(res)
		code, message := errorCodeOf(site, outcome, reason)
		switch outcome {
		case opencli.OutcomeSuccess:
			parsed, err := opencli.ParseAsk(res.Stdout)
			if err != nil {
				// outcome said success but the response is unusable:
				// never fake a success, treat as unknown
				return r.finishUnknown(ctx, taskID, conv.ID, store.ErrorCodeInvalidOutput,
					site.Label+" response could not be parsed", latency)
			}
			// Capture before publishing task success so clients never observe a
			// successful first turn with a transiently missing remote id. This
			// remains best-effort: capture failures leave the conversation read-only.
			if first && !resume {
				_ = r.captureRemoteID(ctx, exec, site, conv.ID)
			}
			if err := r.store.CompleteTask(ctx, taskID, store.TaskSucceeded, "", parsed.Response, "", latency); err != nil {
				return err
			}
			if resume {
				// Only the successful resumed ask has established the shared tab
				// on this conversation for subsequent ordinary asks.
				return r.store.ClearConversationResumePending(ctx, conv.ID)
			}
			return nil
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

// resumeConversation navigates the persistent site session tab to the
// saved conversation and verifies the tab actually landed there before
// any prompt is sent. A failed navigation or a URL mismatch is a
// pre-dispatch failure (nothing was submitted): the task is marked failed
// and errResumeAborted is returned so the ask never runs. A retry re-runs
// the whole resume sequence, so it can never ask in a context we did not
// deliberately navigate to.
// siteOf resolves the site adapter a conversation runs on from its stored
// provider, falling back to the configured default site for pre-migration
// conversations that never got a provider stamped.
func (r *Runner) siteOf(conv *store.Conversation) *opencli.Site {
	if s, err := opencli.ByName(conv.Provider); err == nil {
		return s
	}
	return r.cfg.Site
}

func (r *Runner) resumeConversation(ctx context.Context, exec opencli.Execer, site *opencli.Site, remoteID, taskID string) error {
	aux := exec
	aux.Timeout = auxTimeout
	res := aux.Run(ctx, site.DetailArgs(r.cfg.Profile, remoteID)...)
	if !res.Started {
		if err := r.store.CompleteTask(ctx, taskID, store.TaskFailed, store.ErrorCodeSpawn,
			"", site.Label+" process failed to start", 0); err != nil {
			return err
		}
		return errResumeAborted
	}
	if res.ExitCode != 0 {
		if err := r.store.CompleteTask(ctx, taskID, store.TaskFailed, store.ErrorCodeResumeFailed,
			"", "无法恢复 "+site.Label+" 会话（远端会话可能已删除）", 0); err != nil {
			return err
		}
		return errResumeAborted
	}
	st := aux.Run(ctx, site.StatusArgs(r.cfg.Profile)...)
	if st.ExitCode != 0 || !site.StatusURLHasConversationID(st.Stdout, remoteID) {
		if err := r.store.CompleteTask(ctx, taskID, store.TaskFailed, store.ErrorCodeResumeFailed,
			"", "无法确认 "+site.Label+" 会话已恢复，已中止提问", 0); err != nil {
			return err
		}
		return errResumeAborted
	}
	return nil
}

// captureRemoteID reads the current conversation URL after a successful
// first ask and persists it on the conversation. Best-effort: any failure
// leaves the conversation without a remote id (read-only).
func (r *Runner) captureRemoteID(ctx context.Context, exec opencli.Execer, site *opencli.Site, convID string) error {
	aux := exec
	aux.Timeout = auxTimeout
	st := aux.Run(ctx, site.StatusArgs(r.cfg.Profile)...)
	if st.ExitCode != 0 {
		return nil
	}
	return r.store.SetConversationRemoteID(ctx, convID,
		site.ConversationID(opencli.ParseStatusURL(st.Stdout)))
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
func errorCodeOf(site *opencli.Site, outcome opencli.Outcome, reason string) (code, message string) {
	switch {
	case outcome == opencli.OutcomeAuthRequired:
		return store.ErrorCodeAuthRequired, site.Label + " login required"
	case outcome == opencli.OutcomeFailed && reason == "spawn":
		return store.ErrorCodeSpawn, site.Label + " process failed to start"
	case reason == "sentinel":
		return store.ErrorCodeNoResponse, site.Label + " returned no response"
	case strings.HasPrefix(reason, "overflow"):
		return store.ErrorCodeOutputOverflow, site.Label + " output exceeded the capture limit"
	case reason == "timeout":
		return store.ErrorCodeTimeout, site.Label + " request timed out"
	case reason == "canceled":
		return store.ErrorCodeTerminated, site.Label + " process was terminated"
	case reason == "bad_json":
		return store.ErrorCodeInvalidOutput, site.Label + " returned an invalid response"
	default: // nonzero/unknown exit codes
		return store.ErrorCodeBadExit, site.Label + " exited unexpectedly"
	}
}
