package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"openchat/internal/provider"
	"openchat/internal/queue"
	"openchat/internal/service"
	"openchat/internal/store"
)

// ---- JSON response shapes --------------------------------------------------

type conversationJSON struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	RemoteID string    `json:"remote_id,omitempty"`
	Created  time.Time `json:"created"`
}

type taskJSON struct {
	ID                    string     `json:"id"`
	TurnID                string     `json:"turn"`
	RetryOf               string     `json:"retry_of,omitempty"`
	RequestedModel        string     `json:"requested_model"`
	ResolvedModel         string     `json:"resolved_model"`
	Thinking              string     `json:"thinking"`
	Status                string     `json:"status"`
	Result                string     `json:"result,omitempty"`
	ErrorCode             string     `json:"error_code,omitempty"`
	ErrorMessage          string     `json:"error_message,omitempty"`
	UnknownAcknowledgedAt *time.Time `json:"unknown_acknowledged_at,omitempty"`
	LatencyMS             int64      `json:"latency_ms"`
	Created               time.Time  `json:"created"`
}

type turnJSON struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation"`
	Prompt         string     `json:"prompt"`
	IdempotencyKey string     `json:"idempotency_key"`
	Created        time.Time  `json:"created"`
	Tasks          []taskJSON `json:"tasks"`
	CurrentTask    *taskJSON  `json:"current_task,omitempty"`
}

type paginatedJSON struct {
	Items      []conversationJSON `json:"items"`
	Page       int                `json:"page"`
	PerPage    int                `json:"perPage"`
	TotalItems int                `json:"totalItems"`
	TotalPages int                `json:"totalPages"`
}

type turnRequestJSON struct {
	Prompt   string `json:"prompt"`
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
}

func toConversation(c *store.Conversation) conversationJSON {
	return conversationJSON{ID: c.ID, Title: c.Title, Status: c.Status, RemoteID: c.RemoteID, Created: c.Created}
}

func toTask(t *store.Task) taskJSON {
	return taskJSON{
		ID:                    t.ID,
		TurnID:                t.TurnID,
		RetryOf:               t.RetryOf,
		RequestedModel:        t.RequestedModel,
		ResolvedModel:         t.ResolvedModel,
		Thinking:              t.Thinking,
		Status:                t.Status,
		Result:                t.Result,
		ErrorCode:             t.ErrorCode,
		ErrorMessage:          t.ErrorMessage,
		UnknownAcknowledgedAt: t.UnknownAcknowledgedAt,
		LatencyMS:             t.LatencyMS,
		Created:               t.Created,
	}
}

func toTaskList(tasks []*store.Task) []taskJSON {
	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, toTask(t))
	}
	return out
}

func toTurn(t *store.Turn, tasks []*store.Task, current *store.Task) turnJSON {
	out := turnJSON{
		ID:             t.ID,
		ConversationID: t.ConversationID,
		Prompt:         t.Prompt,
		IdempotencyKey: t.IdempotencyKey,
		Created:        t.Created,
		Tasks:          toTaskList(tasks),
	}
	if current != nil {
		c := toTask(current)
		out.CurrentTask = &c
	}
	return out
}

// ---- error mapping ----------------------------------------------------------

// apiErrorOf maps a service/store/queue/provider error to the unified
// envelope: status code, stable code and a safe message. Unknown errors
// become a generic 500 (no internals are ever leaked).
func apiErrorOf(err error) (int, string, string) {
	switch {
	case errors.Is(err, errBadRequest), errors.Is(err, service.ErrValidation):
		return http.StatusBadRequest, "invalid_request", err.Error()
	case errors.Is(err, store.ErrConversationNotFound),
		errors.Is(err, store.ErrTurnNotFound),
		errors.Is(err, store.ErrTaskNotFound):
		return http.StatusNotFound, "not_found", "resource not found"
	case errors.Is(err, store.ErrIdempotencyConflict):
		return http.StatusConflict, "idempotency_conflict", "idempotency key was used with a different request body"
	case errors.Is(err, store.ErrConversationBusy):
		return http.StatusConflict, "conversation_busy", "another task is pending/running or Gemini is quarantined"
	case errors.Is(err, store.ErrConversationArchived):
		return http.StatusConflict, "conversation_archived", "conversation is archived and read-only"
	case errors.Is(err, store.ErrConversationNotResumable):
		return http.StatusConflict, "conversation_not_resumable", "conversation has no saved Gemini remote session and cannot be resumed"
	case errors.Is(err, store.ErrTurnUnfinished):
		return http.StatusConflict, "turn_unfinished", "the previous turn is still pending"
	case errors.Is(err, store.ErrPrevTurnNotSucceeded):
		return http.StatusConflict, "previous_turn_not_succeeded", "the previous turn must succeed before asking again"
	case errors.Is(err, store.ErrTaskNotPending):
		return http.StatusConflict, "task_not_pending", "task is not pending"
	case errors.Is(err, store.ErrTaskNotRetryable):
		return http.StatusConflict, "task_not_retryable", "task cannot be retried"
	case errors.Is(err, store.ErrTaskNotUnknown):
		return http.StatusConflict, "task_not_unknown", "task is not an unknown outcome"
	case errors.Is(err, queue.ErrFull):
		return http.StatusTooManyRequests, "queue_full", "Gemini queue is full, try again later"
	case errors.Is(err, provider.ErrLoginInProgress):
		return http.StatusConflict, "login_in_progress", "a Gemini login is already queued or running"
	case errors.Is(err, provider.ErrLoginBlocked):
		return http.StatusConflict, "login_blocked", "Gemini login is blocked while a conversation is active or Gemini is quarantined"
	case errors.Is(err, provider.ErrRefreshInProgress):
		return http.StatusConflict, "refresh_in_progress", "a Gemini refresh is already queued or running"
	case errors.Is(err, provider.ErrRefreshBlocked):
		return http.StatusConflict, "refresh_blocked", "Gemini refresh is blocked while a conversation is active or Gemini is quarantined"
	case errors.Is(err, provider.ErrAdapterOverride),
		errors.Is(err, provider.ErrPluginInstalled),
		errors.Is(err, provider.ErrVersionMismatch):
		return http.StatusConflict, provider.BlockedCodeOf(err), err.Error()
	}
	return http.StatusInternalServerError, "internal_error", "internal server error"
}

func (a *API) writeServiceErr(w http.ResponseWriter, err error) {
	status, code, msg := apiErrorOf(err)
	writeErr(w, status, code, msg)
}

// ---- handlers --------------------------------------------------------------

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	var one int
	if err := a.svc.St.DB().NewQuery("SELECT 1").Row(&one); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_unavailable", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := a.svc.CreateConversation(r.Context())
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toConversation(conv))
}

func (a *API) handleResumeConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := a.svc.ResumeConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toConversation(conv))
}

func (a *API) handleListConversations(w http.ResponseWriter, r *http.Request) {
	page, perPage, err := pagination(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	items, total, err := a.svc.St.ListConversations(r.Context(), page, perPage)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	convItems := make([]conversationJSON, 0, len(items))
	for _, c := range items {
		convItems = append(convItems, toConversation(c))
	}
	writeJSON(w, http.StatusOK, paginatedJSON{
		Items:      convItems,
		Page:       page,
		PerPage:    perPage,
		TotalItems: total,
		TotalPages: int(math.Ceil(float64(total) / float64(perPage))),
	})
}

// pagination parses the page/perPage query params with sane bounds.
func pagination(r *http.Request) (page, perPage int, err error) {
	page, perPage = 1, 50
	if v := r.URL.Query().Get("page"); v != "" {
		if page, err = strconv.Atoi(v); err != nil || page < 1 {
			return 0, 0, errors.New("page must be a positive integer")
		}
	}
	if v := r.URL.Query().Get("perPage"); v != "" {
		if perPage, err = strconv.Atoi(v); err != nil || perPage < 1 || perPage > 200 {
			return 0, 0, errors.New("perPage must be between 1 and 200")
		}
	}
	return page, perPage, nil
}

func (a *API) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	conv, err := a.svc.St.ConversationByID(r.Context(), id)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	turns, err := a.svc.St.TurnsOfConversation(r.Context(), id)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	turnViews := make([]turnJSON, 0, len(turns))
	for _, t := range turns {
		tasks, err := a.svc.St.TasksOfTurn(r.Context(), t.ID)
		if err != nil {
			a.writeServiceErr(w, err)
			return
		}
		current, err := a.svc.St.CurrentTaskOfTurn(r.Context(), t.ID)
		if err != nil && !errors.Is(err, store.ErrTaskNotFound) {
			a.writeServiceErr(w, err)
			return
		}
		turnViews = append(turnViews, toTurn(t, tasks, current))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        conv.ID,
		"title":     conv.Title,
		"status":    conv.Status,
		"remote_id": conv.RemoteID,
		"created":   conv.Created,
		"turns":     turnViews,
	})
}

func (a *API) handleCreateTurn(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key header is required")
		return
	}
	var body turnRequestJSON
	if err := a.decodeBody(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	turn, _, err := a.svc.CreateTurn(r.Context(), store.TurnRequest{
		ConversationID: r.PathValue("id"),
		Prompt:         body.Prompt,
		IdempotencyKey: key,
		Model:          body.Model,
		Thinking:       body.Thinking,
	})
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	// the first task of the fresh turn is the current one; a replay points
	// at the original first task — either way it is the current task now.
	tasks, err := a.svc.St.TasksOfTurn(r.Context(), turn.ID)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	current, err := a.svc.St.CurrentTaskOfTurn(r.Context(), turn.ID)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toTurn(turn, tasks, current))
}

func (a *API) handleGetTurn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	turn, err := a.svc.St.TurnByID(r.Context(), id)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	tasks, err := a.svc.St.TasksOfTurn(r.Context(), id)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	current, err := a.svc.St.CurrentTaskOfTurn(r.Context(), id)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTurn(turn, tasks, current))
}

func (a *API) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.svc.RetryTask(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toTask(task))
}

func (a *API) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.svc.CancelTask(r.Context(), id); err != nil {
		a.writeServiceErr(w, err)
		return
	}
	task, err := a.svc.St.TaskByID(r.Context(), id)
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTask(task))
}

func (a *API) handleAcknowledgeUnknown(w http.ResponseWriter, r *http.Request) {
	if _, err := a.svc.AcknowledgeUnknown(r.Context(), r.PathValue("id")); err != nil {
		a.writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	snap, err := a.prov.Snapshot(r.Context())
	if err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := a.prov.RequestLogin(r.Context()); err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"login_operation": provider.LoginOpQueued})
}

func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := a.prov.RequestRefresh(r.Context()); err != nil {
		a.writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"refresh_operation": "queued"})
}
