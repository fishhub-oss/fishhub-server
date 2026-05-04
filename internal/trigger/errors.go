package trigger

import "errors"

var (
	ErrNotFound          = errors.New("trigger not found")
	ErrInvalidPeripheral = errors.New("target_peripheral_id must refer to an actuator peripheral owned by the device")
)
