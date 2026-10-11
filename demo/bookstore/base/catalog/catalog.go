// Package catalog is the books Acme sells.
package catalog

import "github.com/acme/bookstore/cart"

// A Book is one title in the catalog.
type Book struct {
	ISBN   string     `json:"isbn"`
	Title  string     `json:"title"`
	Author string     `json:"author"`
	Price  cart.Cents `json:"price"`
}

// Catalog finds books by ISBN.
type Catalog struct {
	books map[string]Book
	order []string
}

// New builds a Catalog from books.
func New(books ...Book) *Catalog {
	c := &Catalog{books: map[string]Book{}}
	for _, b := range books {
		c.books[b.ISBN] = b
		c.order = append(c.order, b.ISBN)
	}
	return c
}

// Default is the catalog the store ships with.
func Default() *Catalog {
	return New(
		Book{ISBN: "9780000000017", Title: "The Quiet Orchard", Author: "M. Hale", Price: 1899},
		Book{ISBN: "9780000000024", Title: "Salt and Signal", Author: "R. Okafor", Price: 2450},
		Book{ISBN: "9780000000031", Title: "A Field Guide to Small Birds", Author: "J. Lin", Price: 1299},
		Book{ISBN: "9780000000048", Title: "Night Trains of Europe", Author: "A. Novak", Price: 3200},
	)
}

// Find returns the book with the ISBN.
func (c *Catalog) Find(isbn string) (Book, bool) {
	b, ok := c.books[isbn]
	return b, ok
}

// All returns every book, in catalog order.
func (c *Catalog) All() []Book {
	out := make([]Book, 0, len(c.order))
	for _, isbn := range c.order {
		out = append(out, c.books[isbn])
	}
	return out
}
