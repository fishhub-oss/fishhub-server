package sensors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func TestDeviceService_Delete_HappyPath(t *testing.T) {
	pub := &stubPublisher{}
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{deleteMQTTUser: "dev-1"},
		HiveMQ:    &stubHiveMQClient{},
		Publisher: pub,
	}
	if err := svc.Delete(context.Background(), "dev-1", "usr-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeviceService_Delete_NotFound(t *testing.T) {
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{deleteErr: sensors.ErrDeviceNotFound},
		HiveMQ:    &stubHiveMQClient{},
		Publisher: &stubPublisher{},
	}
	err := svc.Delete(context.Background(), "dev-1", "usr-1")
	if !errors.Is(err, sensors.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestDeviceService_Delete_HiveMQErrorIsLogged(t *testing.T) {
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{deleteMQTTUser: "dev-1"},
		HiveMQ:    &stubHiveMQClient{err: errors.New("hivemq down")},
		Publisher: &stubPublisher{},
	}
	if err := svc.Delete(context.Background(), "dev-1", "usr-1"); err != nil {
		t.Fatalf("expected nil error (HiveMQ errors are non-fatal), got %v", err)
	}
}

func TestDeviceService_SendCommand_HappyPath(t *testing.T) {
	pub := &stubPublisher{}
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{},
		HiveMQ:    &stubHiveMQClient{},
		Publisher: pub,
	}
	body := []byte(`{"action":"set","state":true}`)
	if err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "light", body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.publishedTopic != "fishhub/dev-1/commands/light" {
		t.Errorf("topic: got %q", pub.publishedTopic)
	}
}

func TestDeviceService_SendCommand_NotFound(t *testing.T) {
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{findErr: sensors.ErrDeviceNotFound},
		HiveMQ:    &stubHiveMQClient{},
		Publisher: &stubPublisher{},
	}
	err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "light", []byte(`{"action":"set"}`))
	if !errors.Is(err, sensors.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestDeviceService_SendCommand_InvalidAction(t *testing.T) {
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{},
		HiveMQ:    &stubHiveMQClient{},
		Publisher: &stubPublisher{},
	}
	err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "light", []byte(`{"action":"delete"}`))
	if !errors.Is(err, sensors.ErrInvalidCommand) {
		t.Errorf("expected ErrInvalidCommand, got %v", err)
	}
}

func TestDeviceService_SendCommand_PublishError(t *testing.T) {
	publishErr := errors.New("broker unreachable")
	svc := &sensors.DeviceService{
		Store:     &stubDeviceStore{},
		HiveMQ:    &stubHiveMQClient{},
		Publisher: &stubPublisher{err: publishErr},
	}
	err := svc.SendCommand(context.Background(), "dev-1", "usr-1", "light", []byte(`{"action":"set"}`))
	if !errors.Is(err, publishErr) {
		t.Errorf("expected wrapped publishErr, got %v", err)
	}
}
