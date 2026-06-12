package measurement_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testToken    = "apiv3_test-admin-token"
	testDatabase = "test_readings"
)

func writeTokenFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "admin-token.json")
	content := fmt.Sprintf(`{"token":%q,"name":"admin"}`, testToken)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

func startInfluxDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	tokenFile := writeTokenFile(t)

	req := testcontainers.ContainerRequest{
		Image:        "influxdb:3-core",
		ExposedPorts: []string{"8181/tcp"},
		Cmd: []string{
			"influxdb3", "serve",
			"--node-id=test-node",
			"--object-store=memory",
			"--admin-token-file=/etc/influxdb3/admin-token.json",
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      tokenFile,
				ContainerFilePath: "/etc/influxdb3/admin-token.json",
				FileMode:          0644,
			},
		},
		WaitingFor: wait.ForHTTP("/health").
			WithPort("8181/tcp").
			WithHeaders(map[string]string{"Authorization": "Bearer " + testToken}).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start influxdb container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8181")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func createDatabase(t *testing.T, host string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v3/configure/database", host)
	body := fmt.Sprintf(`{"db":"%s"}`, testDatabase)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build create-db request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create database: unexpected status %d", resp.StatusCode)
	}
}

func TestWriteReading_Integration(t *testing.T) {
	host := startInfluxDB(t)
	createDatabase(t, host)

	writer, err := measurement.NewInfluxClient(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	ts := time.Unix(1713000000, 0).UTC()
	err = writer.WriteReading(context.Background(), measurement.Reading{
		DeviceID:  "test-device",
		UserID:    "test-user",
		Timestamp: ts,
		Measurements: map[string]any{
			"temperature": float64(23.4),
			"ph":          float64(7.2),
		},
	})
	if err != nil {
		t.Fatalf("write reading: %v", err)
	}

	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     host,
		Token:    testToken,
		Database: testDatabase,
	})
	if err != nil {
		t.Fatalf("query client: %v", err)
	}
	defer client.Close()

	iter, err := client.Query(context.Background(),
		"SELECT device_id, user_id, temperature, ph FROM sensors LIMIT 1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if !iter.Next() {
		t.Fatal("expected one row, got none")
	}
	row := iter.Value()

	if row["device_id"] != "test-device" {
		t.Errorf("expected device_id 'test-device', got %v", row["device_id"])
	}
	if row["user_id"] != "test-user" {
		t.Errorf("expected user_id 'test-user', got %v", row["user_id"])
	}
	if v, ok := row["temperature"].(float64); !ok || v != 23.4 {
		t.Errorf("expected temperature 23.4, got %v", row["temperature"])
	}
	if v, ok := row["ph"].(float64); !ok || v != 7.2 {
		t.Errorf("expected ph 7.2, got %v", row["ph"])
	}
}

func TestWriteAndQueryStringField_Integration(t *testing.T) {
	host := startInfluxDB(t)
	createDatabase(t, host)

	client, err := measurement.NewInfluxClient(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ts := time.Unix(1713000001, 0).UTC()
	err = client.WriteReading(context.Background(), measurement.Reading{
		DeviceID:  "string-test-device",
		UserID:    "test-user",
		Timestamp: ts,
		Measurements: map[string]any{
			"light/source": "schedule",
			"temperature":  float64(24.0),
		},
	})
	if err != nil {
		t.Fatalf("write reading: %v", err)
	}

	points, err := client.QueryReadings(context.Background(), measurement.Query{
		DeviceID: "string-test-device",
		From:     ts.Add(-time.Minute),
		To:       ts.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("query readings: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least one reading, got none")
	}
	if v, ok := points[0].Values["light/source"].(string); !ok || v != "schedule" {
		t.Errorf("expected light/source 'schedule', got %v", points[0].Values["light/source"])
	}
	if points[0].Values["temperature"] != float64(24.0) {
		t.Errorf("expected temperature 24.0, got %v", points[0].Values["temperature"])
	}
}

func TestQueryLastReadings_Integration(t *testing.T) {
	host := startInfluxDB(t)
	createDatabase(t, host)

	client, err := measurement.NewInfluxClient(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()
	const device = "last-readings-device"

	// Timestamps relative to now so the 30-min recent window covers them.
	now := time.Now().UTC().Truncate(time.Second)
	writeAt := func(ts time.Time, fields map[string]any) {
		t.Helper()
		if err := client.WriteReading(ctx, measurement.Reading{
			DeviceID:     device,
			UserID:       "test-user",
			Timestamp:    ts,
			Measurements: fields,
		}); err != nil {
			t.Fatalf("write reading: %v", err)
		}
	}

	// temperature written twice — newer value must win.
	writeAt(now.Add(-10*time.Minute), map[string]any{"temperature": float64(20.0)})
	writeAt(now.Add(-5*time.Minute), map[string]any{"ph": float64(7.2)})
	writeAt(now.Add(-2*time.Minute), map[string]any{"temperature": float64(23.4)})

	p, err := client.QueryLastReadings(ctx, device)
	if err != nil {
		t.Fatalf("query last readings: %v", err)
	}
	if p == nil {
		t.Fatal("expected a point, got nil")
	}
	if p.Values["temperature"] != float64(23.4) {
		t.Errorf("temperature: want 23.4 (latest), got %v", p.Values["temperature"])
	}
	if p.Values["ph"] != float64(7.2) {
		t.Errorf("ph: want 7.2, got %v", p.Values["ph"])
	}
	// Timestamp is the newest row's timestamp.
	if !p.Timestamp.Equal(now.Add(-2 * time.Minute)) {
		t.Errorf("timestamp: want %v, got %v", now.Add(-2*time.Minute), p.Timestamp)
	}
}

func TestQueryLastReadings_WideFallback_Integration(t *testing.T) {
	host := startInfluxDB(t)
	createDatabase(t, host)

	client, err := measurement.NewInfluxClient(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()
	const device = "fallback-device"

	// Only data is older than the 30-min recent window but within the 7-day window —
	// must be found via the wide-window fallback pass.
	now := time.Now().UTC().Truncate(time.Second)
	if err := client.WriteReading(ctx, measurement.Reading{
		DeviceID:     device,
		UserID:       "test-user",
		Timestamp:    now.Add(-2 * time.Hour),
		Measurements: map[string]any{"temperature": float64(18.5)},
	}); err != nil {
		t.Fatalf("write reading: %v", err)
	}

	p, err := client.QueryLastReadings(ctx, device)
	if err != nil {
		t.Fatalf("query last readings: %v", err)
	}
	if p == nil {
		t.Fatal("expected a point from the wide-window fallback, got nil")
	}
	if p.Values["temperature"] != float64(18.5) {
		t.Errorf("temperature: want 18.5, got %v", p.Values["temperature"])
	}
}

func TestQueryLastReadings_NoData_Integration(t *testing.T) {
	host := startInfluxDB(t)
	createDatabase(t, host)

	client, err := measurement.NewInfluxClient(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()

	// Seed the sensors table with an unrelated device so the table exists — mirrors
	// production, where the table is always present and "no data" means no rows for
	// this particular device (not a missing table).
	if err := client.WriteReading(ctx, measurement.Reading{
		DeviceID:     "some-other-device",
		UserID:       "test-user",
		Timestamp:    time.Now().UTC(),
		Measurements: map[string]any{"temperature": float64(21.0)},
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	p, err := client.QueryLastReadings(ctx, "device-with-no-data")
	if err != nil {
		t.Fatalf("query last readings: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil for device with no data, got %+v", p)
	}
}

func TestQueryReadings_Integration(t *testing.T) {
	host := startInfluxDB(t)
	createDatabase(t, host)

	client, err := measurement.NewInfluxClient(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ts := time.Unix(1713000000, 0).UTC()
	err = client.WriteReading(context.Background(), measurement.Reading{
		DeviceID:  "query-test-device",
		UserID:    "test-user",
		Timestamp: ts,
		Measurements: map[string]any{
			"temperature": float64(22.5),
		},
	})
	if err != nil {
		t.Fatalf("write reading: %v", err)
	}

	points, err := client.QueryReadings(context.Background(), measurement.Query{
		DeviceID: "query-test-device",
		From:     ts.Add(-time.Minute),
		To:       ts.Add(time.Minute),
		Window:   "1m",
	})
	if err != nil {
		t.Fatalf("query readings: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least one reading, got none")
	}
	if points[0].Values["temperature"] != 22.5 {
		t.Errorf("expected temperature 22.5, got %v", points[0].Values["temperature"])
	}
}
