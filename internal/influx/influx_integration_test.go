package influx_test

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
	"github.com/fishhub-oss/fishhub-server/internal/influx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testToken    = "apiv3_test-admin-token"
	testDatabase = "test_readings"
)

// writeTokenFile writes a token JSON file to a temp dir and returns its path.
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

	writer, err := influx.NewReadingWriter(host, testToken, testDatabase)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	ts := time.Unix(1713000000, 0).UTC()
	err = writer.WriteReading(context.Background(), influx.Reading{
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
