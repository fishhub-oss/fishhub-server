package sensors

import (
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"log/slog"
)

// DeviceService is an alias for device.Service kept for backward compatibility.
type DeviceService = device.Service

func NewDeviceService(store DeviceStore, hiveMQ hivemq.Client, publisher mqtt.Publisher, logger *slog.Logger) *DeviceService {
	return device.NewService(store, hiveMQ, publisher, logger)
}
