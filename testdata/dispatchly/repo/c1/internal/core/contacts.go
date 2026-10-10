package core

import (
	"fmt"

	"github.com/acme/dispatchly/models"
)

// UpdateContactWithLists saves a contact and replaces its list memberships.
// preconfirm marks the new memberships confirmed instead of unconfirmed.
func (c *Core) UpdateContactWithLists(id int, in models.Contact, listIDs []int, preconfirm bool) (models.Contact, error) {
	in.ID = id
	saved, err := c.store.SaveContact(in)
	if err != nil {
		return models.Contact{}, fmt.Errorf("saving contact %d: %w", id, err)
	}
	if err := c.store.SetLists(saved.ID, listIDs); err != nil {
		return models.Contact{}, fmt.Errorf("setting lists for contact %d: %w", id, err)
	}
	if preconfirm {
		saved.Status = "confirmed"
	}
	return saved, nil
}
