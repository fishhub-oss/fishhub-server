package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GitHubExchanger exchanges a GitHub OAuth code for user profile data.
type GitHubExchanger interface {
	Exchange(ctx context.Context, code string) (email, name, providerSub string, err error)
}

type gitHubHTTPExchanger struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
}

// NewGitHubHTTPExchanger returns a GitHubExchanger backed by the real GitHub API.
func NewGitHubHTTPExchanger(clientID, clientSecret string) GitHubExchanger {
	return &gitHubHTTPExchanger{
		httpClient:   &http.Client{},
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

func (e *gitHubHTTPExchanger) Exchange(ctx context.Context, code string) (string, string, string, error) {
	accessToken, err := e.exchangeCode(ctx, code)
	if err != nil {
		return "", "", "", err
	}

	email, name, sub, err := e.fetchUser(ctx, accessToken)
	if err != nil {
		return "", "", "", err
	}

	if email == "" {
		email, err = e.fetchPrimaryEmail(ctx, accessToken)
		if err != nil {
			return "", "", "", err
		}
	}

	return email, name, sub, nil
}

func (e *gitHubHTTPExchanger) exchangeCode(ctx context.Context, code string) (string, error) {
	body := url.Values{
		"client_id":     {e.clientID},
		"client_secret": {e.clientSecret},
		"code":          {code},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: github token exchange returned %d: %s", ErrInvalidIDToken, resp.StatusCode, b)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode github token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("%w: %s", ErrInvalidIDToken, tokenResp.Error)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("%w: github returned empty access token", ErrInvalidIDToken)
	}
	return tokenResp.AccessToken, nil
}

func (e *gitHubHTTPExchanger) fetchUser(ctx context.Context, accessToken string) (email, name, sub string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", "", "", fmt.Errorf("build user request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("github user fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("%w: github /user returned %d: %s", ErrInvalidIDToken, resp.StatusCode, b)
	}

	var user struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", "", "", fmt.Errorf("decode github user: %w", err)
	}

	name = user.Name
	if name == "" {
		name = user.Login
	}
	return user.Email, name, fmt.Sprintf("%d", user.ID), nil
}

func (e *gitHubHTTPExchanger) fetchPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", fmt.Errorf("build emails request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github emails fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: github /user/emails returned %d: %s", ErrInvalidIDToken, resp.StatusCode, b)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", fmt.Errorf("decode github emails: %w", err)
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	return "", fmt.Errorf("%w: no verified primary email on github account", ErrInvalidIDToken)
}
