package sensors_test

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"errors"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

// ── DeviceStore ───────────────────────────────────────────────────────────────

type stubDeviceStore struct {
	device         sensors.Device
	findErr        error
	findByIDDevice sensors.Device
	findByIDErr    error
	listDevices    []sensors.Device
	listErr        error
	patchDevice    sensors.Device
	patchErr       error
	deleteMQTTUser string
	deleteErr      error
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _ string) ([]sensors.Device, error) {
	return s.listDevices, s.listErr
}
func (s *stubDeviceStore) FindByID(_ context.Context, _ string) (sensors.Device, error) {
	return s.findByIDDevice, s.findByIDErr
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
func (s *stubDeviceStore) GetActivationStatus(_ context.Context, _ string) (sensors.ActivationStatus, error) {
	return sensors.ActivationStatus{}, nil
}

// ── ProvisioningStore ─────────────────────────────────────────────────────────

type stubProvisioningStore struct {
	code   string
	getErr error

	claimedDeviceID string
	claimUserID     string
	claimErr        error

	activateErr error
}

func (s *stubProvisioningStore) GetOrCreateCode(_ context.Context, _ string) (string, error) {
	return s.code, s.getErr
}
func (s *stubProvisioningStore) ClaimCode(_ context.Context, _ string) (string, string, error) {
	uid := s.claimUserID
	if uid == "" {
		uid = "user-uuid"
	}
	return s.claimedDeviceID, uid, s.claimErr
}
func (s *stubProvisioningStore) Activate(_ context.Context, _ *sql.Tx, _, _, _ string) error {
	return s.activateErr
}

// ── OutboxStore ───────────────────────────────────────────────────────────────

type stubOutboxStore struct {
	insertErr error
}

func (s *stubOutboxStore) ClaimBatch(_ context.Context, _ int) ([]outbox.Event, error) {
	return nil, nil
}
func (s *stubOutboxStore) MarkCompleted(_ context.Context, _ string) error { return nil }
func (s *stubOutboxStore) RecordFailure(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}
func (s *stubOutboxStore) Insert(_ context.Context, _ *sql.Tx, _ string, _ any, _ int) error {
	return s.insertErr
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

func (s *stubPublisher) PublishRetained(_ context.Context, topic string, payload []byte) error {
	s.publishedTopic = topic
	s.publishedPayload = payload
	s.called = true
	return s.err
}

// ── ActivationStatusStore ─────────────────────────────────────────────────────

type stubActivationStatusStore struct {
	stubDeviceStore
	status sensors.ActivationStatus
	err    error
}

func (s *stubActivationStatusStore) GetActivationStatus(_ context.Context, _ string) (sensors.ActivationStatus, error) {
	return s.status, s.err
}

// ── PeripheralStore ───────────────────────────────────────────────────────────

type stubPeripheralStore struct {
	created    sensors.Peripheral
	createErr  error
	listed     []sensors.Peripheral
	listErr    error
	scheduled  sensors.Peripheral
	schedErr   error
	deleteErr  error
}

func (s *stubPeripheralStore) CreatePeripheral(_ context.Context, _ *sql.Tx, _, _, _, _ string, _ int) (sensors.Peripheral, error) {
	return s.created, s.createErr
}
func (s *stubPeripheralStore) ListPeripherals(_ context.Context, _, _ string) ([]sensors.Peripheral, error) {
	return s.listed, s.listErr
}
func (s *stubPeripheralStore) SetPeripheralSchedule(_ context.Context, _, _, _ string, _ []sensors.ScheduleWindow) (sensors.Peripheral, error) {
	return s.scheduled, s.schedErr
}
func (s *stubPeripheralStore) DeletePeripheral(_ context.Context, _ *sql.Tx, _, _, _ string) error {
	return s.deleteErr
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newDevice(id string) sensors.Device {
	return sensors.Device{ID: id, Name: "Tank", CreatedAt: time.Now()}
}

var errSentinel = errors.New("store error")
