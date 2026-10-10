package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/acme/dispatchly/models"
)

// passwordMask replaces a stored secret in what the admin UI is sent.
const passwordMask = "•"

// GetSettings returns the settings, with the secrets masked.
func (a *App) GetSettings(c *Ctx) error {
	s := a.settings
	s.BounceSendgrid.Key = strings.Repeat(passwordMask, len(s.BounceSendgrid.Key))
	s.BouncePostmark.Password = strings.Repeat(passwordMask, len(s.BouncePostmark.Password))
	s.BounceBeacon.SharedSecret = strings.Repeat(passwordMask, len(s.BounceBeacon.SharedSecret))
	return c.JSON(http.StatusOK, s)
}

// UpdateSettings saves the settings. A secret left blank or masked keeps
// the value that is stored.
func (a *App) UpdateSettings(c *Ctx) error {
	var set models.Settings
	if err := json.NewDecoder(c.R.Body).Decode(&set); err != nil {
		return c.Error(http.StatusBadRequest, "invalid settings")
	}

	cur := a.settings
	if set.BounceSendgrid.Key == "" || strings.Contains(set.BounceSendgrid.Key, passwordMask) {
		set.BounceSendgrid.Key = cur.BounceSendgrid.Key
	}
	if set.BouncePostmark.Password == "" || strings.Contains(set.BouncePostmark.Password, passwordMask) {
		set.BouncePostmark.Password = cur.BouncePostmark.Password
	}
	if set.BounceBeacon.SharedSecret == "" || strings.Contains(set.BounceBeacon.SharedSecret, passwordMask) {
		set.BounceBeacon.SharedSecret = cur.BounceBeacon.SharedSecret
	}

	a.settings = set
	return c.JSON(http.StatusOK, true)
}
