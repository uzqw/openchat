package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// turns: one row per user submission. The composite unique index
// (conversation, idempotency_key) is the backstop that guarantees a
// client retry can never create a second Gemini write task.
func init() {
	migrations.Register(func(txApp core.App) error {
		convs, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col := core.NewBaseCollection("turns")
		col.Fields.Add(&core.RelationField{Name: "conversation", CollectionId: convs.Id, MaxSelect: 1, Required: true})
		col.Fields.Add(&core.TextField{Name: "prompt", Required: true, Max: 100000})
		col.Fields.Add(&core.TextField{Name: "idempotency_key", Required: true, Max: 200})
		col.Fields.Add(&core.AutodateField{Name: "created", System: true, OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", System: true, OnCreate: true, OnUpdate: true})
		col.AddIndex("idx_turns_conversation_idemkey", true, "conversation,idempotency_key", "")
		return txApp.Save(col)
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("turns")
		if err != nil {
			return err
		}
		return txApp.Delete(col)
	})
}
