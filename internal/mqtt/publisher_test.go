package mqtt_test

import (
	"context"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
)

func TestNoOpPublisher(t *testing.T) {
	ctx := context.Background()
	p := mqtt.NewNoOpPublisher()

	if err := p.Publish(ctx, "fishhub/dev-1/commands/light", []byte(`{"action":"set","state":true}`)); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
