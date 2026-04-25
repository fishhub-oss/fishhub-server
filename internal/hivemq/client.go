package hivemq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client provisions and removes per-device MQTT credentials in HiveMQ Cloud.
type Client interface {
	ProvisionDevice(ctx context.Context, username, password string) error
	DeleteDevice(ctx context.Context, username string) error
}

type apiClient struct {
	baseURL      string
	apiToken     string
	deviceRoleID string
	http         *http.Client
}

// NewAPIClient returns a Client that calls the HiveMQ Cloud REST API.
func NewAPIClient(baseURL, apiToken, deviceRoleID string) Client {
	return &apiClient{
		baseURL:      baseURL,
		apiToken:     apiToken,
		deviceRoleID: deviceRoleID,
		http:         &http.Client{},
	}
}

func (c *apiClient) ProvisionDevice(ctx context.Context, username, password string) error {
	body, _ := json.Marshal(map[string]any{
		"credentials": map[string]string{
			"username": username,
			"password": password,
		},
	})
	if err := c.do(ctx, http.MethodPost, "/mqtt/credentials", body); err != nil {
		return fmt.Errorf("hivemq: create credential: %w", err)
	}

	attachURL := fmt.Sprintf("/user/%s/roles/%s/attach", username, c.deviceRoleID)
	if err := c.do(ctx, http.MethodPut, attachURL, nil); err != nil {
		// Roll back — delete the credential we just created
		_ = c.do(ctx, http.MethodDelete, "/mqtt/credentials/"+username, nil)
		return fmt.Errorf("hivemq: attach role: %w", err)
	}

	return nil
}

func (c *apiClient) DeleteDevice(ctx context.Context, username string) error {
	detachURL := fmt.Sprintf("/user/%s/roles/%s/detach", username, c.deviceRoleID)
	if err := c.do(ctx, http.MethodPut, detachURL, nil); err != nil {
		return fmt.Errorf("hivemq: detach role: %w", err)
	}
	if err := c.do(ctx, http.MethodDelete, "/mqtt/credentials/"+username, nil); err != nil {
		return fmt.Errorf("hivemq: delete credential: %w", err)
	}
	return nil
}

func (c *apiClient) do(ctx context.Context, method, path string, body []byte) error {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// noopClient is returned when HIVEMQ_API_BASE_URL is not configured.
type noopClient struct{}

func NewNoOp() Client                                                          { return &noopClient{} }
func (n *noopClient) ProvisionDevice(_ context.Context, _, _ string) error    { return nil }
func (n *noopClient) DeleteDevice(_ context.Context, _ string) error          { return nil }
