package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)


// Publisher publishes a payload to an MQTT topic.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

type pahoPublisher struct {
	client paho.Client
}

// NewPublisher connects to the HiveMQ broker with the given server credentials and returns a Publisher.
func NewPublisher(host string, port int, username, password string) (Publisher, error) {
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tls://%s:%d", host, port)).
		SetClientID("fishhub-server").
		SetUsername(username).
		SetPassword(password).
		SetTLSConfig(&tls.Config{}).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetCleanSession(true)

	c := paho.NewClient(opts)
	token := c.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("mqtt: connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("mqtt: connect: %w", err)
	}

	return &pahoPublisher{client: c}, nil
}

func (p *pahoPublisher) Publish(_ context.Context, topic string, payload []byte) error {
	token := p.client.Publish(topic, 0, true, payload)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt: publish timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt: publish: %w", err)
	}
	return nil
}

// noopPublisher is returned when HIVEMQ_HOST is not configured.
type noopPublisher struct{}

func NewNoOpPublisher() Publisher                                                  { return &noopPublisher{} }
func (n *noopPublisher) Publish(_ context.Context, _ string, _ []byte) error { return nil }
