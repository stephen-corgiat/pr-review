// Package server is the storefront's HTTP API.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/acme/bookstore/cart"
	"github.com/acme/bookstore/catalog"
)

// Server holds every shopper's cart, keyed by the cart cookie.
type Server struct {
	catalog *catalog.Catalog
	now     func() time.Time

	mu    sync.Mutex
	carts map[string]*cart.Cart
}

// New returns a Server selling from cat. now is the clock that discount
// codes expire by.
func New(cat *catalog.Catalog, now func() time.Time) *Server {
	return &Server{catalog: cat, now: now, carts: map[string]*cart.Cart{}}
}

// Routes is the API's handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /books", s.books)
	mux.HandleFunc("GET /cart", s.withCart(s.showCart))
	mux.HandleFunc("POST /cart/items", s.withCart(s.addItem))
	mux.HandleFunc("DELETE /cart/items/{isbn}", s.withCart(s.removeItem))
	mux.HandleFunc("POST /cart/discount", s.withCart(s.applyDiscount))
	return mux
}

// withCart finds the shopper's cart from the cart cookie, starting one if
// there's none, and holds the lock while h runs.
func (s *Server) withCart(h func(http.ResponseWriter, *http.Request, *cart.Cart)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := ""
		if ck, err := r.Cookie("cart"); err == nil {
			id = ck.Value
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		c, ok := s.carts[id]
		if !ok {
			id = newCartID()
			c = &cart.Cart{}
			s.carts[id] = c
			http.SetCookie(w, &http.Cookie{Name: "cart", Value: id, Path: "/", Secure: true, HttpOnly: true})
		}
		h(w, r, c)
	}
}

func (s *Server) books(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.catalog.All())
}

// cartView is the cart as the API shows it.
type cartView struct {
	Items    []cart.Item `json:"items"`
	Subtotal cart.Cents  `json:"subtotal"`
	Code     string      `json:"code,omitempty"`
	Savings  cart.Cents  `json:"savings,omitempty"`
	Shipping cart.Cents  `json:"shipping"`
	Total    cart.Cents  `json:"total"`
}

func view(c *cart.Cart) cartView {
	v := cartView{Items: c.Items, Subtotal: c.Subtotal(), Savings: c.Savings(), Shipping: c.Shipping(), Total: c.Total()}
	if d, ok := c.Discount(); ok {
		v.Code = d.Code
	}
	return v
}

func (s *Server) showCart(w http.ResponseWriter, r *http.Request, c *cart.Cart) {
	writeJSON(w, http.StatusOK, view(c))
}

func (s *Server) addItem(w http.ResponseWriter, r *http.Request, c *cart.Cart) {
	var req struct {
		ISBN string `json:"isbn"`
		Qty  int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Qty < 1 {
		writeError(w, http.StatusBadRequest, "want {\"isbn\": \"…\", \"qty\": 1 or more}")
		return
	}
	b, ok := s.catalog.Find(req.ISBN)
	if !ok {
		writeError(w, http.StatusNotFound, "no such book")
		return
	}
	c.Add(cart.Item{ISBN: b.ISBN, Title: b.Title, Price: b.Price, Qty: req.Qty})
	writeJSON(w, http.StatusOK, view(c))
}

func (s *Server) removeItem(w http.ResponseWriter, r *http.Request, c *cart.Cart) {
	c.Remove(r.PathValue("isbn"))
	writeJSON(w, http.StatusOK, view(c))
}

func (s *Server) applyDiscount(w http.ResponseWriter, r *http.Request, c *cart.Cart) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "want {\"code\": \"…\"}")
		return
	}
	if err := c.ApplyDiscount(req.Code, s.now()); err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, cart.ErrUnknownCode) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view(c))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
