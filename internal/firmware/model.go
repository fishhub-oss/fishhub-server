package firmware

import (
	"errors"
	"time"
)

// Release is the latest available firmware version, resolved from GitHub + bucket manifest.
type Release struct {
	Version   string
	SHA256    string
	ObjectKey string // e.g. "firmware/1.2.3/firmware.bin"
}

// Manifest is the parsed content of firmware/<version>/manifest.json.
type Manifest struct {
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	ObjectKey string `json:"object_key"`
}

// UpdateRecord is a row from device_firmware_updates.
type UpdateRecord struct {
	ID             string
	DeviceID       string
	DesiredVersion string
	Nonce          string
	Status         string // "pending" | "succeeded" | "failed"
	LastError      string
	RequestedAt    time.Time
	UpdatedAt      time.Time
}

// UpdateStatus is the computed state returned to API callers.
type UpdateStatus string

const (
	UpdateStatusIdle      UpdateStatus = "idle"
	UpdateStatusPending   UpdateStatus = "pending"
	UpdateStatusSucceeded UpdateStatus = "succeeded"
	UpdateStatusFailed    UpdateStatus = "failed"
	UpdateStatusTimeout   UpdateStatus = "timeout"
)

var (
	ErrNoRelease           = errors.New("firmware: no release available")
	ErrUpdateAlreadyPending = errors.New("firmware: update already pending for this device")
	ErrNoPendingUpdate     = errors.New("firmware: no failed or timed-out update to retry")
	ErrDeviceNotFound      = errors.New("firmware: device not found")
	ErrRecordNotFound      = errors.New("firmware: update record not found")
)
