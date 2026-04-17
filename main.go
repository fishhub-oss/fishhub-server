package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	appdb "github.com/fishhub-oss/fishhub-server/internal/db"
	"github.com/fishhub-oss/fishhub-server/internal/handler"
	"github.com/fishhub-oss/fishhub-server/internal/store"
	"github.com/go-chi/chi/v5"
)

func main() {
	db, err := appdb.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "db open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := appdb.Migrate(db, "db/migrations"); err != nil {
		fmt.Fprintf(os.Stderr, "db migrate: %v\n", err)
		os.Exit(1)
	}

	if err := appdb.SeedUser(db); err != nil {
		fmt.Fprintf(os.Stderr, "db seed: %v\n", err)
		os.Exit(1)
	}

	tokens := &handler.TokensHandler{
		Store:  store.NewTokenStore(db),
		UserID: appdb.SeedUserID(),
	}
	readings := &handler.ReadingsHandler{}

	r := chi.NewRouter()
	r.Get("/health", handler.Health)
	r.Post("/tokens", tokens.Create)
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticator(store.NewDeviceStore(db)))
		r.Post("/readings", readings.Create)
	})

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
