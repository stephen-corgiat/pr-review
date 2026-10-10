package main

import (
	"github.com/acme/dispatchly/internal/bounce"
	"github.com/acme/dispatchly/models"
)

// Consts are the settings that need a restart to change.
type Consts struct {
	BounceWebhooksEnabled bool
	BounceSendgridEnabled bool
	BouncePostmarkEnabled bool
	BounceBeaconEnabled   bool
}

// initConstConfig reads the settings that need a restart to change.
func initConstConfig(s models.Settings) Consts {
	return Consts{
		BounceWebhooksEnabled: s.BounceWebhooksEnabled,
		BounceSendgridEnabled: s.BounceSendgrid.Enabled,
		BouncePostmarkEnabled: s.BouncePostmark.Enabled,
		BounceBeaconEnabled:   s.BounceBeacon.Enabled,
	}
}

// initBounceManager builds the bounce manager from the settings.
func initBounceManager(a *App) *bounce.Manager {
	s := a.settings
	b, err := bounce.New(bounce.Opt{
		SendgridEnabled:    s.BounceSendgrid.Enabled,
		SendgridKey:        s.BounceSendgrid.Key,
		PostmarkEnabled:    s.BouncePostmark.Enabled,
		PostmarkUsername:   s.BouncePostmark.Username,
		PostmarkPassword:   s.BouncePostmark.Password,
		BeaconEnabled:      s.BounceBeacon.Enabled,
		BeaconSecret:       s.BounceBeacon.SharedSecret,
		BeaconSecretHeader: s.BounceBeacon.SharedSecretHeader,
		RecordBounce:       a.recordBounce,
	}, a.log)
	if err != nil {
		a.log.Fatalf("error initializing the bounce manager: %v", err)
	}
	return b
}

// recordBounce stores one bounce.
func (a *App) recordBounce(b models.Bounce) error {
	a.log.Printf("bounce: %s %s via %s", b.Email, b.Type, b.Source)
	return nil
}
