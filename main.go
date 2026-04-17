package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/fishhub-oss/fishhub-server/internal/handler"
	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/health", handler.Health)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
