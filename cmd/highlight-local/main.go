package main

import (
	"log"
	"net/http"
	"os"

	"rat/internal/highlightapi"
)

func main() {
	addr := ":8081"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	mux := http.NewServeMux()
	mux.Handle("/highlight", highlightapi.Handler("./cmd/highlight-local/rat"))

	log.Printf("highlight API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
