// Command dispatchly is a self-hosted newsletter manager.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/acme/dispatchly/internal/bounce"
	"github.com/acme/dispatchly/internal/core"
	"github.com/acme/dispatchly/models"
)

// App is the application's shared state.
type App struct {
	core     *core.Core
	bounce   *bounce.Manager
	consts   Consts
	settings models.Settings
	log      *log.Logger
}

// Ctx is one HTTP request and its response.
type Ctx struct {
	W http.ResponseWriter
	R *http.Request
}

// Param returns a path parameter.
func (c *Ctx) Param(name string) string { return c.R.PathValue(name) }

// Header returns a request header.
func (c *Ctx) Header(name string) string { return c.R.Header.Get(name) }

// JSON writes v as the response body.
func (c *Ctx) JSON(code int, v any) error {
	c.W.Header().Set("Content-Type", "application/json")
	c.W.WriteHeader(code)
	return json.NewEncoder(c.W).Encode(v)
}

// Error writes a JSON error response.
func (c *Ctx) Error(code int, msg string) error {
	return c.JSON(code, map[string]string{"message": msg})
}

type handler func(c *Ctx) error

func wrap(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(&Ctx{W: w, R: r}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// initHTTPHandlers registers the routes.
func initHTTPHandlers(mux *http.ServeMux, a *App) {
	mux.HandleFunc("POST /webhooks/bounce/{service}", wrap(a.BounceWebhook))
	mux.HandleFunc("GET /api/settings", wrap(a.GetSettings))
	mux.HandleFunc("PUT /api/settings", wrap(a.UpdateSettings))
	mux.HandleFunc("PUT /api/contacts/{id}", wrap(a.UpdateContact))
}

func main() {
	lo := log.New(os.Stderr, "", log.LstdFlags)
	a := &App{log: lo}
	a.settings = loadSettings()
	a.consts = initConstConfig(a.settings)
	a.bounce = initBounceManager(a)
	go a.bounce.Run()

	mux := http.NewServeMux()
	initHTTPHandlers(mux, a)
	lo.Fatal(http.ListenAndServe(":9000", mux))
}

// loadSettings reads the settings from the database.
func loadSettings() models.Settings {
	return models.Settings{}
}
