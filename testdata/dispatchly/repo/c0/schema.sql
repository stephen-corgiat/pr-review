-- dispatchly schema.

DROP TABLE IF EXISTS settings CASCADE;
CREATE TABLE settings (
    key   TEXT NOT NULL UNIQUE,
    value JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_settings_key; CREATE INDEX idx_settings_key ON settings(key);

INSERT INTO settings (key, value) VALUES
    ('app.site_name', '"Dispatchly"'),
    ('app.root_url', '"http://localhost:9000"'),
    ('app.lang', '"en"'),
    ('privacy.unsubscribe_header', 'true'),
    ('privacy.allow_blocklist', 'true'),
    ('bounce.enabled', 'false'),
    ('bounce.webhooks_enabled', 'false'),
    ('bounce.actions', '{"soft": {"count": 2, "action": "none"}, "hard": {"count": 1, "action": "blocklist"}, "complaint": {"count": 1, "action": "blocklist"}}'),
    ('bounce.sendgrid', '{"enabled": false, "key": ""}'),
    ('bounce.postmark', '{"enabled": false, "username": "", "password": ""}');
