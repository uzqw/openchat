package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// tasks: one row per Gemini execution attempt of a turn (manual retries
// spawn a new task with retry_of pointing at the original). result is the
// single source of truth for the answer; there is deliberately no separate
// messages table.
func init() {
	migrations.Register(func(txApp core.App) error {
		turnsCol, err := txApp.FindCollectionByNameOrId("turns")
		if err != nil {
			return err
		}
		col := core.NewBaseCollection("tasks")
		col.Fields.Add(&core.RelationField{Name: "turn", CollectionId: turnsCol.Id, MaxSelect: 1, Required: true})
		col.Fields.Add(&core.TextField{Name: "requested_model", Max: 255})
		col.Fields.Add(&core.TextField{Name: "resolved_model", Max: 255})
		col.Fields.Add(&core.SelectField{Name: "thinking", Values: []string{"standard", "extended"}})
		col.Fields.Add(&core.SelectField{Name: "status", Required: true, Values: []string{"pending", "running", "succeeded", "failed", "auth_required", "unknown_outcome", "canceled"}})
		col.Fields.Add(&core.TextField{Name: "result", Max: 8 << 20})
		col.Fields.Add(&core.TextField{Name: "error_code", Max: 64})
		col.Fields.Add(&core.TextField{Name: "error_message", Max: 1000})
		col.Fields.Add(&core.DateField{Name: "unknown_acknowledged_at"})
		col.Fields.Add(&core.NumberField{Name: "latency_ms"})
		col.Fields.Add(&core.AutodateField{Name: "created", System: true, OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", System: true, OnCreate: true, OnUpdate: true})
		if err := txApp.Save(col); err != nil {
			return err
		}
		// self-relation (retry_of) is added after the collection exists, so
		// the relation validation can resolve its own collection
		if err := txApp.ReloadCachedCollections(); err != nil {
			return err
		}
		col.Fields.Add(&core.RelationField{Name: "retry_of", CollectionId: col.Id, MaxSelect: 1})
		return txApp.Save(col)
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("tasks")
		if err != nil {
			return err
		}
		return txApp.Delete(col)
	})
}
