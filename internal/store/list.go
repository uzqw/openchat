package store

import (
	"context"

	"github.com/pocketbase/dbx"
)

// ListConversations returns one page of conversations ordered by creation
// (newest first) plus the total item count. page starts at 1, perPage is
// the page size; the caller validates the bounds.
func (s *Store) ListConversations(ctx context.Context, page, perPage int) ([]*Conversation, int, error) {
	var total int64
	if err := s.app.DB().NewQuery(
		`SELECT COUNT(*) FROM {{conversations}}`,
	).Row(&total); err != nil {
		return nil, 0, err
	}
	recs, err := s.app.FindRecordsByFilter(
		CollectionConversations,
		"",
		"-created,id",
		perPage,
		(page-1)*perPage,
	)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Conversation, 0, len(recs))
	for _, r := range recs {
		out = append(out, conversationFromRecord(r))
	}
	return out, int(total), nil
}

// TurnsOfConversation returns all turns of a conversation in creation
// order (oldest first), for the read-only history view.
func (s *Store) TurnsOfConversation(ctx context.Context, convID string) ([]*Turn, error) {
	recs, err := s.app.FindRecordsByFilter(
		CollectionTurns,
		"conversation = {:conv}",
		"created,id",
		1<<20, 0,
		dbx.Params{"conv": convID},
	)
	if err != nil {
		return nil, err
	}
	out := make([]*Turn, 0, len(recs))
	for _, r := range recs {
		out = append(out, turnFromRecord(r))
	}
	return out, nil
}
