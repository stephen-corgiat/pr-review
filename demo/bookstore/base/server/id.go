package server

import (
	"crypto/rand"
	"encoding/hex"
)

// newCartID is a random cart cookie value.
func newCartID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
