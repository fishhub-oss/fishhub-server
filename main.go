package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/fishhub-oss/fishhub-server/internal/account"
	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/api"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/devicemodel"
	"github.com/fishhub-oss/fishhub-server/internal/emqx"
	"github.com/fishhub-oss/fishhub-server/internal/firmware"
	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/mqttbroker"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
	"github.com/fishhub-oss/fishhub-server/internal/queue"
	asynqqueue "github.com/fishhub-oss/fishhub-server/internal/queue/asynq"
	s3client "github.com/fishhub-oss/fishhub-server/internal/s3"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// deviceOwnerBridge adapts device.Store to firmware.DeviceOwnerChecker.
type deviceOwnerBridge struct{ store device.Store }

func (b deviceOwnerBridge) CheckOwnership(ctx context.Context, deviceID, userID string) error {
	_, err := b.store.FindByIDAndUserID(ctx, deviceID, userID)
	if errors.Is(err, device.ErrNotFound) {
		return firmware.ErrDeviceNotFound
	}
	return err
}

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
	// HiveMQ
	HiveMQBaseURL      string
	HiveMQAPIToken     string
	HiveMQRoleID       string
	HiveMQHost         string
	HiveMQPort         int
	HiveMQServerUser   string
	HiveMQServerPass   string
	// EMQX
	EMQXAPIBaseURL   string
	EMQXAPIKey       string
	EMQXAPISecret    string
	EMQXAuthID       string
	EMQXHost         string
	EMQXPort         int
	EMQXDeviceHost   string
	EMQXDevicePort   int
	EMQXServerUser   string
	EMQXServerPass   string
	// Broker selector
	MQTTBroker       string // "hivemq" (default) or "emqx"
	CORSOrigins      []string
	RedisURL         string
	// S3 / firmware
	S3Endpoint            string
	S3Region              string
	S3Bucket              string
	S3AccessKey           string
	S3SecretKey           string
	GitHubToken           string
	FirmwarePollInterval  time.Duration
	FirmwarePresignExpiry time.Duration
	FirmwareUpdateTimeout time.Duration
	FirmwareRepoSlug      string
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

	emqxPort, _ := strconv.Atoi(os.Getenv("EMQX_PORT"))
	if emqxPort == 0 {
		emqxPort = 1883
	}
	emqxDevicePort, _ := strconv.Atoi(os.Getenv("EMQX_DEVICE_PORT"))
	if emqxDevicePort == 0 {
		emqxDevicePort = 8883
	}

	mqttBroker := os.Getenv("MQTT_BROKER")
	if mqttBroker == "" {
		mqttBroker = "hivemq"
	}

	corsOrigins := []string{"http://localhost:3001"}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		corsOrigins = strings.Split(v, ",")
	}

	firmwarePollInterval := 15 * time.Minute
	if v := os.Getenv("FIRMWARE_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			firmwarePollInterval = d
		}
	}
	firmwarePresignExpiry := 4 * time.Hour
	if v := os.Getenv("FIRMWARE_PRESIGN_EXPIRY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			firmwarePresignExpiry = d
		}
	}
	firmwareUpdateTimeout := 30 * time.Minute
	if v := os.Getenv("FIRMWARE_UPDATE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			firmwareUpdateTimeout = d
		}
	}
	firmwareRepoSlug := os.Getenv("FIRMWARE_REPO_SLUG")
	if firmwareRepoSlug == "" {
		firmwareRepoSlug = "fishhub-oss/fishhub-firmware"
	}

	return config{
		Port:               port,
		LogFormat:          os.Getenv("LOG_FORMAT"),
		SessionJWTPEMKey:   strings.ReplaceAll(os.Getenv("SESSION_JWT_PRIVATE_KEY"), `\n`, "\n"),
		SessionJWTKID:      os.Getenv("SESSION_JWT_KID"),
		JWTTTLHours:        jwtTTLHours,
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		InfluxHost:         os.Getenv("INFLUXDB3_HOST"),
		InfluxToken:        os.Getenv("INFLUXDB3_TOKEN"),
		InfluxDatabase:     os.Getenv("INFLUXDB3_DATABASE"),
		DeviceJWTPEMKey:    strings.ReplaceAll(os.Getenv("DEVICE_JWT_PRIVATE_KEY"), `\n`, "\n"),
		DeviceJWTKID:       os.Getenv("DEVICE_JWT_KID"),
		IDPHost:            os.Getenv("IDP_HOST"),
		HiveMQBaseURL:      os.Getenv("HIVEMQ_API_BASE_URL"),
		HiveMQAPIToken:     os.Getenv("HIVEMQ_API_TOKEN"),
		HiveMQRoleID:       os.Getenv("HIVEMQ_DEVICE_ROLE_ID"),
		HiveMQHost:         os.Getenv("HIVEMQ_HOST"),
		HiveMQPort:         hivemqPort,
		HiveMQServerUser:   os.Getenv("HIVEMQ_SERVER_USERNAME"),
		HiveMQServerPass:   os.Getenv("HIVEMQ_SERVER_PASSWORD"),
		EMQXAPIBaseURL:     os.Getenv("EMQX_API_BASE_URL"),
		EMQXAPIKey:         os.Getenv("EMQX_API_KEY"),
		EMQXAPISecret:      os.Getenv("EMQX_API_SECRET"),
		EMQXAuthID:         os.Getenv("EMQX_AUTH_ID"),
		EMQXHost:           os.Getenv("EMQX_HOST"),
		EMQXPort:           emqxPort,
		EMQXDeviceHost:     os.Getenv("EMQX_DEVICE_HOST"),
		EMQXDevicePort:     emqxDevicePort,
		EMQXServerUser:     os.Getenv("EMQX_SERVER_USERNAME"),
		EMQXServerPass:     os.Getenv("EMQX_SERVER_PASSWORD"),
		MQTTBroker:            mqttBroker,
		CORSOrigins:           corsOrigins,
		RedisURL:              os.Getenv("REDIS_URL"),
		S3Endpoint:            os.Getenv("S3_ENDPOINT"),
		S3Region:              os.Getenv("S3_REGION"),
		S3Bucket:              os.Getenv("S3_BUCKET"),
		S3AccessKey:           os.Getenv("S3_ACCESS_KEY_ID"),
		S3SecretKey:           os.Getenv("S3_SECRET_ACCESS_KEY"),
		GitHubToken:           os.Getenv("GITHUB_TOKEN"),
		FirmwarePollInterval:  firmwarePollInterval,
		FirmwarePresignExpiry: firmwarePresignExpiry,
		FirmwareUpdateTimeout: firmwareUpdateTimeout,
		FirmwareRepoSlug:      firmwareRepoSlug,
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

	// ── MQTT broker provisioner + connection ──────────────────────────────────
	var brokerProvisioner mqttbroker.Provisioner
	var mqttHost string
	var mqttPort int
	var mqttUser, mqttPass string
	var mqttUseTLS bool
	var deviceMQTTHost string
	var deviceMQTTPort int

	switch cfg.MQTTBroker {
	case "emqx":
		if cfg.EMQXAPIBaseURL != "" {
			brokerProvisioner = emqx.NewAPIClient(cfg.EMQXAPIBaseURL, cfg.EMQXAPIKey, cfg.EMQXAPISecret, cfg.EMQXAuthID)
			logger.Info("emqx api configured", "base_url", cfg.EMQXAPIBaseURL)
		} else {
			brokerProvisioner = emqx.NewNoOp()
			logger.Warn("emqx api not configured — mqtt credentials will not be provisioned at activation")
		}
		mqttHost, mqttPort = cfg.EMQXHost, cfg.EMQXPort
		mqttUser, mqttPass = cfg.EMQXServerUser, cfg.EMQXServerPass
		mqttUseTLS = false // plain TCP over private Railway network
		deviceMQTTHost, deviceMQTTPort = cfg.EMQXDeviceHost, cfg.EMQXDevicePort
	default: // "hivemq"
		if cfg.HiveMQBaseURL != "" {
			brokerProvisioner = hivemq.NewAPIClient(cfg.HiveMQBaseURL, cfg.HiveMQAPIToken, cfg.HiveMQRoleID)
			logger.Info("hivemq api configured", "base_url", cfg.HiveMQBaseURL)
		} else {
			brokerProvisioner = hivemq.NewNoOp()
			logger.Warn("hivemq api not configured — mqtt credentials will not be provisioned at activation")
		}
		mqttHost, mqttPort = cfg.HiveMQHost, cfg.HiveMQPort
		mqttUser, mqttPass = cfg.HiveMQServerUser, cfg.HiveMQServerPass
		mqttUseTLS = true // HiveMQ Cloud requires TLS
		deviceMQTTHost, deviceMQTTPort = cfg.HiveMQHost, cfg.HiveMQPort
	}

	var mqttPublisher mqtt.Publisher = mqtt.NewNoOpPublisher()
	var mqttSubscriber mqtt.Subscriber = mqtt.NewNoOpSubscriber()
	if mqttHost != "" {
		p, err := mqtt.NewPublisher(mqttHost, mqttPort, mqttUser, mqttPass, mqttUseTLS, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mqtt init: %v\n", err)
			os.Exit(1)
		}
		mqttPublisher = p
		logger.Info("mqtt publisher configured", "host", mqttHost, "broker", cfg.MQTTBroker, "tls", mqttUseTLS)

		sub, err := mqtt.NewSubscriber(mqttHost, mqttPort, mqttUser, mqttPass, mqttUseTLS, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mqtt subscriber init: %v\n", err)
			os.Exit(1)
		}
		mqttSubscriber = sub
	} else {
		logger.Warn("mqtt publishing disabled — broker host not set", "broker", cfg.MQTTBroker)
	}

	// ── Stores & services ─────────────────────────────────────────────────────
	deviceStore := device.NewStore(db)
	peripheralStore := peripheral.NewStore(db)
	deviceModelStore := devicemodel.NewStore(db)
	triggerStore := trigger.NewStore(db)
	provisioningStore := provisioning.NewStore(db)
	outboxStore := outbox.NewPostgresStore(db)
	readingsSvc := measurement.NewReadingsService(deviceFinderBridge{deviceStore}, influxClient, influxClient, logger)
	deviceSvc := device.NewService(deviceStore, brokerProvisioner, mqttPublisher, logger)
	peripheralSvc := peripheral.NewService(db, peripheralStore, outboxStore, influxClient, mqttPublisher, logger)
	triggerSvc := trigger.NewService(db, triggerStore, outboxStore, logger)
	provisioningSvc := provisioning.NewService(provisioningStore, logger)
	activationSvc := provisioning.NewActivationService(db, provisioningStore, outboxStore, deviceSigner,
		accountTimezoneReaderBridge{accountStore}, deviceModelStore, logger)

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

	// ── Firmware release tracking ─────────────────────────────────────────────
	var s3 s3client.Client
	if cfg.S3Endpoint != "" && cfg.S3Bucket != "" {
		c, err := s3client.NewClient(cfg.S3Endpoint, cfg.S3Region, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "s3 init: %v\n", err)
			os.Exit(1)
		}
		s3 = c
		logger.Info("s3 configured", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)
	} else {
		s3 = s3client.NewNoOp()
		logger.Warn("s3 not configured — firmware release tracking disabled")
	}

	manifests := firmware.NewManifestReader(s3)
	presigner := firmware.NewURLPresigner(s3)
	poller := firmware.NewGitHubPoller(cfg.FirmwareRepoSlug, cfg.GitHubToken, cfg.FirmwarePollInterval, manifests, logger)
	updateStore := firmware.NewDeviceFirmwareStore(db)
	firmwareSvc := firmware.NewService(poller, updateStore, presigner, mqttPublisher, deviceOwnerBridge{deviceStore}, cfg.FirmwarePresignExpiry, cfg.FirmwareUpdateTimeout, logger)

	if err := mqttSubscriber.Subscribe(ctx, "fishhub/+/status", firmware.NewStatusMQTTHandler(firmwareSvc, logger).Handle); err != nil {
		logger.Error("mqtt status subscription failed", "error", err)
	}
	go poller.Run(ctx)

	// ── Outbox runner ─────────────────────────────────────────────────────────
	outboxRunner := outbox.NewRunner(
		outboxStore,
		[]outbox.EventProcessor{
			provisioning.NewMQTTProvisionProcessor(brokerProvisioner, logger),
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
			MQTTHost: deviceMQTTHost,
			MQTTPort: deviceMQTTPort,
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
		r.Get("/api/devices/{id}/model", (&api.DeviceModelHandler{Store: deviceModelStore}).ServeHTTP)
		r.Post("/api/devices/{id}/peripherals", (&api.CreatePeripheralHandler{Service: peripheralSvc, ModelStore: deviceModelStore}).ServeHTTP)
		r.Get("/api/devices/{id}/peripherals", (&api.ListPeripheralsHandler{Service: peripheralSvc}).ServeHTTP)
		r.Put("/api/devices/{id}/peripherals/{peripheralId}/schedule", (&api.SetPeripheralScheduleHandler{Service: peripheralSvc}).ServeHTTP)
		r.Patch("/api/devices/{id}/peripherals/{peripheralId}", (&api.PatchPeripheralHandler{Service: peripheralSvc}).ServeHTTP)
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
		r.Get("/api/devices/{id}/firmware", (&firmware.FirmwareStatusHandler{Service: firmwareSvc}).ServeHTTP)
		r.Post("/api/devices/{id}/firmware/confirm", (&firmware.FirmwareConfirmHandler{Service: firmwareSvc}).ServeHTTP)
		r.Post("/api/devices/{id}/firmware/retry", (&firmware.FirmwareRetryHandler{Service: firmwareSvc}).ServeHTTP)
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
