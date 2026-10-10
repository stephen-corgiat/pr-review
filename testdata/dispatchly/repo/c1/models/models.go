package models

import (
	"encoding/json"
	"time"
)

// Bounce types.
const (
	BounceTypeSoft      = "soft"
	BounceTypeHard      = "hard"
	BounceTypeComplaint = "complaint"
)

// Contact is a person on one or more mailing lists.
type Contact struct {
	ID     int
	Email  string
	Name   string
	Status string
}

// Bounce is a delivery failure reported by a mail provider.
type Bounce struct {
	Email        string          `json:"email"`
	CampaignUUID string          `json:"campaign_uuid"`
	Source       string          `json:"source"`
	Type         string          `json:"type"`
	Meta         json.RawMessage `json:"meta"`
	CreatedAt    time.Time       `json:"created_at"`
}
