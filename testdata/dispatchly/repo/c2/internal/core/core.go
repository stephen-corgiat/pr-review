// Package core holds the application's business logic over the data store.
package core

import "github.com/acme/dispatchly/models"

// Store is the data layer the core runs on.
type Store interface {
	SaveContact(c models.Contact) (models.Contact, error)
	SetLists(contactID int, listIDs []int) error
}

// Core is the business logic.
type Core struct {
	store Store
}

// New returns a Core over store.
func New(store Store) *Core {
	return &Core{store: store}
}
