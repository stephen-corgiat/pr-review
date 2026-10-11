package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acme/bookstore/catalog"
)

// shopper is one browser: it keeps the cart cookie between requests.
type shopper struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func newShopper(t *testing.T) *shopper {
	s := New(catalog.Default())
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
