package webhooks

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acme/dispatchly/models"
)

// Postmark parses Postmark bounce webhooks.
type Postmark struct {
	username string
	password string
}

type postmarkBounce struct {
	Email       string    `json:"Email"`
	Type        string    `json:"Type"`
	BouncedAt   time.Time `json:"BouncedAt"`
	Description string    `json:"Description"`
}

// NewPostmark returns a Postmark parser that checks basic-auth credentials.
func NewPostmark(username, password string) *Postmark {
	return &Postmark{username: username, password: password}
}

// ProcessBounce checks the basic-auth credentials and parses one bounce.
func (p *Postmark) ProcessBounce(user, pass string, b []byte) (models.Bounce, error) {
	if subtle.ConstantTimeCompare([]byte(user), []byte(p.username)) != 1 ||
		subtle.ConstantTimeCompare([]byte(pass), []byte(p.password)) != 1 {
		return models.Bounce{}, errors.New("invalid Postmark credentials")
	}
	var pb postmarkBounce
	if err := json.Unmarshal(b, &pb); err != nil {
		return models.Bounce{}, fmt.Errorf("error unmarshalling Postmark event: %v", err)
	}
	typ := models.BounceTypeSoft
	switch pb.Type {
	case "HardBounce":
		typ = models.BounceTypeHard
	case "SpamComplaint":
		typ = models.BounceTypeComplaint
	}
	return models.Bounce{
		Email:     pb.Email,
		Source:    "postmark",
		Type:      typ,
		Meta:      b,
		CreatedAt: pb.BouncedAt,
	}, nil
}
