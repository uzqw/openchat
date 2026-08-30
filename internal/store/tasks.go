package store

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// TaskCAS atomically moves a task between two states; ok=false when the
// task was not in `from` (someone else won the transition).
func (s *Store) TaskCAS(ctx context.Context, id, from, to string) (bool, error) {
	return taskCAS(s.app, ctx, id, from, to)
}

func taskCAS(app core.App, ctx context.Context, id, from, to string) (bool, error) {
	res, err := app.DB().NewQuery(
		`UPDATE {{tasks}} SET [[status]] = {:to}, [[updated]] = {:now} WHERE [[id]] = {:id} AND [[status]] = {:from}`,
	).Bind(dbx.Params{"to": to, "now": nowString(), "id": id, "from": from}).Execute()
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// TaskByID returns a typed task.
func (s *Store) TaskByID(ctx context.Context, id string) (*Task, error) { return s.taskByID(ctx, id) }

// LoadTaskContext loads a task together with its turn and conversation
// (the runner needs all three).
func (s *Store) LoadTaskContext(ctx context.Context, taskID string) (*Task, *Turn, *Conversation, error) {
	task, err := s.taskByID(ctx, taskID)
	if err != nil {
		return nil, nil, nil, err
	}
	turn, err := s.turnByID(ctx, task.TurnID)
	if err != nil {
		return nil, nil, nil, err
	}
	conv, err := s.conversationByID(ctx, turn.ConversationID)
	if err != nil {
		return nil, nil, nil, err
	}
	return task, turn, conv, nil
}

// CompleteTask persists the terminal outcome of a running task. The
// WHERE status='running' guard keeps concurrent writers (or a mid-run
// recovery) from overwriting a terminal state.
func (s *Store) CompleteTask(ctx context.Context, id, status, errorCode, result, errorMessage string, latencyMS int64) error {
	_, err := s.app.DB().NewQuery(`
		UPDATE {{tasks}}
		SET [[status]] = {:status}, [[error_code]] = {:ec}, [[error_message]] = {:em},
		    [[result]] = {:res}, [[latency_ms]] = {:lat}, [[updated]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = {:running}
	`).Bind(dbx.Params{
		"status":  status,
		"ec":      errorCode,
		"em":      errorMessage,
		"res":     result,
		"lat":     latencyMS,
		"now":     nowString(),
		"id":      id,
		"running": TaskRunning,
	}).Execute()
	return err
}

// CommitRetryTask creates a new pending task that retries the original one
// (copying model/thinking and linking retry_of). Only failed, auth_required
// and canceled tasks on a still-active conversation may be retried.
func (s *Store) CommitRetryTask(ctx context.Context, originalTaskID string) (*Task, error) {
	var created *Task
	err := s.app.RunInTransaction(func(txApp core.App) error {
		orig, err := s.taskByIDTx(txApp, ctx, originalTaskID)
		if err != nil {
			return err
		}
		if !retryableStatuses[orig.Status] {
			return ErrTaskNotRetryable
		}
		turn, err := s.turnByIDTx(txApp, ctx, orig.TurnID)
		if err != nil {
			return err
		}
		conv, err := s.conversationByIDTx(txApp, ctx, turn.ConversationID)
		if err != nil {
			return err
		}
		if conv.Status != ConvActive {
			return ErrConversationArchived
		}

		taskCol, err := txApp.FindCollectionByNameOrId(CollectionTasks)
		if err != nil {
			return err
		}
		rec := core.NewRecord(taskCol)
		rec.Set("turn", orig.TurnID)
		rec.Set("retry_of", orig.ID)
		rec.Set("requested_model", orig.RequestedModel)
		rec.Set("thinking", orig.Thinking)
		rec.Set("status", TaskPending)
		if err := txApp.Save(rec); err != nil {
			return err
		}
		created = taskFromRecord(rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) taskByIDTx(app core.App, ctx context.Context, id string) (*Task, error) {
	rec, err := app.FindRecordById(CollectionTasks, id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return taskFromRecord(rec), nil
}

func (s *Store) turnByIDTx(app core.App, ctx context.Context, id string) (*Turn, error) {
	rec, err := app.FindRecordById(CollectionTurns, id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrTurnNotFound
		}
		return nil, err
	}
	return turnFromRecord(rec), nil
}

// CurrentTaskOfTurn returns the latest task of a turn (the poll target for
// the UI).
func (s *Store) CurrentTaskOfTurn(ctx context.Context, turnID string) (*Task, error) {
	var taskID string
	err := s.app.DB().NewQuery(
		`SELECT [[id]] FROM {{tasks}} WHERE [[turn]] = {:turn} ORDER BY [[created]] DESC, [[id]] DESC LIMIT 1`,
	).Bind(dbx.Params{"turn": turnID}).Row(&taskID)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return s.taskByID(ctx, taskID)
}

// TasksOfTurn returns all tasks of a turn ordered oldest first.
func (s *Store) TasksOfTurn(ctx context.Context, turnID string) ([]*Task, error) {
	recs, err := s.app.FindRecordsByFilter(
		CollectionTasks,
		"turn = {:turn}",
		"created",
		1<<20, 0,
		dbx.Params{"turn": turnID},
	)
	if err != nil {
		return nil, err
	}
	out := make([]*Task, 0, len(recs))
	for _, r := range recs {
		out = append(out, taskFromRecord(r))
	}
	return out, nil
}

// AcknowledgeUnknownTask stamps the acknowledgment date on an unknown task;
// ok=false when the task was not an unacknowledged unknown.
func (s *Store) AcknowledgeUnknownTask(ctx context.Context, taskID string) (bool, error) {
	res, err := s.app.DB().NewQuery(`
		UPDATE {{tasks}}
		SET [[unknown_acknowledged_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = {:st} AND [[unknown_acknowledged_at]] = ''
	`).Bind(dbx.Params{
		"now": nowString(),
		"id":  taskID,
		"st":  TaskUnknownOutcome,
	}).Execute()
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Recover performs the startup safety downgrade: pending → canceled,
// running → unknown_outcome (which quarantines Gemini via the derived
// state), and every active conversation is archived. Nothing is ever
// re-dispatched automatically.
func (s *Store) Recover(ctx context.Context) error {
	q := s.app.DB()
	if _, err := q.NewQuery(
		`UPDATE {{tasks}} SET [[status]] = {:to} WHERE [[status]] = {:from}`,
	).Bind(dbx.Params{"to": TaskCanceled, "from": TaskPending}).Execute(); err != nil {
		return err
	}
	if _, err := q.NewQuery(`
		UPDATE {{tasks}}
		SET [[status]] = {:to}, [[error_code]] = {:ec}, [[error_message]] = {:em}
		WHERE [[status]] = {:from}
	`).Bind(dbx.Params{
		"to": TaskUnknownOutcome, "ec": ErrorCodeRecovery,
		"em": "Gemini interrupted by a restart", "from": TaskRunning,
	}).Execute(); err != nil {
		return err
	}
	if _, err := q.NewQuery(
		`UPDATE {{conversations}} SET [[status]] = {:to} WHERE [[status]] = {:from}`,
	).Bind(dbx.Params{"to": ConvArchived, "from": ConvActive}).Execute(); err != nil {
		return err
	}
	return nil
}
