package sensors

import (
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

// DeviceStore is an alias for device.Store kept for backward compatibility within this package.
type DeviceStore = device.Store

// PeripheralStore is an alias for peripheral.Store kept for backward compatibility within this package.
type PeripheralStore = peripheral.Store

// ProvisioningStore is an alias for provisioning.Store kept for backward compatibility within this package.
type ProvisioningStore = provisioning.Store
