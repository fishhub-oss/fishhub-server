package sensors

import (
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

// Peripheral types aliased from internal/peripheral for backward compat.
type Peripheral = peripheral.Peripheral
type ScheduleWindow = peripheral.ScheduleWindow

// Reading types aliased from internal/measurement for backward compat.
type ReadingPoint = measurement.Point
type Reading = measurement.Reading
type ReadingQuery = measurement.Query
type ReadingWriter = measurement.Writer
type ReadingQuerier = measurement.Querier

// Error sentinels.
var ErrNotAnActuator           = peripheral.ErrNotAnActuator
var ErrDeviceNotFound          = device.ErrNotFound
var ErrCodeNotFound            = provisioning.ErrCodeNotFound
var ErrCodeAlreadyUsed         = provisioning.ErrCodeAlreadyUsed
var ErrInvalidCommand          = peripheral.ErrInvalidCommand
var ErrInfluxWrite             = measurement.ErrInfluxWrite
var ErrPeripheralNotFound      = peripheral.ErrNotFound
var ErrPeripheralAlreadyExists = peripheral.ErrAlreadyExists
var ErrPeripheralPinInUse      = peripheral.ErrPinInUse
