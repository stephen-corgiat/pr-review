// Package cart is a shopper's cart and what it costs.
package cart

import (
	"fmt"
	"time"
)

// Cents is an amount of money in US cents.
type Cents int64

func (c Cents) String() string { return fmt.Sprintf("$%d.%02d", c/100, c%100) }

const (
	// ShippingFee is charged on carts below FreeShippingFrom.
	ShippingFee Cents = 499
	// FreeShippingFrom is the subtotal at which shipping is free.
	FreeShippingFrom Cents = 3500
)

// An Item is a book in the cart and how many copies.
type Item struct {
	ISBN  string `json:"isbn"`
	Title string `json:"title"`
	Price Cents  `json:"price"`
	Qty   int    `json:"qty"`
}

// A Cart is one shopper's cart.
type Cart struct {
	Items    []Item `json:"items"`
	discount *Discount
}

// Add puts copies of a book in the cart, adding to any already there.
func (c *Cart) Add(it Item) {
	for i := range c.Items {
		if c.Items[i].ISBN == it.ISBN {
			c.Items[i].Qty += it.Qty
			return
		}
	}
	c.Items = append(c.Items, it)
}

// Remove takes a book out of the cart.
func (c *Cart) Remove(isbn string) {
	for i := range c.Items {
		if c.Items[i].ISBN == isbn {
			c.Items = append(c.Items[:i], c.Items[i+1:]...)
			return
		}
	}
}

// Subtotal is the price of everything in the cart.
func (c *Cart) Subtotal() Cents {
	var sum Cents
	for _, it := range c.Items {
		sum += it.Price * Cents(it.Qty)
	}
	return sum
}

// Shipping is the shipping charge: free on an empty cart or from
// FreeShippingFrom.
func (c *Cart) Shipping() Cents {
	if len(c.Items) == 0 || c.Subtotal() >= FreeShippingFrom {
		return 0
	}
	return ShippingFee
}

// ApplyDiscount puts a discount code on the cart, replacing any code
// already there. Codes are case-insensitive.
func (c *Cart) ApplyDiscount(code string, now time.Time) error {
	d, ok := lookupCode(code)
	if !ok {
		return ErrUnknownCode
	}
	if now.After(d.Expires) {
		return ErrExpired
	}
	if c.Subtotal() < d.MinSubtotal {
		return ErrBelowMinimum
	}
	c.discount = &d
	return nil
}

// Discount is the code on the cart, if there is one.
func (c *Cart) Discount() (Discount, bool) {
	if c.discount == nil {
		return Discount{}, false
	}
	return *c.discount, true
}

// Savings is how much the discount code takes off the subtotal.
func (c *Cart) Savings() Cents {
	if c.discount == nil {
		return 0
	}
	return c.Subtotal() * Cents(c.discount.PercentOff) / 100
}

// Total is what the shopper pays: the subtotal less the discount, plus
// shipping.
func (c *Cart) Total() Cents {
	subtotal := c.Subtotal()
	savings := c.Savings()
	shipping := c.Shipping()
	return subtotal - savings + shipping
}
