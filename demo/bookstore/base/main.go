// Command bookstore serves the Acme Books storefront API.
package main

import (
	"log"
	"net/http"

	"github.com/acme/bookstore/catalog"
	"github.com/acme/bookstore/server"
)

func main() {
	s := server.New(catalog.Default())
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", s.Routes()))
}
