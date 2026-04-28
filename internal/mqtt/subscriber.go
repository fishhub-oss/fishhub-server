package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// MessageHandler is called for each message received on a subscribed topic.
type MessageHandler func(ctx context.Context, topic string, payload []byte)

// Subscriber subscribes to MQTT topics and dispatches messages to a handler.
type Subscriber interface {
	Subscribe(ctx context.Context, topic string, handler MessageHandler) error
}

type pahoSubscriber struct {
	client paho.Client
}

// NewSubscriber connects to the HiveMQ broker and returns a Subscriber.
// Uses a separate client ID from the publisher so both can coexist.
// CleanSession(false) ensures the subscription survives reconnects.
func NewSubscriber(host string, port int, username, password string, logger *slog.Logger) (Subscriber, error) {
	if logger == nil {
		logger = slog.Default()
	}
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tls://%s:%d", host, port)).
		SetClientID("fishhub-server-sub").
		SetUsername(username).
		SetPassword(password).
		SetTLSConfig(&tls.Config{}).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetCleanSession(false).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			logger.Warn("mqtt subscriber connection lost", "error", err)
		}).
		SetOnConnectHandler(func(_ paho.Client) {
			logger.Info("mqtt subscriber connected", "host", host, "port", port)
		})

	c := paho.NewClient(opts)
	token := c.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("mqtt subscriber: connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("mqtt subscriber: connect: %w", err)
	}

	return &pahoSubscriber{client: c}, nil
}

func (s *pahoSubscriber) Subscribe(_ context.Context, topic string, handler MessageHandler) error {
	token := s.client.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		handler(context.Background(), msg.Topic(), msg.Payload())
	})
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt subscriber: subscribe timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt subscriber: subscribe: %w", err)
	}
	return nil
}

type noopSubscriber struct{}

func NewNoOpSubscriber() Subscriber { return &noopSubscriber{} }
func (n *noopSubscriber) Subscribe(_ context.Context, _ string, _ MessageHandler) error {
	return nil
}
