package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acme/bookstore/catalog"
)

// shopper is one browser: it keeps the cart cookie between requests.
type shopper struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

var oct1 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newShopper(t *testing.T) *shopper {
	s := New(catalog.Default(), func() time.Time { return oct1 })
	return &shopper{t: t, h: s.Routes()}
}

func (sh *shopper) do(method, path, body string) (int, map[string]any) {
	sh.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if sh.cookie != nil {
		req.AddCookie(sh.cookie)
	}
	rec := httptest.NewRecorder()
	sh.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "cart" {
			sh.cookie = ck
		}
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAddAndRemove(t *testing.T) {
	sh := newShopper(t)
	sh.do("POST", "/cart/items", `{"isbn": "9780000000017", "qty": 1}`)
	_, got := sh.do("POST", "/cart/items", `{"isbn": "9780000000024", "qty": 1}`)
	if got["total"] != float64(4349) {
		t.Fatalf("total = %v, want 4349", got["total"])
	}
	_, got = sh.do("DELETE", "/cart/items/9780000000024", "")
	if got["total"] != float64(1899+499) {
		t.Fatalf("total = %v, want %d", got["total"], 1899+499)
	}
}

func TestUnknownBook(t *testing.T) {
	sh := newShopper(t)
	if code, _ := sh.do("POST", "/cart/items", `{"isbn": "nope", "qty": 1}`); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestApplyDiscount(t *testing.T) {
	sh := newShopper(t)
	sh.do("POST", "/cart/items", `{"isbn": "9780000000048", "qty": 2}`)
	code, got := sh.do("POST", "/cart/discount", `{"code": "bookworm20"}`)
	if code != http.StatusOK || got["savings"] != float64(1280) || got["total"] != float64(5120) {
		t.Fatalf("status %d, cart %v: want savings 1280 and total 5120", code, got)
	}
}

func TestDiscountErrors(t *testing.T) {
	sh := newShopper(t)
	sh.do("POST", "/cart/items", `{"isbn": "9780000000017", "qty": 1}`)
	if code, _ := sh.do("POST", "/cart/discount", `{"code": "BOOKWORM20"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("below the minimum: status = %d, want 422", code)
	}
	if code, _ := sh.do("POST", "/cart/discount", `{"code": "FREEBOOKS"}`); code != http.StatusNotFound {
		t.Errorf("unknown code: status = %d, want 404", code)
	}
}
