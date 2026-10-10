// Package bounce receives bounce notifications from mail providers and
// records them through a queue.
package bounce

import (
	"fmt"
	"log"

	"github.com/acme/dispatchly/internal/bounce/webhooks"
	"github.com/acme/dispatchly/models"
)

// Opt configures a Manager.
type Opt struct {
	SendgridEnabled bool
	SendgridKey     string

	PostmarkEnabled  bool
	PostmarkUsername string
	PostmarkPassword string

	BeaconEnabled      bool
	BeaconSecret       string
	BeaconSecretHeader string

	// RecordBounce stores one bounce.
	RecordBounce func(models.Bounce) error
}

// Manager queues bounces and records them in the background.
type Manager struct {
	queue    chan models.Bounce
	opt      Opt
	log      *log.Logger
	Sendgrid *webhooks.Sendgrid
	Postmark *webhooks.Postmark
	Beacon   *webhooks.Beacon
}

// New returns a Manager with the enabled webhook providers wired in.
func New(opt Opt, lo *log.Logger) (*Manager, error) {
	if opt.RecordBounce == nil {
		return nil, fmt.Errorf("bounce: RecordBounce is required")
	}
	m := &Manager{
		queue: make(chan models.Bounce, 1000),
		opt:   opt,
		log:   lo,
	}
	if opt.SendgridEnabled {
		m.Sendgrid = webhooks.NewSendgrid(opt.SendgridKey)
	}
	if opt.PostmarkEnabled {
		m.Postmark = webhooks.NewPostmark(opt.PostmarkUsername, opt.PostmarkPassword)
	}
	if opt.BeaconEnabled {
		m.Beacon = webhooks.NewBeacon(opt.BeaconSecret, opt.BeaconSecretHeader)
	}
	return m, nil
}

// Run records queued bounces until the queue is closed.
func (m *Manager) Run() {
	for b := range m.queue {
		if err := m.opt.RecordBounce(b); err != nil {
			m.log.Printf("error recording bounce for %s: %v", b.Email, err)
		}
	}
}

// Enqueue adds a bounce to the queue. It fails when the queue is full.
func (m *Manager) Enqueue(b models.Bounce) error {
	select {
	case m.queue <- b:
		return nil
	default:
		return fmt.Errorf("bounce queue is full")
	}
}
