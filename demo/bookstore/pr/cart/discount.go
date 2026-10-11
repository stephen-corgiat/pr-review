package cart

import (
	"errors"
	"strings"
	"time"
)

// A Discount takes PercentOff percent off the subtotal of a cart that has
// reached MinSubtotal, until it Expires.
type Discount struct {
	Code        string    `json:"code"`
	PercentOff  int       `json:"percentOff"`
	MinSubtotal Cents     `json:"minSubtotal"`
	Expires     time.Time `json:"expires"`
}

var (
	ErrUnknownCode  = errors.New("unknown discount code")
	ErrExpired      = errors.New("discount code has expired")
	ErrBelowMinimum = errors.New("cart is below the code's minimum subtotal")
)

// codes are the live discount codes. Marketing owns this list.
var codes = []Discount{
	{Code: "WELCOME10", PercentOff: 10, Expires: day(2027, 1, 1)},
	{Code: "BOOKWORM20", PercentOff: 20, MinSubtotal: 5000, Expires: day(2026, 12, 31)},
	{Code: "SUMMER15", PercentOff: 15, MinSubtotal: 3000, Expires: day(2026, 9, 1)},
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// lookupCode finds a live code, ignoring case and surrounding spaces.
func lookupCode(code string) (Discount, bool) {
	code = strings.TrimSpace(code)
	for _, d := range codes {
		if strings.EqualFold(d.Code, code) {
			return d, true
		}
	}
	return Discount{}, false
}
