package peripheral

import "errors"

var (
	ErrNotAnActuator  = errors.New("peripheral is not an actuator")
	ErrNotFound       = errors.New("peripheral not found")
	ErrAlreadyExists  = errors.New("peripheral already exists")
	ErrPinInUse       = errors.New("peripheral pin already in use")
	ErrInvalidCommand = errors.New("command must be 'set' or 'schedule'")
)
