// Package main is a minimal net/http service used by the action smoke test.
package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.Handle("GET /api/items", withLogging(listItems))
	mux.HandleFunc("POST /api/items", createItem)
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// health reports that the process is running.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func listItems(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}

func createItem(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
}
