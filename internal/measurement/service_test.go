package measurement_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/senml"
)

func TestReadingsService_Query_HappyPath(t *testing.T) {
	now := time.Now()
	expected := []measurement.Point{{Timestamp: now, Values: map[string]any{"temperature": 25.5}}}
	svc := measurement.NewReadingsService(&stubDeviceFinder{}, &stubReadingQuerier{points: expected}, nil, discardLogger)
	points, err := svc.Query(context.Background(), "usr-1", measurement.Query{DeviceID: "dev-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(points) != 1 {
		t.Errorf("expected 1 point, got %d", len(points))
	}
}

func TestReadingsService_Query_DeviceNotOwned(t *testing.T) {
	svc := measurement.NewReadingsService(&stubDeviceFinder{err: device.ErrNotFound}, &stubReadingQuerier{}, nil, discardLogger)
	_, err := svc.Query(context.Background(), "usr-1", measurement.Query{DeviceID: "dev-1"})
	if !errors.Is(err, device.ErrNotFound) {
		t.Errorf("expected device.ErrNotFound, got %v", err)
	}
}

func TestReadingsService_Query_QuerierError(t *testing.T) {
	querierErr := errors.New("influx unavailable")
	svc := measurement.NewReadingsService(&stubDeviceFinder{}, &stubReadingQuerier{err: querierErr}, nil, discardLogger)
	_, err := svc.Query(context.Background(), "usr-1", measurement.Query{DeviceID: "dev-1"})
	if !errors.Is(err, querierErr) {
		t.Errorf("expected wrapped querierErr, got %v", err)
	}
}

func senMLPayload() []byte {
	return []byte(`[{"bn":"dev-1","bt":1700000000},{"n":"temperature","v":25.5}]`)
}

func TestReadingsService_Write_HappyPath(t *testing.T) {
	svc := measurement.NewReadingsService(nil, nil, &stubReadingWriter{}, discardLogger)
	if err := svc.Write(context.Background(), "dev-1", "usr-1", senMLPayload()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadingsService_Write_NilWriter(t *testing.T) {
	svc := measurement.NewReadingsService(nil, nil, nil, discardLogger)
	if err := svc.Write(context.Background(), "dev-1", "usr-1", senMLPayload()); err != nil {
		t.Fatalf("nil writer should be a no-op, got: %v", err)
	}
}

func TestReadingsService_Write_ParseError(t *testing.T) {
	svc := measurement.NewReadingsService(nil, nil, &stubReadingWriter{}, discardLogger)
	err := svc.Write(context.Background(), "dev-1", "usr-1", []byte(`not json`))
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestReadingsService_Write_EmptyPayload(t *testing.T) {
	svc := measurement.NewReadingsService(nil, nil, &stubReadingWriter{}, discardLogger)
	err := svc.Write(context.Background(), "dev-1", "usr-1", []byte(`[{"bn":"dev-1","bt":1700000000}]`))
	if !errors.Is(err, senml.ErrEmptyPayload) {
		t.Errorf("expected ErrEmptyPayload, got %v", err)
	}
}

func TestReadingsService_Write_WriterError(t *testing.T) {
	writeErr := errors.New("influx write failed")
	svc := measurement.NewReadingsService(nil, nil, &stubReadingWriter{err: writeErr}, discardLogger)
	err := svc.Write(context.Background(), "dev-1", "usr-1", senMLPayload())
	if !errors.Is(err, measurement.ErrInfluxWrite) {
		t.Errorf("expected ErrInfluxWrite, got %v", err)
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("expected wrapped writeErr, got %v", err)
	}
}
