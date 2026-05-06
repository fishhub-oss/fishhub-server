package device

import "errors"

var (
	ErrNotFound       = errors.New("device not found")
	ErrInvalidCommand = errors.New("command must be 'set' or 'schedule'")
)
