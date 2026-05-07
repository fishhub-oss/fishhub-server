package queue

import (
	"context"
	"log/slog"
)

type noopQueue struct {
	logger *slog.Logger
}

func NewNoOpQueue(logger *slog.Logger) Queue {
	return &noopQueue{logger: logger}
}

func (q *noopQueue) Enqueue(_ context.Context, queue string, job Job) error {
	q.logger.Debug("queue noop: job not enqueued", "queue", queue, "job_id", job.ID, "job_type", job.Type)
	return nil
}
