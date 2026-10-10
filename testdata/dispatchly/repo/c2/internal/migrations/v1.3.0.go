package migrations

import "fmt"

// V1_3_0 adds the Beacon bounce webhook settings. It writes the row with
// an upsert, so a site that added the setting by hand keeps working.
func V1_3_0(tx Tx) error {
	const val = `{"enabled": false, "shared_secret": "", "shared_secret_header": ""}`
	err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('bounce.beacon', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, val)
	if err != nil {
		return fmt.Errorf("v1.3.0: %w", err)
	}
	return nil
}
