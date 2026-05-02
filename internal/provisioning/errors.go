package provisioning

import "errors"

var (
	ErrCodeNotFound    = errors.New("provisioning code not found")
	ErrCodeAlreadyUsed = errors.New("provisioning code already used")
)
