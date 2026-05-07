package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/fishhub-oss/fishhub-server/internal/actions/alert"
	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/queue"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
	"github.com/hibiken/asynq"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logFormat := os.Getenv("LOG_FORMAT")
	var logHandler slog.Handler
	if logFormat == "json" {
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

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		fmt.Fprintln(os.Stderr, "REDIS_URL is required for the worker")
		os.Exit(1)
	}

	alertStore := alerts.NewPostgresStore(db)
	triggerStore := trigger.NewStore(db)
	processor := alert.NewProcessor(alertStore, triggerStore)

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisURL},
		asynq.Config{
			Queues: map[string]int{"trigger-actions": 1},
			Logger: &asynqLogger{logger},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(processor.Type(), func(ctx context.Context, t *asynq.Task) error {
		return processor.Process(ctx, queue.Job{
			Type:    t.Type(),
			Payload: t.Payload(),
		})
	})

	logger.Info("worker starting", "queue", "trigger-actions")
	go func() {
		<-ctx.Done()
		srv.Shutdown()
	}()

	if err := srv.Run(mux); err != nil {
		fmt.Fprintf(os.Stderr, "worker error: %v\n", err)
		os.Exit(1)
	}
}

type asynqLogger struct{ l *slog.Logger }

func (a *asynqLogger) Debug(args ...any) { a.l.Debug(fmt.Sprint(args...)) }
func (a *asynqLogger) Info(args ...any)  { a.l.Info(fmt.Sprint(args...)) }
func (a *asynqLogger) Warn(args ...any)  { a.l.Warn(fmt.Sprint(args...)) }
func (a *asynqLogger) Error(args ...any) { a.l.Error(fmt.Sprint(args...)) }
func (a *asynqLogger) Fatal(args ...any) {
	a.l.Error(fmt.Sprint(args...))
	os.Exit(1)
}
