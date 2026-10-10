package webhooks

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/acme/dispatchly/models"
)

// Beacon parses webhooks from Beacon Relay. Beacon first asks the receiver
// to prove it is listening (the handshake), then posts a JSON array of
// delivery reports for every message it handles.
type Beacon struct {
	secret string
	header string
}

// beaconEvent is one report in the array Beacon posts.
type beaconEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Time string `json:"time"`
	Data struct {
		Recipient string `json:"recipient"`
		Status    string `json:"status"`
		Details   string `json:"details"`
		// MessageRef is the message's Message-ID header. Beacon's docs
		// don't say when it is set, so nothing here depends on it.
		MessageRef string `json:"messageRef"`
	} `json:"data"`

	raw json.RawMessage
}

// smtpCode matches an enhanced SMTP status code, such as 5.1.1 or 4.2.2.
var smtpCode = regexp.MustCompile(`\b([45])\.\d{1,3}\.\d{1,3}\b`)

// softPhrases are failure details that clear up on their own.
var softPhrases = []string{
	"mailbox full",
	"over quota",
	"try again",
	"temporarily",
	"greylisted",
}

// NewBeacon returns a Beacon parser. An empty secret turns the check off.
// header names a request header that may carry the secret, besides the
// "code" query parameter.
func NewBeacon(secret, header string) *Beacon {
	return &Beacon{
		secret: strings.TrimSpace(secret),
		header: strings.TrimSpace(header),
	}
}

// ProcessHandshake answers Beacon's handshake by echoing the challenge it
// sent. The handshake carries no secret, so it is not checked against one.
func (b *Beacon) ProcessHandshake(body []byte) (map[string]string, error) {
	var req struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("error unmarshalling Beacon handshake: %v", err)
	}
	if req.Challenge == "" {
		return nil, errors.New("Beacon handshake has no challenge")
	}
	return map[string]string{"challenge": req.Challenge}, nil
}

// ProcessBounce checks the shared secret, parses the delivery reports and
// returns a bounce for each one that failed.
func (b *Beacon) ProcessBounce(h http.Header, q url.Values, body []byte) ([]models.Bounce, error) {
	if err := b.verifyAuth(h, q); err != nil {
		return nil, err
	}

	events, err := parseBeaconEvents(body)
	if err != nil {
		return nil, err
	}

	var out []models.Bounce
	for _, e := range events {
		if e.Type != "delivery.report" {
			continue
		}
		typ, ok := mapBeaconStatus(e.Data.Status, e.Data.Details)
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			at = time.Now()
		}
		out = append(out, models.Bounce{
			Email:     strings.ToLower(strings.TrimSpace(e.Data.Recipient)),
			Source:    "beacon",
			Type:      typ,
			Meta:      e.raw,
			CreatedAt: at,
		})
	}
	return out, nil
}

// verifyAuth accepts the shared secret in the "code" query parameter or in
// the configured header. With no secret set, every request is accepted.
func (b *Beacon) verifyAuth(h http.Header, q url.Values) error {
	if b.secret == "" {
		return nil
	}
	if secretsEqual(q.Get("code"), b.secret) {
		return nil
	}
	if b.header != "" && secretsEqual(h.Get(b.header), b.secret) {
		return nil
	}
	return errors.New("invalid Beacon shared secret")
}

// secretsEqual compares two secrets in constant time. Empty values are
// never equal.
func secretsEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// parseBeaconEvents parses the JSON array Beacon posts, keeping each
// event's raw JSON for the bounce's meta.
func parseBeaconEvents(body []byte) ([]beaconEvent, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil {
		return nil, fmt.Errorf("error unmarshalling Beacon events: %v", err)
	}
	events := make([]beaconEvent, 0, len(raws))
	for _, r := range raws {
		var e beaconEvent
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("error unmarshalling Beacon event: %v", err)
		}
		e.raw = r
		events = append(events, e)
	}
	return events, nil
}

// mapBeaconStatus maps a delivery report's status and details to a bounce
// type. The bool is false for reports that aren't bounces.
func mapBeaconStatus(status, details string) (string, bool) {
	d := strings.ToLower(details)

	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complained":
		return models.BounceTypeComplaint, true
	case "suppressed":
		return models.BounceTypeHard, true
	case "failed":
		// An SMTP code says whether the failure is permanent (5.x.x) or
		// temporary (4.x.x).
		if m := smtpCode.FindStringSubmatch(d); m != nil {
			if m[1] == "5" {
				return models.BounceTypeHard, true
			}
			return models.BounceTypeSoft, true
		}
		for _, p := range softPhrases {
			if strings.Contains(d, p) {
				return models.BounceTypeSoft, true
			}
		}
		// Without a code or a soft phrase, a mention of the domain is
		// taken as the recipient's domain being gone.
		if strings.Contains(d, "domain") {
			return models.BounceTypeHard, true
		}
		return models.BounceTypeSoft, true
	}
	return "", false
}
