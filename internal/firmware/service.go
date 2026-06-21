package firmware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/google/uuid"
)

// DeviceOwnerChecker verifies that a device belongs to a user.
type DeviceOwnerChecker interface {
	CheckOwnership(ctx context.Context, deviceID, userID string) error
}

// DeviceFirmwareStatus is the response shape for GET /api/devices/{id}/firmware.
type DeviceFirmwareStatus struct {
	ReportedVersion string       `json:"reported_version"`
	LatestVersion   string       `json:"latest_version"`
	UpdateStatus    UpdateStatus `json:"update_status"`
	LastError       string       `json:"last_error,omitempty"`
}

type Service struct {
	releases      ReleaseSource
	updates       DeviceFirmwareStore
	presigner     URLPresigner
	publisher     mqtt.Publisher
	deviceChecker DeviceOwnerChecker
	presignExpiry time.Duration
	updateTimeout time.Duration
	logger        *slog.Logger
}

func NewService(
	releases ReleaseSource,
	updates DeviceFirmwareStore,
	presigner URLPresigner,
	publisher mqtt.Publisher,
	deviceChecker DeviceOwnerChecker,
	presignExpiry time.Duration,
	updateTimeout time.Duration,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		releases:      releases,
		updates:       updates,
		presigner:     presigner,
		publisher:     publisher,
		deviceChecker: deviceChecker,
		presignExpiry: presignExpiry,
		updateTimeout: updateTimeout,
		logger:        logger,
	}
}

func (s *Service) GetStatus(ctx context.Context, deviceID, userID string) (DeviceFirmwareStatus, error) {
	if err := s.deviceChecker.CheckOwnership(ctx, deviceID, userID); err != nil {
		return DeviceFirmwareStatus{}, err
	}

	reportedVersion, err := s.updates.GetFirmwareVersion(ctx, deviceID)
	if err != nil {
		return DeviceFirmwareStatus{}, fmt.Errorf("get firmware version: %w", err)
	}

	var latestVersion string
	if r := s.releases.LatestRelease(); r != nil {
		latestVersion = r.Version
	}

	record, err := s.updates.LatestRecord(ctx, deviceID)
	if errors.Is(err, ErrRecordNotFound) {
		return DeviceFirmwareStatus{
			ReportedVersion: reportedVersion,
			LatestVersion:   latestVersion,
			UpdateStatus:    UpdateStatusIdle,
		}, nil
	}
	if err != nil {
		return DeviceFirmwareStatus{}, fmt.Errorf("latest record: %w", err)
	}

	status := s.computeStatus(record)
	return DeviceFirmwareStatus{
		ReportedVersion: reportedVersion,
		LatestVersion:   latestVersion,
		UpdateStatus:    status,
		LastError:       record.LastError,
	}, nil
}

func (s *Service) ConfirmUpdate(ctx context.Context, deviceID, userID string) error {
	if err := s.deviceChecker.CheckOwnership(ctx, deviceID, userID); err != nil {
		return err
	}

	release := s.releases.LatestRelease()
	if release == nil {
		return ErrNoRelease
	}

	pending, err := s.updates.HasPending(ctx, deviceID, release.Version)
	if err != nil {
		return fmt.Errorf("check pending: %w", err)
	}
	if pending {
		return ErrUpdateAlreadyPending
	}

	nonce, err := newNonce()
	if err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	url, err := s.presigner.PresignDownloadURL(ctx, release.ObjectKey, s.presignExpiry)
	if err != nil {
		return fmt.Errorf("presign url: %w", err)
	}

	if err := s.publishManifest(ctx, deviceID, release, url, nonce); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}

	if err := s.updates.InsertPending(ctx, deviceID, release.Version, nonce); err != nil {
		return fmt.Errorf("insert pending: %w", err)
	}

	s.logger.Info("firmware: update confirmed", "device_id", deviceID, "version", release.Version)
	return nil
}

func (s *Service) RetryUpdate(ctx context.Context, deviceID, userID string) error {
	if err := s.deviceChecker.CheckOwnership(ctx, deviceID, userID); err != nil {
		return err
	}

	release := s.releases.LatestRelease()
	if release == nil {
		return ErrNoRelease
	}

	record, err := s.updates.LatestRecord(ctx, deviceID)
	if errors.Is(err, ErrRecordNotFound) {
		return ErrNoPendingUpdate
	}
	if err != nil {
		return fmt.Errorf("latest record: %w", err)
	}

	// Allow retry only on failed or timed-out records.
	status := s.computeStatus(record)
	if status != UpdateStatusFailed && status != UpdateStatusTimeout {
		return ErrNoPendingUpdate
	}

	nonce, err := newNonce()
	if err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	url, err := s.presigner.PresignDownloadURL(ctx, release.ObjectKey, s.presignExpiry)
	if err != nil {
		return fmt.Errorf("presign url: %w", err)
	}

	if err := s.publishManifest(ctx, deviceID, release, url, nonce); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}

	if err := s.updates.ResetPending(ctx, deviceID, nonce); err != nil {
		return fmt.Errorf("reset pending: %w", err)
	}

	s.logger.Info("firmware: update retry triggered", "device_id", deviceID, "version", release.Version)
	return nil
}

// IngestStatus is called by the MQTT handler for each fishhub/+/status message.
func (s *Service) IngestStatus(ctx context.Context, deviceID, version, lastUpdateResult string) error {
	if err := s.updates.SetFirmwareVersion(ctx, deviceID, version); err != nil {
		return fmt.Errorf("set firmware version: %w", err)
	}

	switch {
	case lastUpdateResult == "ok":
		if err := s.updates.MarkSucceeded(ctx, deviceID, version); err != nil {
			s.logger.Warn("firmware: mark succeeded", "device_id", deviceID, "error", err)
		}
	case strings.HasPrefix(lastUpdateResult, "failed:"):
		reason := strings.TrimPrefix(lastUpdateResult, "failed:")
		if err := s.updates.MarkFailed(ctx, deviceID, reason); err != nil {
			s.logger.Warn("firmware: mark failed", "device_id", deviceID, "error", err)
		}
	}

	return nil
}

func (s *Service) computeStatus(r UpdateRecord) UpdateStatus {
	switch r.Status {
	case "succeeded":
		return UpdateStatusSucceeded
	case "failed":
		return UpdateStatusFailed
	case "pending":
		if time.Since(r.RequestedAt) > s.updateTimeout {
			return UpdateStatusTimeout
		}
		return UpdateStatusPending
	}
	return UpdateStatusIdle
}

func (s *Service) publishManifest(ctx context.Context, deviceID string, release *Release, url, nonce string) error {
	payload, err := json.Marshal(struct {
		Version string `json:"version"`
		URL     string `json:"url"`
		SHA256  string `json:"sha256"`
		Nonce   string `json:"nonce"`
	}{
		Version: release.Version,
		URL:     url,
		SHA256:  release.SHA256,
		Nonce:   nonce,
	})
	if err != nil {
		return err
	}
	topic := fmt.Sprintf("fishhub/%s/firmware", deviceID)
	return s.publisher.PublishRetained(ctx, topic, payload)
}

func newNonce() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
