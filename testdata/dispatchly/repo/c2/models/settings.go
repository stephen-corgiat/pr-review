package models

// Settings is the application's settings, stored as key/value rows in the
// settings table and exposed to the admin UI as one JSON object.
type Settings struct {
	AppSiteName string `json:"app.site_name"`
	AppRootURL  string `json:"app.root_url"`
	AppLang     string `json:"app.lang"`

	PrivacyUnsubHeader bool `json:"privacy.unsubscribe_header"`
	PrivacyAllowBlock  bool `json:"privacy.allow_blocklist"`

	BounceEnabled         bool `json:"bounce.enabled"`
	BounceWebhooksEnabled bool `json:"bounce.webhooks_enabled"`
	BounceActions         map[string]struct {
		Count  int    `json:"count"`
		Action string `json:"action"`
	} `json:"bounce.actions"`
	BounceSendgrid struct {
		Enabled bool   `json:"enabled"`
		Key     string `json:"key"`
	} `json:"bounce.sendgrid"`
	BouncePostmark struct {
		Enabled  bool   `json:"enabled"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"bounce.postmark"`
	BounceBeacon struct {
		Enabled            bool   `json:"enabled"`
		SharedSecret       string `json:"shared_secret"`
		SharedSecretHeader string `json:"shared_secret_header"`
	} `json:"bounce.beacon"`
}
