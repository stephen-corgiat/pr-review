package main

import (
	"io"
	"net/http"

	"github.com/acme/dispatchly/models"
)

// maxBounceBody caps a webhook body, so a bad sender can't fill the memory.
const maxBounceBody = 1 << 20

// BounceWebhook receives a bounce notification from a mail provider and
// queues the bounces in it. The provider is the "service" path parameter.
func (a *App) BounceWebhook(c *Ctx) error {
	if !a.consts.BounceWebhooksEnabled {
		return c.Error(http.StatusForbidden, "bounce webhooks are disabled")
	}

	body, err := io.ReadAll(io.LimitReader(c.R.Body, maxBounceBody))
	if err != nil {
		return c.Error(http.StatusBadRequest, "error reading the request body")
	}

	var (
		service = c.Param("service")
		bounces []models.Bounce
	)
	switch service {
	case "sendgrid":
		if a.bounce.Sendgrid == nil {
			return c.Error(http.StatusBadRequest, "Sendgrid webhooks are disabled")
		}
		bounces, err = a.bounce.Sendgrid.ProcessBounce(body)

	case "postmark":
		if a.bounce.Postmark == nil {
			return c.Error(http.StatusBadRequest, "Postmark webhooks are disabled")
		}
		user, pass, _ := c.R.BasicAuth()
		var b models.Bounce
		b, err = a.bounce.Postmark.ProcessBounce(user, pass, body)
		bounces = []models.Bounce{b}

	case "beacon":
		if a.bounce.Beacon == nil {
			return c.Error(http.StatusBadRequest, "Beacon webhooks are disabled")
		}
		// Beacon says in a header whether this is its handshake or a
		// batch of delivery reports.
		if c.Header("X-Beacon-Event") == "handshake" {
			resp, err := a.bounce.Beacon.ProcessHandshake(body)
			if err != nil {
				return c.Error(http.StatusBadRequest, err.Error())
			}
			return c.JSON(http.StatusOK, resp)
		}
		bounces, err = a.bounce.Beacon.ProcessBounce(c.R.Header, c.R.URL.Query(), body)

	default:
		return c.Error(http.StatusBadRequest, "unknown bounce service")
	}
	if err != nil {
		a.log.Printf("error processing %s webhook: %v", service, err)
		return c.Error(http.StatusBadRequest, err.Error())
	}

	for _, b := range bounces {
		if err := a.bounce.Enqueue(b); err != nil {
			a.log.Printf("error queueing bounce for %s: %v", b.Email, err)
			return c.Error(http.StatusServiceUnavailable, "error queueing the bounce")
		}
	}

	return c.JSON(http.StatusOK, true)
}
