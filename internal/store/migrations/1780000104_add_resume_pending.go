package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// resume_pending records that a resumed conversation still needs one
// deliberate navigation to its saved Gemini URL before the next ask.
func init() {
	migrations.Register(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.BoolField{Name: "resume_pending"})
		return txApp.Save(col)
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col.Fields.RemoveByName("resume_pending")
		return txApp.Save(col)
	})
}
