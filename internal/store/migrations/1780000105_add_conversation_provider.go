package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// ProviderCutoff marks the deploy timeline switch from the gemini
// deployment to OPENCLI_SITE=grok on 2026-08-31T16:25:00Z: conversations
// created before it are gemini, at/after it grok.
const ProviderCutoff = "2026-08-31T16:25:00Z"

// BackfillProviders stamps provider on rows not yet stamped, by that
// timeline. PocketBase stores datetimes as "2006-01-02 15:04:05.000Z"
// (space separator, not 'T'), so the comparison normalizes both sides
// with datetime() — a raw string compare against a 'T'-form cut would
// sort every row below the cut (space < 'T') and mislabel them gemini.
func BackfillProviders(db dbx.Builder) error {
	for _, site := range []struct{ name, cond string }{
		{"gemini", "datetime([[created]]) < datetime({:cut})"},
		{"grok", "datetime([[created]]) >= datetime({:cut})"},
	} {
		if _, err := db.NewQuery(
			`UPDATE {{conversations}} SET [[provider]] = {:site} WHERE [[provider]] = '' AND `+site.cond,
		).Bind(dbx.Params{"site": site.name, "cut": ProviderCutoff}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	migrations.Register(func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.TextField{Name: "provider", Max: 20})
		if err := txApp.Save(col); err != nil {
			return err
		}
		return BackfillProviders(txApp.DB())
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId("conversations")
		if err != nil {
			return err
		}
		col.Fields.RemoveByName("provider")
		return txApp.Save(col)
	})
}
