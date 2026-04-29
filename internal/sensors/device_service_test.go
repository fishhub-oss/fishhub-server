package sensors_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func TestDeviceService_Delete_HappyPath(t *testing.T) {
	pub := &stubPublisher{}
	svc := sensors.NewDeviceService(&stubDeviceStore{deleteMQTTUser: "dev-1"}, &stubHiveMQClient{}, pub, discardLogger)
	if err := svc.Delete(context.Background(), "dev-1", "usr-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeviceService_Delete_NotFound(t *testing.T) {
	svc := sensors.NewDeviceService(&stubDeviceStore{deleteErr: sensors.ErrDeviceNotFound}, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	err := svc.Delete(context.Background(), "dev-1", "usr-1")
	if !errors.Is(err, sensors.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestDeviceService_Delete_HiveMQErrorIsLogged(t *testing.T) {
	svc := sensors.NewDeviceService(&stubDeviceStore{deleteMQTTUser: "dev-1"}, &stubHiveMQClient{err: errors.New("hivemq down")}, &stubPublisher{}, discardLogger)
	if err := svc.Delete(context.Background(), "dev-1", "usr-1"); err != nil {
		t.Fatalf("expected nil error (HiveMQ errors are non-fatal), got %v", err)
	}
}

func newPeripheralSvcForCommand(t *testing.T, pStore *stubPeripheralStore, pub *stubPublisher) *sensors.PeripheralService {
	t.Helper()
	return sensors.NewPeripheralService(testutil.NewTestDB(t), pStore, &stubOutboxStore{}, pub, discardLogger)
}

func TestPeripheralService_SendCommand_HappyPath(t *testing.T) {
	pub := &stubPublisher{}
	relay := sensors.Peripheral{ID: "p-1", DeviceID: "dev-1", Kind: "relay", Pin: 5}
	svc := newPeripheralSvcForCommand(t, &stubPeripheralStore{created: relay}, pub)
	body := []byte(`{"action":"set","value":1}`)
	if err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "p-1", body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.publishedTopic != "fishhub/dev-1/commands/relay-5" {
		t.Errorf("topic: got %q", pub.publishedTopic)
	}
}

func TestPeripheralService_SendCommand_NotFound(t *testing.T) {
	svc := newPeripheralSvcForCommand(t, &stubPeripheralStore{createErr: sensors.ErrPeripheralNotFound}, &stubPublisher{})
	err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "p-1", []byte(`{"action":"set","value":1}`))
	if !errors.Is(err, sensors.ErrPeripheralNotFound) {
		t.Errorf("expected ErrPeripheralNotFound, got %v", err)
	}
}

func TestPeripheralService_SendCommand_InvalidAction(t *testing.T) {
	relay := sensors.Peripheral{ID: "p-1", DeviceID: "dev-1", Kind: "relay", Pin: 5}
	svc := newPeripheralSvcForCommand(t, &stubPeripheralStore{created: relay}, &stubPublisher{})
	err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "p-1", []byte(`{"action":"delete"}`))
	if !errors.Is(err, sensors.ErrInvalidCommand) {
		t.Errorf("expected ErrInvalidCommand, got %v", err)
	}
}

func TestPeripheralService_SendCommand_PublishError(t *testing.T) {
	publishErr := errors.New("broker unreachable")
	relay := sensors.Peripheral{ID: "p-1", DeviceID: "dev-1", Kind: "relay", Pin: 5}
	svc := newPeripheralSvcForCommand(t, &stubPeripheralStore{created: relay}, &stubPublisher{err: publishErr})
	err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "p-1", []byte(`{"action":"set","value":1}`))
	if !errors.Is(err, publishErr) {
		t.Errorf("expected wrapped publishErr, got %v", err)
	}
}

// ── List tests ────────────────────────────────────────────────────────────────

func TestDeviceService_List_HappyPath(t *testing.T) {
	devices := []sensors.Device{
		{ID: "dev-1", Name: "Tank", CreatedAt: time.Now()},
	}
	svc := sensors.NewDeviceService(&stubDeviceStore{listDevices: devices}, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	got, err := svc.List(context.Background(), "usr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "dev-1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ── Patch tests ───────────────────────────────────────────────────────────────

func TestDeviceService_Patch_HappyPath(t *testing.T) {
	updated := sensors.Device{ID: "dev-1", Name: "Tank A", CreatedAt: time.Now()}
	svc := sensors.NewDeviceService(&stubDeviceStore{patchDevice: updated}, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	got, err := svc.Patch(context.Background(), "dev-1", "usr-1", "Tank A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Tank A" {
		t.Errorf("expected name 'Tank A', got %q", got.Name)
	}
}

func TestDeviceService_Patch_NotFound(t *testing.T) {
	svc := sensors.NewDeviceService(&stubDeviceStore{patchErr: sensors.ErrDeviceNotFound}, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	_, err := svc.Patch(context.Background(), "dev-x", "usr-1", "Tank A")
	if !errors.Is(err, sensors.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}
