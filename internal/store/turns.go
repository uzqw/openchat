package store

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// firstTurnTitleLength truncates the first prompt for the conversation title.
const firstTurnTitleLength = 60

// PreflightCreateTurn checks, in the locked order, whether a turn may be
// created: (1) idempotency replay, (2) conversation active, (3) no
// unfinished turn in the conversation, (4) previous turn succeeded.
// A replay returns the original turn and task (nil error) without ever
// touching queue capacity. Otherwise it returns nil,nil,nil meaning the
// caller may reserve capacity and call CommitCreateTurn.
func (s *Store) PreflightCreateTurn(ctx context.Context, req TurnRequest) (*Turn, *Task, error) {
	// 1. idempotency replay comes before state and capacity checks.
	if turn, task, err := s.turnByIdempotency(ctx, req.ConversationID, req.IdempotencyKey); err == nil {
		if sameTurnRequest(req, turn, task) {
			return turn, task, nil
		}
		return nil, nil, ErrIdempotencyConflict
	} else if !isNoRows(err) {
		return nil, nil, err
	}

	// 2. active conversation
	conv, err := s.conversationByID(ctx, req.ConversationID)
	if err != nil {
		return nil, nil, err
	}
	if conv.Status != ConvActive {
		return nil, nil, ErrConversationArchived
	}

	// 3+4: conversation turn-state guards
	if err := s.checkTurnGuards(s.app, ctx, req.ConversationID); err != nil {
		return nil, nil, err
	}
	return nil, nil, nil
}

// checkTurnGuards enforces "at most one unfinished turn" and "the previous
// turn's current task succeeded before asking again".
func (s *Store) checkTurnGuards(app core.App, ctx context.Context, convID string) error {
	if unfinished, err := exists(app, ctx,
		`SELECT EXISTS(
			SELECT 1 FROM {{tasks}} AS t
			JOIN {{turns}} AS tn ON t.turn = tn.id
			WHERE tn.conversation = {:conv} AND t.status IN ({:pending}, {:running})
		)`,
		dbx.Params{"conv": convID, "pending": TaskPending, "running": TaskRunning}); err != nil {
		return err
	} else if unfinished {
		return ErrTurnUnfinished
	}

	prevOK, err := s.previousTurnSucceeded(app, ctx, convID)
	if err != nil {
		return err
	}
	if !prevOK {
		return ErrPrevTurnNotSucceeded
	}
	return nil
}

// previousTurnSucceeded reports whether the conversation has no turns yet,
// or the latest turn's current (latest) task succeeded.
func (s *Store) previousTurnSucceeded(app core.App, ctx context.Context, convID string) (bool, error) {
	var latestTurnID string
	err := app.DB().NewQuery(
		`SELECT [[id]] FROM {{turns}} WHERE [[conversation]] = {:conv} ORDER BY [[created]] DESC, [[id]] DESC LIMIT 1`,
	).Bind(dbx.Params{"conv": convID}).Row(&latestTurnID)
	if isNoRows(err) {
		return true, nil // first turn of the conversation
	}
	if err != nil {
		return false, err
	}
	var currentTaskID string
	err = app.DB().NewQuery(
		`SELECT [[id]] FROM {{tasks}} WHERE [[turn]] = {:turn} ORDER BY [[created]] DESC, [[id]] DESC LIMIT 1`,
	).Bind(dbx.Params{"turn": latestTurnID}).Row(&currentTaskID)
	if err != nil {
		return false, err
	}
	task, err := s.taskByID(ctx, currentTaskID)
	if err != nil {
		return false, err
	}
	return task.Status == TaskSucceeded, nil
}

// CommitCreateTurn persists turn + first task (pending) in one transaction.
// The caller must hold a reserved queue slot; on any failure it releases
// it. A concurrent duplicate (unique index backstop) is resolved by
// re-reading: identical body replays the original, different body conflicts.
func (s *Store) CommitCreateTurn(ctx context.Context, req TurnRequest) (*Turn, *Task, error) {
	var turn *Turn
	var task *Task

	err := s.app.RunInTransaction(func(txApp core.App) error {
		if err := s.checkTurnGuards(txApp, ctx, req.ConversationID); err != nil {
			return err
		}
		conv, err := s.conversationByIDTx(txApp, ctx, req.ConversationID)
		if err != nil {
			return err
		}
		if conv.Status != ConvActive {
			return ErrConversationArchived
		}

		turnCol, err := txApp.FindCollectionByNameOrId(CollectionTurns)
		if err != nil {
			return err
		}
		taskCol, err := txApp.FindCollectionByNameOrId(CollectionTasks)
		if err != nil {
			return err
		}

		// title = truncated first prompt, only for the first turn
		first, err := s.countTurnsOfConversation(txApp, ctx, req.ConversationID)
		if err != nil {
			return err
		}
		if first == 0 && conv.Title == "" { // zero existing turns → this is the first
			crec, err := txApp.FindRecordById(CollectionConversations, conv.ID)
			if err != nil {
				return err
			}
			crec.Set("title", truncateRunes(req.Prompt, firstTurnTitleLength))
			if err := txApp.Save(crec); err != nil {
				return err
			}
		}

		turnRec := core.NewRecord(turnCol)
		turnRec.Set("conversation", req.ConversationID)
		turnRec.Set("prompt", req.Prompt)
		turnRec.Set("idempotency_key", req.IdempotencyKey)
		if err := txApp.Save(turnRec); err != nil {
			return err
		}

		taskRec := core.NewRecord(taskCol)
		taskRec.Set("turn", turnRec.Id)
		taskRec.Set("requested_model", req.Model)
		taskRec.Set("thinking", req.Thinking)
		taskRec.Set("status", TaskPending)
		if err := txApp.Save(taskRec); err != nil {
			return err
		}

		turn = turnFromRecord(turnRec)
		task = taskFromRecord(taskRec)
		return nil
	})
	if err != nil {
		if isUniqueErr(err) {
			// concurrent duplicate (conversation, idempotency_key): resolve
			return s.turnByIdempotency(ctx, req.ConversationID, req.IdempotencyKey)
		}
		return nil, nil, err
	}
	return turn, task, nil
}

// conversationByIDTx loads a conversation inside a transaction.
func (s *Store) conversationByIDTx(app core.App, ctx context.Context, id string) (*Conversation, error) {
	rec, err := app.FindRecordById(CollectionConversations, id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return conversationFromRecord(rec), nil
}

// turnByIdempotency looks up an existing turn by (conversation,
// idempotency_key); missing rows surface as sql.ErrNoRows.
func (s *Store) turnByIdempotency(ctx context.Context, convID, key string) (*Turn, *Task, error) {
	var turnID string
	err := s.app.DB().NewQuery(
		`SELECT [[id]] FROM {{turns}} WHERE [[conversation]] = {:conv} AND [[idempotency_key]] = {:key} LIMIT 1`,
	).Bind(dbx.Params{"conv": convID, "key": key}).Row(&turnID)
	if err != nil {
		return nil, nil, err
	}
	turn, err := s.turnByID(ctx, turnID)
	if err != nil {
		return nil, nil, err
	}
	var taskID string
	err = s.app.DB().NewQuery(
		`SELECT [[id]] FROM {{tasks}} WHERE [[turn]] = {:turn} ORDER BY [[created]] ASC, [[id]] ASC LIMIT 1`,
	).Bind(dbx.Params{"turn": turnID}).Row(&taskID)
	if err != nil {
		return nil, nil, err
	}
	task, err := s.taskByID(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	return turn, task, nil
}

// sameTurnRequest reports whether the request body matches the original
// turn/task (prompt on the turn; model/thinking on its first task).
func sameTurnRequest(req TurnRequest, turn *Turn, task *Task) bool {
	return turn.Prompt == req.Prompt &&
		task.RequestedModel == req.Model &&
		task.Thinking == req.Thinking
}

// IsFirstTurn reports whether a turn is the first turn of its conversation
// (first turns open a new web session: --new true).
func (s *Store) IsFirstTurn(ctx context.Context, turnID string) (bool, error) {
	return s.isFirstTurnTx(s.app, ctx, turnID)
}

func (s *Store) isFirstTurnTx(app core.App, ctx context.Context, turnID string) (bool, error) {
	// the turn already exists: it is the first iff its conversation has
	// exactly one turn
	var convID string
	if err := app.DB().NewQuery(
		`SELECT [[conversation]] FROM {{turns}} WHERE [[id]] = {:turn}`,
	).Bind(dbx.Params{"turn": turnID}).Row(&convID); err != nil {
		return false, err
	}
	n, err := s.countTurnsOfConversation(app, ctx, convID)
	return n == 1, err
}

// countTurnsOfConversation returns the number of turns of a conversation.
func (s *Store) countTurnsOfConversation(app core.App, ctx context.Context, convID string) (int64, error) {
	var n int64
	err := app.DB().NewQuery(
		`SELECT COUNT(*) FROM {{turns}} WHERE [[conversation]] = {:conv}`,
	).Bind(dbx.Params{"conv": convID}).Row(&n)
	return n, err
}

// truncateRunes truncates s to at most n runes.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// TurnByID returns a typed turn (public wrapper).
func (s *Store) TurnByID(ctx context.Context, id string) (*Turn, error) { return s.turnByID(ctx, id) }
