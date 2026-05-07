package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/account"
	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/api"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
	"github.com/fishhub-oss/fishhub-server/internal/queue"
	asynqqueue "github.com/fishhub-oss/fishhub-server/internal/queue/asynq"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// deviceFinderBridge adapts device.Store to measurement.DeviceFinder.
// device.Store.FindByIDAndUserID returns (device.Device, error) while
// measurement.DeviceFinder expects only error.
type deviceFinderBridge struct{ store device.Store }

func (b deviceFinderBridge) FindByIDAndUserID(ctx context.Context, deviceID, userID string) error {
	_, err := b.store.FindByIDAndUserID(ctx, deviceID, userID)
	if err != nil {
		return device.ErrNotFound
	}
	return nil
}

// accountTimezoneReaderBridge adapts account.AccountStore to provisioning.TimezoneReader.
type accountTimezoneReaderBridge struct{ store account.AccountStore }

func (b accountTimezoneReaderBridge) GetTimezone(ctx context.Context, userID string) (string, error) {
	a, err := b.store.FindByUserID(ctx, userID)
	if err != nil {
		return "UTC", err
	}
	return a.Timezone, nil
}

// triggerActionGetterBridge adapts trigger.Store to trigger_events.ActionGetter.
type triggerActionGetterBridge struct{ store trigger.Store }

func (b triggerActionGetterBridge) GetActions(ctx context.Context, triggerID string) ([]trigger_events.TriggerAction, error) {
	actions, err := b.store.GetActions(ctx, triggerID)
	if err != nil {
		return nil, err
	}
	out := make([]trigger_events.TriggerAction, len(actions))
	for i, a := range actions {
		out[i] = trigger_events.TriggerAction{ID: a.ID, Type: a.Type, Config: a.Config}
	}
	return out, nil
}

// deviceIDListerBridge adapts device.Store to account.DeviceLister.
type deviceIDListerBridge struct{ store device.Store }

func (b deviceIDListerBridge) ListIDsByUserID(ctx context.Context, userID string) ([]string, error) {
	devices, err := b.store.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(devices))
	for i, d := range devices {
		ids[i] = d.ID
	}
	return ids, nil
}

type config struct {
	Port             string
	LogFormat        string
	SessionJWTPEMKey string
	SessionJWTKID    string
	JWTTTLHours      int
	GoogleClientID   string
	InfluxHost       string
	InfluxToken        string
	InfluxDatabase     string
	DeviceJWTPEMKey    string
	DeviceJWTKID       string
	IDPHost            string
	HiveMQBaseURL      string
	HiveMQAPIToken     string
	HiveMQRoleID       string
	HiveMQHost         string
	HiveMQPort         int
	HiveMQServerUser   string
	HiveMQServerPass   string
	CORSOrigins        []string
	RedisURL           string
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
		Port:               port,
		LogFormat:          os.Getenv("LOG_FORMAT"),
		SessionJWTPEMKey:   strings.ReplaceAll(os.Getenv("SESSION_JWT_PRIVATE_KEY"), `\n`, "\n"),
		SessionJWTKID:      os.Getenv("SESSION_JWT_KID"),
		JWTTTLHours:        jwtTTLHours,
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		InfluxHost:         os.Getenv("INFLUXDB3_HOST"),
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
		RedisURL:         os.Getenv("REDIS_URL"),
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

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
	var influxClient measurement.Client
	if cfg.InfluxHost != "" && cfg.InfluxToken != "" && cfg.InfluxDatabase != "" {
		c, err := measurement.NewInfluxClient(cfg.InfluxHost, cfg.InfluxToken, cfg.InfluxDatabase)
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

	sessionSigner := jwtutil.Signer(jwtutil.NewNoOp())
	if cfg.SessionJWTPEMKey != "" {
		s, err := jwtutil.NewRSASigner(cfg.SessionJWTPEMKey, cfg.SessionJWTKID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session jwt init: %v\n", err)
			os.Exit(1)
		}
		sessionSigner = s
		logger.Info("session jwt signer configured", "kid", cfg.SessionJWTKID)
	} else {
		logger.Warn("session jwt not configured — session tokens will not be signed with RSA")
	}

	accountStore := account.NewPostgresStore(db)

	authSvc, err := auth.NewOIDCService(ctx, auth.OIDCConfig{
		Providers:         map[string]string{"google": cfg.GoogleClientID},
		Store:             auth.NewPostgresStore(db),
		RefreshStore:      auth.NewPostgresRefreshTokenStore(db),
		EventHandler:      &account.AccountEventHandler{Store: accountStore},
		Signer:            sessionSigner,
		JWTTTL:            jwtTTL,
		GitHubUserFetcher: auth.NewGitHubHTTPFetcher(),
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

	// ── MQTT publisher + subscriber ───────────────────────────────────────────
	var mqttPublisher mqtt.Publisher = mqtt.NewNoOpPublisher()
	var mqttSubscriber mqtt.Subscriber = mqtt.NewNoOpSubscriber()
	if cfg.HiveMQHost != "" {
		p, err := mqtt.NewPublisher(cfg.HiveMQHost, cfg.HiveMQPort, cfg.HiveMQServerUser, cfg.HiveMQServerPass, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mqtt init: %v\n", err)
			os.Exit(1)
		}
		mqttPublisher = p
		logger.Info("mqtt publisher configured", "host", cfg.HiveMQHost)

		sub, err := mqtt.NewSubscriber(cfg.HiveMQHost, cfg.HiveMQPort, cfg.HiveMQServerUser, cfg.HiveMQServerPass, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mqtt subscriber init: %v\n", err)
			os.Exit(1)
		}
		mqttSubscriber = sub
	} else {
		logger.Warn("mqtt publishing disabled — HIVEMQ_HOST not set")
	}

	// ── Stores & services ─────────────────────────────────────────────────────
	deviceStore := device.NewStore(db)
	peripheralStore := peripheral.NewStore(db)
	triggerStore := trigger.NewStore(db)
	provisioningStore := provisioning.NewStore(db)
	outboxStore := outbox.NewPostgresStore(db)
	readingsSvc := measurement.NewReadingsService(deviceFinderBridge{deviceStore}, influxClient, influxClient, logger)
	deviceSvc := device.NewService(deviceStore, hivemqClient, mqttPublisher, logger)
	peripheralSvc := peripheral.NewService(db, peripheralStore, outboxStore, influxClient, mqttPublisher, logger)
	triggerSvc := trigger.NewService(db, triggerStore, outboxStore, logger)
	provisioningSvc := provisioning.NewService(provisioningStore, logger)
	activationSvc := provisioning.NewActivationService(db, provisioningStore, outboxStore, deviceSigner,
		accountTimezoneReaderBridge{accountStore}, logger)

	// ── MQTT readings subscription ────────────────────────────────────────────
	readingsMQTTHandler := measurement.NewReadingsMQTTHandler(deviceStore, readingsSvc, logger)
	if err := mqttSubscriber.Subscribe(ctx, "fishhub/+/readings", readingsMQTTHandler.Handle); err != nil {
		logger.Error("mqtt readings subscription failed", "error", err)
	}

	// ── Queue ─────────────────────────────────────────────────────────────────
	var jobQueue queue.Queue
	if cfg.RedisURL != "" {
		jobQueue = asynqqueue.NewQueue(cfg.RedisURL)
		logger.Info("asynq queue configured", "redis_url", cfg.RedisURL)
	} else {
		jobQueue = queue.NewNoOpQueue(logger)
		logger.Warn("redis not configured — jobs will not be enqueued")
	}

	// ── Alerts store ──────────────────────────────────────────────────────────
	alertStore := alerts.NewPostgresStore(db)

	// ── MQTT trigger_events subscription ──────────────────────────────────────
	triggerEventStore := trigger_events.NewStore(db)
	triggerEventsMQTTHandler := trigger_events.NewMQTTHandler(
		triggerEventStore,
		triggerActionGetterBridge{store: triggerStore},
		jobQueue,
		logger,
	)
	if err := mqttSubscriber.Subscribe(ctx, "fishhub/+/trigger_events", triggerEventsMQTTHandler.Handle); err != nil {
		logger.Error("mqtt trigger_events subscription failed", "error", err)
	}

	// ── Outbox runner ─────────────────────────────────────────────────────────
	outboxRunner := outbox.NewRunner(
		outboxStore,
		[]outbox.EventProcessor{
			provisioning.NewHiveMQProvisionProcessor(hivemqClient, logger),
			peripheral.NewPeripheralPushProcessor(mqttPublisher, logger),
			account.NewConfigPushProcessor(mqttPublisher, logger),
			trigger.NewTriggerPushProcessor(mqttPublisher, logger),
		},
		10*time.Second,
		5,
		logger,
	)
	go outboxRunner.Run(ctx)

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
	r.Get("/.well-known/jwks.json", (&jwtutil.JWKSHandler{Signers: []jwtutil.Signer{jwkSigner, sessionSigner}}).ServeHTTP)
	r.Post("/auth/verify", auth.NewVerifyHandler(authSvc, logger).ServeHTTP)
	r.Post("/auth/refresh", auth.NewRefreshHandler(authSvc, logger).ServeHTTP)
	r.Post("/auth/logout", auth.NewLogoutHandler(authSvc).ServeHTTP)
	r.Post("/devices/activate", (&api.ActivateHandler{Service: activationSvc}).ServeHTTP)

	r.Group(func(r chi.Router) {
		r.Use(platform.DeviceAuthenticator(deviceSigner))
		r.Get("/devices/{id}/status", (&api.ActivationStatusHandler{
			Store:    deviceStore,
			MQTTHost: cfg.HiveMQHost,
			MQTTPort: cfg.HiveMQPort,
		}).ServeHTTP)
	})

	r.Group(func(r chi.Router) {
		r.Use(platform.SessionAuthenticator(authSvc))
		accountSvc := &account.AccountService{
			Store:       accountStore,
			DB:          db,
			OutboxStore: outboxStore,
			Devices:     deviceIDListerBridge{deviceStore},
		}
		r.Get("/api/me", (&account.MeHandler{Service: accountSvc}).ServeHTTP)
		r.Patch("/api/me", (&account.PatchMeHandler{Service: accountSvc}).ServeHTTP)
		r.Post("/api/devices/provision", (&api.ProvisionHandler{Service: provisioningSvc}).ServeHTTP)
		r.Get("/api/devices", (&api.DevicesHandler{Service: deviceSvc}).List)
		r.Patch("/api/devices/{id}", (&api.PatchDeviceHandler{Service: deviceSvc}).ServeHTTP)
		r.Delete("/api/devices/{id}", (&api.DeleteDeviceHandler{Service: deviceSvc}).ServeHTTP)
		r.Get("/api/devices/{id}/readings", (&api.ReadingsQueryHandler{Service: readingsSvc}).List)
		r.Post("/api/devices/{id}/peripherals", (&api.CreatePeripheralHandler{Service: peripheralSvc}).ServeHTTP)
		r.Get("/api/devices/{id}/peripherals", (&api.ListPeripheralsHandler{Service: peripheralSvc}).ServeHTTP)
		r.Put("/api/devices/{id}/peripherals/{peripheralId}/schedule", (&api.SetPeripheralScheduleHandler{Service: peripheralSvc}).ServeHTTP)
		r.Delete("/api/devices/{id}/peripherals/{peripheralId}", (&api.DeletePeripheralHandler{Service: peripheralSvc}).ServeHTTP)
		r.Patch("/api/devices/{id}/peripherals/{peripheralId}/control-mode", (&api.SetControlModeHandler{Service: peripheralSvc}).ServeHTTP)
		r.Post("/api/devices/{id}/peripherals/{peripheralId}/commands", (&api.CommandHandler{Service: peripheralSvc}).ServeHTTP)
		r.Post("/api/devices/{id}/triggers", (&api.CreateTriggerHandler{Service: triggerSvc}).ServeHTTP)
		r.Get("/api/devices/{id}/triggers", (&api.ListTriggersHandler{Service: triggerSvc}).ServeHTTP)
		r.Get("/api/devices/{id}/triggers/{tid}", (&api.GetTriggerHandler{Service: triggerSvc}).ServeHTTP)
		r.Patch("/api/devices/{id}/triggers/{tid}", (&api.PatchTriggerHandler{Service: triggerSvc}).ServeHTTP)
		r.Delete("/api/devices/{id}/triggers/{tid}", (&api.DeleteTriggerHandler{Service: triggerSvc}).ServeHTTP)
		r.Get("/api/devices/{id}/triggers/{tid}/events", (&api.ListTriggerEventsHandler{TriggerStore: triggerStore, EventStore: triggerEventStore}).ServeHTTP)
		r.Get("/api/alerts", (&api.ListAlertsHandler{Store: alertStore}).ServeHTTP)
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "server shutdown: %v\n", err)
		}
	}()

	fmt.Printf("listening on :%s\n", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
