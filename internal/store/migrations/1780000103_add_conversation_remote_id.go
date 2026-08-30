package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// conversations.remote_id: the Gemini web conversation id captured after
// the first successful ask of a conversation. It is what makes an
// archived conversation resumable (gemini detail <id> + ask). Empty for
// conversations created before this migration or whose first ask never
// succeeded — those stay read-only.
func init() {
	migrations.Register(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.TextField{Name: "remote_id", Max: 200})
		return txApp.Save(col)
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col.Fields.RemoveByName("remote_id")
		return txApp.Save(col)
	})
}
