package measurement_test

import (
	"context"
	"io"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// ── stubReadingWriter ─────────────────────────────────────────────────────────

type stubReadingWriter struct {
	called  bool
	reading measurement.Reading
	err     error
}

func (s *stubReadingWriter) WriteReading(_ context.Context, r measurement.Reading) error {
	s.called = true
	s.reading = r
	return s.err
}

// ── stubReadingQuerier ────────────────────────────────────────────────────────

type stubReadingQuerier struct {
	points []measurement.Point
	err    error
}

func (s *stubReadingQuerier) QueryReadings(_ context.Context, _ measurement.Query) ([]measurement.Point, error) {
	return s.points, s.err
}

func (s *stubReadingQuerier) QueryLastReadings(_ context.Context, _ string) (*measurement.Point, error) {
	return nil, nil
}

// ── stubDeviceFinder ──────────────────────────────────────────────────────────

type stubDeviceFinder struct {
	err error
}

func (s *stubDeviceFinder) FindByIDAndUserID(_ context.Context, _, _ string) error {
	return s.err
}

// ── stubDeviceStore ───────────────────────────────────────────────────────────

type stubDeviceStore struct {
	findByIDDevice device.Device
	findByIDErr    error
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _ string) ([]device.Device, error) {
	return nil, nil
}
func (s *stubDeviceStore) FindByID(_ context.Context, _ string) (device.Device, error) {
	return s.findByIDDevice, s.findByIDErr
}
func (s *stubDeviceStore) FindByIDAndUserID(_ context.Context, _, _ string) (device.Device, error) {
	return device.Device{}, nil
}
func (s *stubDeviceStore) PatchDevice(_ context.Context, _, _, _ string) (device.Device, error) {
	return device.Device{}, nil
}
func (s *stubDeviceStore) DeleteDevice(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (s *stubDeviceStore) GetActivationStatus(_ context.Context, _ string) (device.ActivationStatus, error) {
	return device.ActivationStatus{}, nil
}
