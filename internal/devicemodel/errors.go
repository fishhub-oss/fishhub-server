package devicemodel

import "errors"

var (
	ErrNotFound     = errors.New("device model not found")
	ErrPortNotFound = errors.New("port not found")
)
