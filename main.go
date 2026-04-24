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

	"github.com/fishhub-oss/fishhub-server/internal/account"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
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

	accountStore := account.NewPostgresStore(db)
	authSvc, err := auth.NewOIDCService(ctx, auth.OIDCConfig{
		Providers: map[string]string{
			"google": os.Getenv("GOOGLE_CLIENT_ID"),
		},
		Store:        auth.NewPostgresStore(db),
		RefreshStore: auth.NewPostgresRefreshTokenStore(db),
		EventHandler: &account.AccountEventHandler{Store: accountStore},
		JWTSecret:    os.Getenv("JWT_SECRET"),
		JWTTTL:       jwtTTL,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth init: %v\n", err)
		os.Exit(1)
	}

	jwkSigner := jwtutil.Signer(jwtutil.NewNoOp())
	deviceSigner := devicejwt.Signer(devicejwt.NewNoOp())
	if pemKey := os.Getenv("DEVICE_JWT_PRIVATE_KEY"); pemKey != "" {
		kid := os.Getenv("DEVICE_JWT_KID")
		issuer := os.Getenv("IDP_HOST")
		inner, err := jwtutil.NewRSASigner(pemKey, kid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "devicejwt init: %v\n", err)
			os.Exit(1)
		}
		jwkSigner = inner
		deviceSigner = devicejwt.New(inner, issuer)
		log.Printf("device JWT signer configured: kid=%s issuer=%s", kid, issuer)
	} else {
		log.Printf("warning: DEVICE_JWT_PRIVATE_KEY not set — token will not be issued at activation")
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
		AllowedMethods:   []string{"GET", "POST", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}))
	r.Get("/health", platform.Health)
	r.Post("/auth/verify", (&auth.VerifyHandler{Service: authSvc}).ServeHTTP)
	r.Post("/auth/refresh", (&auth.RefreshHandler{Service: authSvc}).ServeHTTP)
	r.Post("/auth/logout", (&auth.LogoutHandler{Service: authSvc}).ServeHTTP)
	provisioningStore := sensors.NewProvisioningStore(db)

	hivemqClient := hivemq.Client(hivemq.NewNoOp())
	if baseURL := os.Getenv("HIVEMQ_API_BASE_URL"); baseURL != "" {
		hivemqClient = hivemq.NewAPIClient(
			baseURL,
			os.Getenv("HIVEMQ_API_TOKEN"),
			os.Getenv("HIVEMQ_DEVICE_ROLE_ID"),
		)
		log.Printf("HiveMQ API client configured: base_url=%s", baseURL)
	} else {
		log.Printf("warning: HIVEMQ_API_BASE_URL not set — MQTT credentials will not be provisioned at activation")
	}

	mqttPort, _ := strconv.Atoi(os.Getenv("HIVEMQ_PORT"))
	if mqttPort == 0 {
		mqttPort = 8883
	}

	r.Get("/.well-known/jwks.json", (&jwtutil.JWKSHandler{Signer: jwkSigner}).ServeHTTP)
	r.Post("/devices/activate", (&sensors.ActivateHandler{
		Store:    provisioningStore,
		Signer:   deviceSigner,
		HiveMQ:   hivemqClient,
		MQTTHost: os.Getenv("HIVEMQ_HOST"),
		MQTTPort: mqttPort,
	}).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(platform.DeviceAuthenticator(deviceSigner))
		r.Post("/readings", readings.Create)
	})
	r.Group(func(r chi.Router) {
		r.Use(platform.SessionAuthenticator(authSvc))
		r.Get("/api/me", (&account.MeHandler{Store: accountStore}).ServeHTTP)
		deviceStore := sensors.NewDeviceStore(db)
		r.Post("/api/devices/provision", (&sensors.ProvisionHandler{Store: provisioningStore}).ServeHTTP)
		r.Get("/api/devices", (&sensors.DevicesHandler{Store: deviceStore}).List)
		r.Patch("/api/devices/{id}", (&sensors.PatchDeviceHandler{Store: deviceStore}).ServeHTTP)
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
