// Package webhooks parses the bounce notifications mail providers post.
package webhooks

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/acme/dispatchly/models"
)

// Sendgrid parses Sendgrid event webhooks.
type Sendgrid struct {
	key string
}

type sendgridEvent struct {
	Email     string `json:"email"`
	Event     string `json:"event"`
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
}

// NewSendgrid returns a Sendgrid parser that checks the given key.
func NewSendgrid(key string) *Sendgrid {
	return &Sendgrid{key: key}
}

// ProcessBounce turns a Sendgrid event list into bounces.
func (s *Sendgrid) ProcessBounce(b []byte) ([]models.Bounce, error) {
	var events []sendgridEvent
	if err := json.Unmarshal(b, &events); err != nil {
		return nil, fmt.Errorf("error unmarshalling Sendgrid event: %v", err)
	}
	var out []models.Bounce
	for _, e := range events {
		typ := models.BounceTypeSoft
		switch e.Event {
		case "bounce":
			typ = models.BounceTypeHard
		case "spamreport":
			typ = models.BounceTypeComplaint
		case "dropped", "deferred":
		default:
			continue
		}
		raw, _ := json.Marshal(e)
		out = append(out, models.Bounce{
			Email:     e.Email,
			Source:    "sendgrid",
			Type:      typ,
			Meta:      raw,
			CreatedAt: time.Unix(e.Timestamp, 0),
		})
	}
	return out, nil
}
