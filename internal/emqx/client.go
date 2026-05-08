package emqx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/mqttbroker"
)

var _ mqttbroker.Provisioner = (*apiClient)(nil)
var _ mqttbroker.Provisioner = (*noopClient)(nil)

type apiClient struct {
	baseURL string
	authID  string
	http    *http.Client
	key     string
	secret  string
}

// NewAPIClient returns a Provisioner that manages credentials via the EMQX v5 REST API.
// key and secret are the EMQX admin credentials (HTTP Basic auth).
// authID is the authentication resource ID (e.g. "password_based:built_in_database").
func NewAPIClient(baseURL, key, secret, authID string) mqttbroker.Provisioner {
	return &apiClient{
		baseURL: baseURL,
		authID:  authID,
		key:     key,
		secret:  secret,
		http:    &http.Client{},
	}
}

func (c *apiClient) ProvisionDevice(ctx context.Context, username, password string) error {
	body, _ := json.Marshal(map[string]string{
		"user_id":  username,
		"password": password,
	})
	path := fmt.Sprintf("/api/v5/authentication/%s/users", c.authID)
	if err := c.do(ctx, http.MethodPost, path, body); err != nil {
		return fmt.Errorf("emqx: create credential: %w", err)
	}
	return nil
}

func (c *apiClient) DeleteDevice(ctx context.Context, username string) error {
	path := fmt.Sprintf("/api/v5/authentication/%s/users/%s", c.authID, username)
	if err := c.do(ctx, http.MethodDelete, path, nil); err != nil {
		return fmt.Errorf("emqx: delete credential: %w", err)
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
	req.SetBasicAuth(c.key, c.secret)
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

// noopClient is returned when EMQX is not configured.
type noopClient struct{}

func NewNoOp() mqttbroker.Provisioner                                          { return &noopClient{} }
func (n *noopClient) ProvisionDevice(_ context.Context, _, _ string) error    { return nil }
func (n *noopClient) DeleteDevice(_ context.Context, _ string) error          { return nil }
