package device

import "errors"

var (
	ErrNotFound       = errors.New("device not found")
	ErrInvalidCommand = errors.New("action must be 'set' or 'schedule'")
)
