// Package migrations holds the schema upgrades, one per release.
package migrations

import "fmt"

// Tx is the slice of a database transaction a migration needs.
type Tx interface {
	Exec(query string, args ...any) error
}

// Migration upgrades the database from the previous release.
type Migration struct {
	Version string
	Run     func(tx Tx) error
}

// All lists the migrations in release order.
var All = []Migration{
	{Version: "v1.2.0", Run: V1_2_0},
}

// V1_2_0 adds the bounce webhook settings.
func V1_2_0(tx Tx) error {
	for _, s := range []struct{ key, val string }{
		{"bounce.sendgrid", `{"enabled": false, "key": ""}`},
		{"bounce.postmark", `{"enabled": false, "username": "", "password": ""}`},
	} {
		if err := tx.Exec(`INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT DO NOTHING`, s.key, s.val); err != nil {
			return fmt.Errorf("v1.2.0: %w", err)
		}
	}
	return nil
}
