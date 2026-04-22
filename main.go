package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
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

	var influxClient sensors.InfluxClient
	influxHost := os.Getenv("INFLUXDB3_HOST")
	influxToken := os.Getenv("INFLUXDB3_TOKEN")
	influxDatabase := os.Getenv("INFLUXDB3_DATABASE")
	if influxHost != "" && influxToken != "" && influxDatabase != "" {
		c, err := sensors.NewInfluxClient(influxHost, influxToken, influxDatabase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "influx init: %v\n", err)
			os.Exit(1)
		}
		influxClient = c
		log.Printf("InfluxDB client configured: host=%s database=%s", influxHost, influxDatabase)
	} else {
		log.Printf("warning: INFLUXDB3_HOST/TOKEN/DATABASE not set — readings will not be persisted to InfluxDB")
	}

	ctx := context.Background()

	jwtTTL := 24 * time.Hour
	if h, err := strconv.Atoi(os.Getenv("JWT_TTL_HOURS")); err == nil && h > 0 {
		jwtTTL = time.Duration(h) * time.Hour
	}

	authSvc, err := auth.NewOIDCService(ctx, auth.OIDCConfig{
		Providers: map[string]string{
			"google": os.Getenv("GOOGLE_CLIENT_ID"),
		},
		Store:        auth.NewPostgresStore(db),
		RefreshStore: auth.NewPostgresRefreshTokenStore(db),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		JWTTTL:       jwtTTL,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth init: %v\n", err)
		os.Exit(1)
	}

	tokens := &sensors.TokensHandler{
		Store:  sensors.NewTokenStore(db),
		UserID: platform.SeedUserID(),
	}
	readings := &sensors.ReadingsHandler{
		Writer: influxClient,
	}

	allowedOrigins := []string{"http://localhost:3001"}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		allowedOrigins = strings.Split(v, ",")
	}

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}))
	r.Get("/health", platform.Health)
	r.Post("/auth/verify", (&auth.VerifyHandler{Service: authSvc}).ServeHTTP)
	r.Post("/auth/refresh", (&auth.RefreshHandler{Service: authSvc}).ServeHTTP)
	r.Post("/auth/logout", (&auth.LogoutHandler{Service: authSvc}).ServeHTTP)
	r.Post("/tokens", tokens.Create)
	r.Group(func(r chi.Router) {
		r.Use(platform.DeviceAuthenticator(sensors.NewDeviceStore(db)))
		r.Post("/readings", readings.Create)
	})
	r.Group(func(r chi.Router) {
		r.Use(platform.SessionAuthenticator(authSvc))
		deviceStore := sensors.NewDeviceStore(db)
		r.Get("/api/devices", (&sensors.DevicesHandler{Store: deviceStore}).List)
		r.Get("/api/devices/{id}/readings", (&sensors.ReadingsQueryHandler{
			Querier: influxClient,
			Devices: deviceStore,
		}).List)
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
