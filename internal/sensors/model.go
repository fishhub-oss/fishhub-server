package sensors

import (
	"errors"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
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
var ErrCodeNotFound            = errors.New("provisioning code not found")
var ErrCodeAlreadyUsed         = errors.New("provisioning code already used")
var ErrInvalidCommand          = peripheral.ErrInvalidCommand
var ErrInfluxWrite             = measurement.ErrInfluxWrite
var ErrPeripheralNotFound      = peripheral.ErrNotFound
var ErrPeripheralAlreadyExists = peripheral.ErrAlreadyExists
var ErrPeripheralPinInUse      = peripheral.ErrPinInUse
