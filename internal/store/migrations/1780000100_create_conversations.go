package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// conversations: one row per local chat session. The partial unique index
// is the database-level guarantee that at most one conversation is active
// at any moment (the app-level guard is only ever a fast path).
func init() {
	migrations.Register(func(txApp core.App) error {
		col := core.NewBaseCollection("conversations")
		// Rules stay nil (deny all): the business API is the only writer.
		col.Fields.Add(&core.TextField{Name: "title", Max: 500})
		col.Fields.Add(&core.SelectField{Name: "status", Required: true, Values: []string{"active", "archived"}})
		col.Fields.Add(&core.AutodateField{Name: "created", System: true, OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", System: true, OnCreate: true, OnUpdate: true})
		col.AddIndex("idx_conversations_single_active", true, "status", "status = 'active'")
		return txApp.Save(col)
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		return txApp.Delete(col)
	})
}
