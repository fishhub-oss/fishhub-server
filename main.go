package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/chi/v5"
)

func main() {
	db, err := platform.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "db open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := platform.Migrate(db, "db/migrations"); err != nil {
		fmt.Fprintf(os.Stderr, "db migrate: %v\n", err)
		os.Exit(1)
	}

	if err := platform.SeedUser(db); err != nil {
		fmt.Fprintf(os.Stderr, "db seed: %v\n", err)
		os.Exit(1)
	}

	var writer sensors.ReadingWriter
	influxHost := os.Getenv("INFLUXDB3_HOST")
	influxToken := os.Getenv("INFLUXDB3_TOKEN")
	influxDatabase := os.Getenv("INFLUXDB3_DATABASE")
	if influxHost != "" && influxToken != "" && influxDatabase != "" {
		w, err := sensors.NewReadingWriter(influxHost, influxToken, influxDatabase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "influx init: %v\n", err)
			os.Exit(1)
		}
		writer = w
		log.Printf("InfluxDB writer configured: host=%s database=%s", influxHost, influxDatabase)
	} else {
		log.Printf("warning: INFLUXDB3_HOST/TOKEN/DATABASE not set — readings will not be persisted to InfluxDB")
	}

	tokens := &sensors.TokensHandler{
		Store:  sensors.NewTokenStore(db),
		UserID: platform.SeedUserID(),
	}
	readings := &sensors.ReadingsHandler{
		Writer: writer,
	}

	r := chi.NewRouter()
	r.Get("/health", platform.Health)
	r.Post("/tokens", tokens.Create)
	r.Group(func(r chi.Router) {
		r.Use(platform.DeviceAuthenticator(sensors.NewDeviceStore(db)))
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
