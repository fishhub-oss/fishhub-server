package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GitHubUserFetcher fetches a GitHub user profile from an access token.
type GitHubUserFetcher interface {
	Fetch(ctx context.Context, accessToken string) (email, name, providerSub string, err error)
}

type gitHubHTTPFetcher struct {
	httpClient *http.Client
}

// NewGitHubHTTPFetcher returns a GitHubUserFetcher backed by the real GitHub API.
func NewGitHubHTTPFetcher() GitHubUserFetcher {
	return &gitHubHTTPFetcher{httpClient: &http.Client{}}
}

func (e *gitHubHTTPFetcher) Fetch(ctx context.Context, accessToken string) (string, string, string, error) {
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

func (e *gitHubHTTPFetcher) fetchUser(ctx context.Context, accessToken string) (email, name, sub string, err error) {
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

func (e *gitHubHTTPFetcher) fetchPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
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
