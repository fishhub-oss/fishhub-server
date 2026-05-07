package queue

import (
	"context"
	"encoding/json"
)

type Job struct {
	ID      string
	Type    string
	Payload json.RawMessage
}

type Queue interface {
	Enqueue(ctx context.Context, queue string, job Job) error
}

type Processor interface {
	Type() string
	Process(ctx context.Context, job Job) error
}
