package sensors_test

import (
	"context"
	"crypto/rsa"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

// ── DeviceStore ───────────────────────────────────────────────────────────────

type stubDeviceStore struct {
	device         sensors.Device
	findErr        error
	listDevices    []sensors.Device
	listErr        error
	patchDevice    sensors.Device
	patchErr       error
	deleteMQTTUser string
	deleteErr      error
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _, _ string) ([]sensors.Device, error) {
	return s.listDevices, s.listErr
}
func (s *stubDeviceStore) FindByIDAndUserID(_ context.Context, _, _ string) (sensors.Device, error) {
	return s.device, s.findErr
}
func (s *stubDeviceStore) PatchDevice(_ context.Context, _, _, _ string) (sensors.Device, error) {
	return s.patchDevice, s.patchErr
}
func (s *stubDeviceStore) DeleteDevice(_ context.Context, _, _ string) (string, error) {
	return s.deleteMQTTUser, s.deleteErr
}

// ── ProvisioningStore ─────────────────────────────────────────────────────────

type stubProvisioningStore struct {
	deviceID string
	code     string
	getErr   error

	claimedDeviceID string
	claimUserID     string
	claimErr        error

	activateErr error
}

func (s *stubProvisioningStore) GetOrCreatePending(_ context.Context, _ string) (string, string, error) {
	return s.deviceID, s.code, s.getErr
}
func (s *stubProvisioningStore) ClaimCode(_ context.Context, _ string) (string, string, error) {
	uid := s.claimUserID
	if uid == "" {
		uid = "user-uuid"
	}
	return s.claimedDeviceID, uid, s.claimErr
}
func (s *stubProvisioningStore) Activate(_ context.Context, _, _, _ string) error {
	return s.activateErr
}

// ── HiveMQ ────────────────────────────────────────────────────────────────────

type stubHiveMQClient struct{ err error }

func (s *stubHiveMQClient) ProvisionDevice(_ context.Context, _, _ string) error { return s.err }
func (s *stubHiveMQClient) DeleteDevice(_ context.Context, _ string) error        { return s.err }

// ── Signer ────────────────────────────────────────────────────────────────────

type stubSigner struct {
	token string
	err   error
}

func (s *stubSigner) Sign(_, _ string) (string, error) { return s.token, s.err }
func (s *stubSigner) PublicKey() *rsa.PublicKey         { return nil }
func (s *stubSigner) KID() string                       { return "" }
func (s *stubSigner) Issuer() string                    { return "" }

// ── ReadingWriter ─────────────────────────────────────────────────────────────

type stubReadingWriter struct {
	called  bool
	reading sensors.Reading
	err     error
}

func (s *stubReadingWriter) WriteReading(_ context.Context, r sensors.Reading) error {
	s.called = true
	s.reading = r
	return s.err
}

// ── ReadingQuerier ────────────────────────────────────────────────────────────

type stubReadingQuerier struct {
	points []sensors.ReadingPoint
	err    error
}

func (s *stubReadingQuerier) QueryReadings(_ context.Context, _ sensors.ReadingQuery) ([]sensors.ReadingPoint, error) {
	return s.points, s.err
}

// ── Publisher ─────────────────────────────────────────────────────────────────

type stubPublisher struct {
	publishedTopic   string
	publishedPayload []byte
	called           bool
	err              error
}

func (s *stubPublisher) Publish(_ context.Context, topic string, payload []byte) error {
	s.publishedTopic = topic
	s.publishedPayload = payload
	s.called = true
	return s.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newDevice(id string) sensors.Device {
	return sensors.Device{ID: id, Name: "Tank", CreatedAt: time.Now()}
}
