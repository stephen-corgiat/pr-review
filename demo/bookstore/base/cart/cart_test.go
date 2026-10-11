package cart

import "testing"

func TestAddMergesCopies(t *testing.T) {
	var c Cart
	c.Add(Item{ISBN: "1", Price: 1000, Qty: 1})
	c.Add(Item{ISBN: "1", Price: 1000, Qty: 2})
	if len(c.Items) != 1 || c.Items[0].Qty != 3 {
		t.Fatalf("items = %+v, want one line with 3 copies", c.Items)
	}
}

func TestRemove(t *testing.T) {
	var c Cart
	c.Add(Item{ISBN: "1", Price: 1000, Qty: 1})
	c.Add(Item{ISBN: "2", Price: 500, Qty: 1})
	c.Remove("1")
	if got := c.Subtotal(); got != 500 {
		t.Fatalf("subtotal = %v, want $5.00", got)
	}
}

func TestTotal(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		want  Cents
	}{
		{"empty", nil, 0},
		{"below free shipping", []Item{{ISBN: "1", Price: 1899, Qty: 1}}, 1899 + ShippingFee},
		{"free shipping", []Item{{ISBN: "1", Price: 1899, Qty: 2}}, 3798},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Cart{Items: tt.items}
			if got := c.Total(); got != tt.want {
				t.Errorf("Total() = %v, want %v", got, tt.want)
			}
		})
	}
}
