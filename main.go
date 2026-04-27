package main

import (
	"context"
	"fmt"
	"log/slog"
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
	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

type config struct {
	Port             string
	LogFormat        string
	JWTSecret        string
	JWTTTLHours      int
	GoogleClientID   string
	InfluxHost       string
	InfluxToken      string
	InfluxDatabase   string
	DeviceJWTPEMKey  string
	DeviceJWTKID     string
	IDPHost          string
	HiveMQBaseURL    string
	HiveMQAPIToken   string
	HiveMQRoleID     string
	HiveMQHost       string
	HiveMQPort       int
	HiveMQServerUser string
	HiveMQServerPass string
	CORSOrigins      []string
}

func loadConfig() config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	jwtTTLHours, _ := strconv.Atoi(os.Getenv("JWT_TTL_HOURS"))

	hivemqPort, _ := strconv.Atoi(os.Getenv("HIVEMQ_PORT"))
	if hivemqPort == 0 {
		hivemqPort = 8883
	}

	corsOrigins := []string{"http://localhost:3001"}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		corsOrigins = strings.Split(v, ",")
	}

	return config{
		Port:             port,
		LogFormat:        os.Getenv("LOG_FORMAT"),
		JWTSecret:        os.Getenv("JWT_SECRET"),
		JWTTTLHours:      jwtTTLHours,
		GoogleClientID:   os.Getenv("GOOGLE_CLIENT_ID"),
		InfluxHost:       os.Getenv("INFLUXDB3_HOST"),
		InfluxToken:      os.Getenv("INFLUXDB3_TOKEN"),
		InfluxDatabase:   os.Getenv("INFLUXDB3_DATABASE"),
		DeviceJWTPEMKey:  strings.ReplaceAll(os.Getenv("DEVICE_JWT_PRIVATE_KEY"), `\n`, "\n"),
		DeviceJWTKID:     os.Getenv("DEVICE_JWT_KID"),
		IDPHost:          os.Getenv("IDP_HOST"),
		HiveMQBaseURL:    os.Getenv("HIVEMQ_API_BASE_URL"),
		HiveMQAPIToken:   os.Getenv("HIVEMQ_API_TOKEN"),
		HiveMQRoleID:     os.Getenv("HIVEMQ_DEVICE_ROLE_ID"),
		HiveMQHost:       os.Getenv("HIVEMQ_HOST"),
		HiveMQPort:       hivemqPort,
		HiveMQServerUser: os.Getenv("HIVEMQ_SERVER_USERNAME"),
		HiveMQServerPass: os.Getenv("HIVEMQ_SERVER_PASSWORD"),
		CORSOrigins:      corsOrigins,
	}
}

func main() {
	cfg := loadConfig()

	var logHandler slog.Handler
	if cfg.LogFormat == "json" {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

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

	// ── InfluxDB ──────────────────────────────────────────────────────────────
	var influxClient sensors.InfluxClient
	if cfg.InfluxHost != "" && cfg.InfluxToken != "" && cfg.InfluxDatabase != "" {
		c, err := sensors.NewInfluxClient(cfg.InfluxHost, cfg.InfluxToken, cfg.InfluxDatabase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "influx init: %v\n", err)
			os.Exit(1)
		}
		influxClient = c
		logger.Info("influxdb configured", "host", cfg.InfluxHost, "database", cfg.InfluxDatabase)
	} else {
		logger.Warn("influxdb not configured — readings will not be persisted")
	}

	// ── Auth ──────────────────────────────────────────────────────────────────
	jwtTTL := 24 * time.Hour
	if cfg.JWTTTLHours > 0 {
		jwtTTL = time.Duration(cfg.JWTTTLHours) * time.Hour
	}

	accountStore := account.NewPostgresStore(db)
	authSvc, err := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:    map[string]string{"google": cfg.GoogleClientID},
		Store:        auth.NewPostgresStore(db),
		RefreshStore: auth.NewPostgresRefreshTokenStore(db),
		EventHandler: &account.AccountEventHandler{Store: accountStore},
		JWTSecret:    cfg.JWTSecret,
		JWTTTL:       jwtTTL,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth init: %v\n", err)
		os.Exit(1)
	}

	// ── Device JWT signer ─────────────────────────────────────────────────────
	jwkSigner := jwtutil.Signer(jwtutil.NewNoOp())
	deviceSigner := devicejwt.Signer(devicejwt.NewNoOp())
	if cfg.DeviceJWTPEMKey != "" {
		inner, err := jwtutil.NewRSASigner(cfg.DeviceJWTPEMKey, cfg.DeviceJWTKID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "devicejwt init: %v\n", err)
			os.Exit(1)
		}
		jwkSigner = inner
		deviceSigner = devicejwt.New(inner, cfg.IDPHost)
		logger.Info("device jwt signer configured", "kid", cfg.DeviceJWTKID, "issuer", cfg.IDPHost)
	} else {
		logger.Warn("device jwt not configured — tokens will not be issued at activation")
	}

	// ── HiveMQ ────────────────────────────────────────────────────────────────
	hivemqClient := hivemq.Client(hivemq.NewNoOp())
	if cfg.HiveMQBaseURL != "" {
		hivemqClient = hivemq.NewAPIClient(cfg.HiveMQBaseURL, cfg.HiveMQAPIToken, cfg.HiveMQRoleID)
		logger.Info("hivemq api configured", "base_url", cfg.HiveMQBaseURL)
	} else {
		logger.Warn("hivemq api not configured — mqtt credentials will not be provisioned at activation")
	}

	// ── MQTT publisher ────────────────────────────────────────────────────────
	var mqttPublisher sensors.CommandPublisher = mqtt.NewNoOpPublisher()
	if cfg.HiveMQHost != "" {
		p, err := mqtt.NewPublisher(cfg.HiveMQHost, cfg.HiveMQPort, cfg.HiveMQServerUser, cfg.HiveMQServerPass, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mqtt init: %v\n", err)
			os.Exit(1)
		}
		mqttPublisher = p
		logger.Info("mqtt publisher configured", "host", cfg.HiveMQHost)
	} else {
		logger.Warn("mqtt publishing disabled — HIVEMQ_HOST not set")
	}

	// ── Stores & services ─────────────────────────────────────────────────────
	deviceStore := sensors.NewDeviceStore(db)
	provisioningStore := sensors.NewProvisioningStore(db)
	readingsSvc := sensors.NewReadingsService(deviceStore, influxClient, influxClient, logger)
	deviceSvc := sensors.NewDeviceService(deviceStore, hivemqClient, mqttPublisher, logger)
	provisioningSvc := sensors.NewProvisioningService(provisioningStore, logger)
	activationSvc := sensors.NewActivationService(provisioningStore, hivemqClient, deviceSigner, cfg.HiveMQHost, cfg.HiveMQPort, logger)

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(platform.RequestLogger(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	r.Get("/health", platform.Health)
	r.Get("/.well-known/jwks.json", (&jwtutil.JWKSHandler{Signer: jwkSigner}).ServeHTTP)
	r.Post("/auth/verify", auth.NewVerifyHandler(authSvc, logger).ServeHTTP)
	r.Post("/auth/refresh", auth.NewRefreshHandler(authSvc, logger).ServeHTTP)
	r.Post("/auth/logout", auth.NewLogoutHandler(authSvc).ServeHTTP)
	r.Post("/devices/activate", (&sensors.ActivateHandler{Service: activationSvc}).ServeHTTP)

	r.Group(func(r chi.Router) {
		r.Use(platform.DeviceAuthenticator(deviceSigner))
		r.Post("/readings", (&sensors.ReadingsHandler{Service: readingsSvc}).Create)
	})

	r.Group(func(r chi.Router) {
		r.Use(platform.SessionAuthenticator(authSvc))
		r.Get("/api/me", (&account.MeHandler{Service: &account.AccountService{Store: accountStore}}).ServeHTTP)
		r.Post("/api/devices/provision", (&sensors.ProvisionHandler{Service: provisioningSvc}).ServeHTTP)
		r.Get("/api/devices", (&sensors.DevicesHandler{Service: deviceSvc}).List)
		r.Patch("/api/devices/{id}", (&sensors.PatchDeviceHandler{Service: deviceSvc}).ServeHTTP)
		r.Delete("/api/devices/{id}", (&sensors.DeleteDeviceHandler{Service: deviceSvc}).ServeHTTP)
		r.Get("/api/devices/{id}/readings", (&sensors.ReadingsQueryHandler{Service: readingsSvc}).List)
		r.Post("/api/devices/{id}/peripherals/{name}/commands", (&sensors.CommandHandler{Service: deviceSvc}).ServeHTTP)
	})

	fmt.Printf("listening on :%s\n", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
