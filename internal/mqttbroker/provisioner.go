package mqttbroker

import "context"

// Provisioner manages per-device MQTT credentials on a broker.
type Provisioner interface {
	ProvisionDevice(ctx context.Context, username, password string) error
	DeleteDevice(ctx context.Context, username string) error
}
