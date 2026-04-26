package sensors_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func TestReadingsService_Query_HappyPath(t *testing.T) {
	now := time.Now()
	expected := []sensors.ReadingPoint{{Timestamp: now, Values: map[string]float64{"temperature": 25.5}}}
	svc := &sensors.ReadingsService{
		Devices: &stubDeviceStore{},
		Querier: &stubReadingQuerier{points: expected},
	}
	points, err := svc.Query(context.Background(), "usr-1", sensors.ReadingQuery{DeviceID: "dev-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(points) != 1 {
		t.Errorf("expected 1 point, got %d", len(points))
	}
}

func TestReadingsService_Query_DeviceNotOwned(t *testing.T) {
	svc := &sensors.ReadingsService{
		Devices: &stubDeviceStore{findErr: sensors.ErrDeviceNotFound},
		Querier: &stubReadingQuerier{},
	}
	_, err := svc.Query(context.Background(), "usr-1", sensors.ReadingQuery{DeviceID: "dev-1"})
	if !errors.Is(err, sensors.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestReadingsService_Query_QuerierError(t *testing.T) {
	querierErr := errors.New("influx unavailable")
	svc := &sensors.ReadingsService{
		Devices: &stubDeviceStore{},
		Querier: &stubReadingQuerier{err: querierErr},
	}
	_, err := svc.Query(context.Background(), "usr-1", sensors.ReadingQuery{DeviceID: "dev-1"})
	if !errors.Is(err, querierErr) {
		t.Errorf("expected wrapped querierErr, got %v", err)
	}
}

func senMLPayload() []byte {
	return []byte(`[{"bn":"dev-1","bt":1700000000},{"n":"temperature","v":25.5}]`)
}

func TestReadingsService_Write_HappyPath(t *testing.T) {
	svc := &sensors.ReadingsService{
		Writer: &stubReadingWriter{},
	}
	device := sensors.DeviceInfo{DeviceID: "dev-1", UserID: "usr-1"}
	if err := svc.Write(context.Background(), device, senMLPayload()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadingsService_Write_NilWriter(t *testing.T) {
	svc := &sensors.ReadingsService{Writer: nil}
	device := sensors.DeviceInfo{DeviceID: "dev-1", UserID: "usr-1"}
	if err := svc.Write(context.Background(), device, senMLPayload()); err != nil {
		t.Fatalf("nil writer should be a no-op, got: %v", err)
	}
}

func TestReadingsService_Write_ParseError(t *testing.T) {
	svc := &sensors.ReadingsService{Writer: &stubReadingWriter{}}
	device := sensors.DeviceInfo{DeviceID: "dev-1", UserID: "usr-1"}
	err := svc.Write(context.Background(), device, []byte(`not json`))
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestReadingsService_Write_EmptyPayload(t *testing.T) {
	svc := &sensors.ReadingsService{Writer: &stubReadingWriter{}}
	device := sensors.DeviceInfo{DeviceID: "dev-1", UserID: "usr-1"}
	err := svc.Write(context.Background(), device, []byte(`[{"bn":"dev-1","bt":1700000000}]`))
	if !errors.Is(err, sensors.ErrEmptyPayload) {
		t.Errorf("expected ErrEmptyPayload, got %v", err)
	}
}

func TestReadingsService_Write_WriterError(t *testing.T) {
	writeErr := errors.New("influx write failed")
	svc := &sensors.ReadingsService{Writer: &stubReadingWriter{err: writeErr}}
	device := sensors.DeviceInfo{DeviceID: "dev-1", UserID: "usr-1"}
	err := svc.Write(context.Background(), device, senMLPayload())
	if !errors.Is(err, sensors.ErrInfluxWrite) {
		t.Errorf("expected ErrInfluxWrite, got %v", err)
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("expected wrapped writeErr, got %v", err)
	}
}
