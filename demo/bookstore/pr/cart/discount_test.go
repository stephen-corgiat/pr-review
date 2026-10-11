package cart

import (
	"errors"
	"testing"
	"time"
)

var oct1 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestApplyDiscount(t *testing.T) {
	tests := []struct {
		name     string
		subtotal Cents
		code     string
		wantErr  error
		want     Cents // Total() after applying
	}{
		{"percent off", 6000, "BOOKWORM20", nil, 4800},
		{"any case", 6000, " bookworm20 ", nil, 4800},
		{"below the minimum", 4000, "BOOKWORM20", ErrBelowMinimum, 4000},
		{"expired", 6000, "SUMMER15", ErrExpired, 6000},
		{"unknown", 6000, "FREEBOOKS", ErrUnknownCode, 6000},
		{"no minimum, plus shipping", 1000, "WELCOME10", nil, 900 + ShippingFee},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Cart{Items: []Item{{ISBN: "1", Price: tt.subtotal, Qty: 1}}}
			err := c.ApplyDiscount(tt.code, oct1)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ApplyDiscount() error = %v, want %v", err, tt.wantErr)
			}
			if got := c.Total(); got != tt.want {
				t.Errorf("Total() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyDiscountReplaces(t *testing.T) {
	c := Cart{Items: []Item{{ISBN: "1", Price: 6000, Qty: 1}}}
	c.ApplyDiscount("WELCOME10", oct1)
	c.ApplyDiscount("BOOKWORM20", oct1)
	if d, _ := c.Discount(); d.Code != "BOOKWORM20" {
		t.Fatalf("discount = %q, want BOOKWORM20", d.Code)
	}
}
