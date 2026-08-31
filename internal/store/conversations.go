package store

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// HasNonTerminalTasks reports whether any task is pending or running.
func (s *Store) HasNonTerminalTasks(ctx context.Context) (bool, error) {
	return hasNonTerminalTasks(s.app, ctx)
}

func hasNonTerminalTasks(app core.App, ctx context.Context) (bool, error) {
	return exists(app, ctx,
		`SELECT EXISTS(SELECT 1 FROM {{tasks}} WHERE [[status]] IN ({:pending}, {:running}))`,
		dbx.Params{"pending": TaskPending, "running": TaskRunning})
}

// IsQuarantined reports whether any task is an unacknowledged unknown
// outcome. Quarantine is derived from the database, never stored
// separately, so it survives restarts.
func (s *Store) IsQuarantined(ctx context.Context) (bool, error) {
	return isQuarantined(s.app, ctx)
}

func isQuarantined(app core.App, ctx context.Context) (bool, error) {
	// DateField columns store '' (never NULL), so "unacknowledged" is an
	// empty unknown_acknowledged_at.
	return exists(app, ctx,
		`SELECT EXISTS(SELECT 1 FROM {{tasks}} WHERE [[status]] = {:st} AND [[unknown_acknowledged_at]] = '')`,
		dbx.Params{"st": TaskUnknownOutcome})
}

// CreateConversation archives the old active conversation (if any) and
// creates a new active one on the given site provider, all in one
// transaction. Refused while any task is pending/running or a site is
// quarantined. The partial unique index on status='active' is the final
// arbiter under concurrency.
func (s *Store) CreateConversation(ctx context.Context, provider string) (*Conversation, error) {
	if provider == "" {
		return nil, ErrConversationNoProvider
	}
	if busy, err := s.HasNonTerminalTasks(ctx); err != nil {
		return nil, err
	} else if busy {
		return nil, ErrConversationBusy
	}
	if q, err := s.IsQuarantined(ctx); err != nil {
		return nil, err
	} else if q {
		return nil, ErrConversationBusy
	}

	var created *Conversation
	err := s.app.RunInTransaction(func(txApp core.App) error {
		// re-check inside the transaction: concurrent writers are serialized
		// here only enough that the unique index can finish the job.
		if busy, err := hasNonTerminalTasks(txApp, ctx); err != nil {
			return err
		} else if busy {
			return ErrConversationBusy
		}
		if q, err := isQuarantined(txApp, ctx); err != nil {
			return err
		} else if q {
			return ErrConversationBusy
		}
		if _, err := txApp.DB().NewQuery(
			`UPDATE {{conversations}} SET [[status]] = {:archived} WHERE [[status]] = {:active}`,
		).Bind(dbx.Params{"archived": ConvArchived, "active": ConvActive}).Execute(); err != nil {
			return err
		}
		col, err := txApp.FindCollectionByNameOrId(CollectionConversations)
		if err != nil {
			return err
		}
		rec := core.NewRecord(col)
		rec.Set("title", "")
		rec.Set("status", ConvActive)
		rec.Set("provider", provider)
		rec.Set("resume_pending", false)
		if err := txApp.Save(rec); err != nil {
			return err
		}
		created = conversationFromRecord(rec)
		return nil
	})
	if err != nil {
		if isUniqueErr(err) {
			return nil, ErrConversationBusy
		}
		return nil, err
	}
	return created, nil
}

// ConversationByID returns a typed conversation.
func (s *Store) ConversationByID(ctx context.Context, id string) (*Conversation, error) {
	return s.conversationByID(ctx, id)
}

// SetConversationRemoteID persists the Gemini web conversation id captured
// after the first successful ask (best-effort; empty never overwrites).
func (s *Store) SetConversationRemoteID(ctx context.Context, id, remoteID string) error {
	if remoteID == "" {
		return nil
	}
	_, err := s.app.DB().NewQuery(
		`UPDATE {{conversations}} SET [[remote_id]] = {:rid} WHERE [[id]] = {:id}`,
	).Bind(dbx.Params{"rid": remoteID, "id": id}).Execute()
	return err
}

// ResumeConversation archives the current active conversation (if any),
// reactivates the target conversation and marks its next turn for remote
// navigation, all in one transaction. Refused while
// any task is pending/running or Gemini is quarantined, and when the target
// has no saved Gemini remote conversation id (it cannot be resumed safely —
// asking without a remote session would land in the wrong web context). The
// partial unique index on status='active' is the final arbiter under
// concurrency. Resuming the already-active conversation is a no-op.
func (s *Store) ResumeConversation(ctx context.Context, id string) (*Conversation, error) {
	target, err := s.conversationByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if target.Status == ConvActive {
		return target, nil
	}
	if target.RemoteID == "" {
		return nil, ErrConversationNotResumable
	}
	if busy, err := s.HasNonTerminalTasks(ctx); err != nil {
		return nil, err
	} else if busy {
		return nil, ErrConversationBusy
	}
	if q, err := s.IsQuarantined(ctx); err != nil {
		return nil, err
	} else if q {
		return nil, ErrConversationBusy
	}

	var resumed *Conversation
	err = s.app.RunInTransaction(func(txApp core.App) error {
		// re-check inside the transaction: concurrent writers are serialized
		// here only enough that the unique index can finish the job.
		if busy, err := hasNonTerminalTasks(txApp, ctx); err != nil {
			return err
		} else if busy {
			return ErrConversationBusy
		}
		if q, err := isQuarantined(txApp, ctx); err != nil {
			return err
		} else if q {
			return ErrConversationBusy
		}
		if _, err := txApp.DB().NewQuery(
			`UPDATE {{conversations}} SET [[status]] = {:archived} WHERE [[status]] = {:active}`,
		).Bind(dbx.Params{"archived": ConvArchived, "active": ConvActive}).Execute(); err != nil {
			return err
		}
		rec, err := txApp.FindRecordById(CollectionConversations, id)
		if err != nil {
			if isNoRows(err) {
				return ErrConversationNotFound
			}
			return err
		}
		rec.Set("status", ConvActive)
		rec.Set("resume_pending", true)
		if err := txApp.Save(rec); err != nil {
			return err
		}
		resumed = conversationFromRecord(rec)
		return nil
	})
	if err != nil {
		if isUniqueErr(err) {
			return nil, ErrConversationBusy
		}
		return nil, err
	}
	return resumed, nil
}

// ClearConversationResumePending marks the saved remote conversation as
// already selected. It is called only after the resumed ask succeeds; a
// failed ask leaves the flag set so its retry navigates to the same URL.
func (s *Store) ClearConversationResumePending(ctx context.Context, id string) error {
	_, err := s.app.DB().NewQuery(
		`UPDATE {{conversations}} SET [[resume_pending]] = {:done} WHERE [[id]] = {:id} AND [[resume_pending]] = {:pending}`,
	).Bind(dbx.Params{"done": false, "id": id, "pending": true}).Execute()
	return err
}

// ArchiveConversation marks one conversation archived (idempotent).
func (s *Store) ArchiveConversation(ctx context.Context, id string) error {
	_, err := s.app.DB().NewQuery(
		`UPDATE {{conversations}} SET [[status]] = {:archived} WHERE [[id]] = {:id} AND [[status]] = {:active}`,
	).Bind(dbx.Params{"archived": ConvArchived, "id": id, "active": ConvActive}).Execute()
	return err
}

// ActiveConversation returns the single active conversation, or nil.
func (s *Store) ActiveConversation(ctx context.Context) (*Conversation, error) {
	cols, err := s.app.FindRecordsByFilter(
		CollectionConversations,
		"status = {:active}",
		"-created",
		1, 0,
		dbx.Params{"active": ConvActive},
	)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, nil
	}
	return conversationFromRecord(cols[0]), nil
}

// ConversationHasSuccessfulTask reports whether any task of the
// conversation ever succeeded (drives auth_required archiving and the
// "active already has a success" rules).
func (s *Store) ConversationHasSuccessfulTask(ctx context.Context, conversationID string) (bool, error) {
	return s.conversationHasSuccessfulTask(s.app, ctx, conversationID)
}

func (s *Store) conversationHasSuccessfulTask(app core.App, ctx context.Context, conversationID string) (bool, error) {
	return exists(app, ctx,
		`SELECT EXISTS(
			SELECT 1 FROM {{tasks}} AS t
			JOIN {{turns}} AS tn ON t.turn = tn.id
			WHERE tn.conversation = {:conv} AND t.status = {:st}
		)`,
		dbx.Params{"conv": conversationID, "st": TaskSucceeded})
}

// exists runs a SELECT EXISTS(...) and reports whether it returned 1.
func exists(app core.App, ctx context.Context, sql string, params dbx.Params) (bool, error) {
	var n int64
	if err := app.DB().NewQuery(sql).Bind(params).Row(&n); err != nil {
		return false, err
	}
	return n == 1, nil
}
