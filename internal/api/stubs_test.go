package api_test

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status: got %d, want %d", rec.Code, wantStatus)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != wantCode {
		t.Errorf("code: got %q, want %q", body.Code, wantCode)
	}
}

// ── DeviceStore ───────────────────────────────────────────────────────────────

type stubDeviceStore struct {
	device         device.Device
	findErr        error
	findByIDDevice device.Device
	findByIDErr    error
	listDevices    []device.Device
	listErr        error
	patchDevice    device.Device
	patchErr       error
	deleteMQTTUser string
	deleteErr      error
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _ string) ([]device.Device, error) {
	return s.listDevices, s.listErr
}
func (s *stubDeviceStore) FindByID(_ context.Context, _ string) (device.Device, error) {
	return s.findByIDDevice, s.findByIDErr
}
func (s *stubDeviceStore) FindByIDAndUserID(_ context.Context, _, _ string) (device.Device, error) {
	return s.device, s.findErr
}
func (s *stubDeviceStore) PatchDevice(_ context.Context, _, _, _ string) (device.Device, error) {
	return s.patchDevice, s.patchErr
}
func (s *stubDeviceStore) DeleteDevice(_ context.Context, _, _ string) (string, error) {
	return s.deleteMQTTUser, s.deleteErr
}
func (s *stubDeviceStore) GetActivationStatus(_ context.Context, _ string) (device.ActivationStatus, error) {
	return device.ActivationStatus{}, nil
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
	reading measurement.Reading
	err     error
}

func (s *stubReadingWriter) WriteReading(_ context.Context, r measurement.Reading) error {
	s.called = true
	s.reading = r
	return s.err
}

// ── ReadingQuerier ────────────────────────────────────────────────────────────

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
	status device.ActivationStatus
	err    error
}

func (s *stubActivationStatusStore) GetActivationStatus(_ context.Context, _ string) (device.ActivationStatus, error) {
	return s.status, s.err
}

// ── PeripheralStore ───────────────────────────────────────────────────────────

type stubPeripheralStore struct {
	created        peripheral.Peripheral
	createErr      error
	listed         []peripheral.Peripheral
	listErr        error
	scheduled      peripheral.Peripheral
	schedErr       error
	controlModeP   peripheral.Peripheral
	controlModeErr error
	deleteErr      error
}

func (s *stubPeripheralStore) CreatePeripheral(_ context.Context, _ *sql.Tx, _, _, _, _, _ string, _ int) (peripheral.Peripheral, error) {
	return s.created, s.createErr
}
func (s *stubPeripheralStore) ListPeripherals(_ context.Context, _, _ string) ([]peripheral.Peripheral, error) {
	return s.listed, s.listErr
}
func (s *stubPeripheralStore) GetPeripheral(_ context.Context, _, _, _ string) (peripheral.Peripheral, error) {
	return s.created, s.createErr
}
func (s *stubPeripheralStore) SetPeripheralSchedule(_ context.Context, _, _, _ string, _ []peripheral.ScheduleWindow) (peripheral.Peripheral, error) {
	return s.scheduled, s.schedErr
}
func (s *stubPeripheralStore) SetControlMode(_ context.Context, _ *sql.Tx, _, _, _, _ string) (peripheral.Peripheral, error) {
	return s.controlModeP, s.controlModeErr
}
func (s *stubPeripheralStore) DeletePeripheral(_ context.Context, _ *sql.Tx, _, _, _ string) (peripheral.Peripheral, error) {
	return s.created, s.deleteErr
}

// ── deviceFinderBridge ────────────────────────────────────────────────────────

type stubDeviceFinder struct {
	device device.Device
	err    error
}

func (s *stubDeviceFinder) FindByIDAndUserID(_ context.Context, _, _ string) error {
	return s.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newDevice(id string) device.Device {
	return device.Device{ID: id, Name: "Tank", CreatedAt: time.Now()}
}

var errSentinel = errors.New("store error")

// ── TriggerStore ──────────────────────────────────────────────────────────────

type stubTriggerStore struct {
	created       trigger.Trigger
	createKindPin string
	createErr     error
	listed        []trigger.Trigger
	listErr       error
	got           trigger.Trigger
	getErr        error
	updated       trigger.Trigger
	updateKindPin string
	updateErr     error
	deletedErr    error
}

func (s *stubTriggerStore) Create(_ context.Context, _ *sql.Tx, _, _ string, _ trigger.TriggerCreate) (trigger.Trigger, string, error) {
	return s.created, s.createKindPin, s.createErr
}
func (s *stubTriggerStore) List(_ context.Context, _, _ string) ([]trigger.Trigger, error) {
	return s.listed, s.listErr
}
func (s *stubTriggerStore) Get(_ context.Context, _, _, _ string) (trigger.Trigger, error) {
	return s.got, s.getErr
}
func (s *stubTriggerStore) Update(_ context.Context, _ *sql.Tx, _, _, _ string, _ trigger.TriggerUpdate) (trigger.Trigger, string, error) {
	return s.updated, s.updateKindPin, s.updateErr
}
func (s *stubTriggerStore) Delete(_ context.Context, _ *sql.Tx, _, _, _ string) (trigger.Trigger, error) {
	return s.created, s.deletedErr
}
