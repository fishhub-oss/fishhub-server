package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const batchSize = 50

// Runner polls the outbox and dispatches events to registered processors.
// One goroutine per event type is spawned per tick; rows within each type
// are processed sequentially to preserve ordering.
type Runner struct {
	store       Store
	processors  map[string]EventProcessor
	interval    time.Duration
	maxAttempts int
	logger      *slog.Logger
}

func NewRunner(store Store, processors []EventProcessor, interval time.Duration, maxAttempts int, logger *slog.Logger) *Runner {
	pm := make(map[string]EventProcessor, len(processors))
	for _, p := range processors {
		pm[p.EventType()] = p
	}
	return &Runner{
		store:       store,
		processors:  pm,
		interval:    interval,
		maxAttempts: maxAttempts,
		logger:      logger,
	}
}

func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	events, err := r.store.ClaimBatch(ctx, batchSize)
	if err != nil {
		r.logger.Error("outbox: claim batch", "error", err)
		return
	}
	if len(events) == 0 {
		return
	}

	// Group by event type.
	groups := make(map[string][]Event)
	for _, e := range events {
		groups[e.EventType] = append(groups[e.EventType], e)
	}

	var wg sync.WaitGroup
	for eventType, batch := range groups {
		processor, ok := r.processors[eventType]
		if !ok {
			r.logger.Warn("outbox: no processor registered", "event_type", eventType)
			continue
		}

		wg.Add(1)
		go func(p EventProcessor, batch []Event) {
			defer wg.Done()
			for _, e := range batch {
				r.process(ctx, p, e)
			}
		}(processor, batch)
	}
	wg.Wait()
}

func (r *Runner) process(ctx context.Context, p EventProcessor, e Event) {
	if err := p.Process(ctx, e); err != nil {
		r.logger.Warn("outbox: process failed",
			"event_id", e.ID,
			"event_type", e.EventType,
			"attempts", e.Attempts+1,
			"error", err,
		)
		newAttempts := e.Attempts + 1
		if ferr := r.store.RecordFailure(ctx, e.ID, e.Attempts, r.maxAttempts, err.Error()); ferr != nil {
			r.logger.Error("outbox: record failure", "event_id", e.ID, "error", ferr)
		}
		if newAttempts >= r.maxAttempts {
			r.logger.Error("outbox: event dead",
				"event_id", e.ID,
				"event_type", e.EventType,
				"attempts", newAttempts,
			)
		}
		return
	}

	if err := r.store.MarkCompleted(ctx, e.ID); err != nil {
		r.logger.Error("outbox: mark completed", "event_id", e.ID, "error", err)
	}
}
