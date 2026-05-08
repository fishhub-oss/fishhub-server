package asynq

import (
	"context"
	"fmt"

	"github.com/fishhub-oss/fishhub-server/internal/queue"
	"github.com/hibiken/asynq"
)

type Queue struct {
	client *asynq.Client
}

func NewQueue(redisURL string) *Queue {
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		panic(fmt.Sprintf("asynq: invalid redis URL: %v", err))
	}
	return &Queue{
		client: asynq.NewClient(opt),
	}
}

func (q *Queue) Enqueue(_ context.Context, queueName string, job queue.Job) error {
	task := asynq.NewTask(job.Type, job.Payload)
	_, err := q.client.Enqueue(task, asynq.Queue(queueName))
	if err != nil {
		return fmt.Errorf("asynq enqueue: %w", err)
	}
	return nil
}
