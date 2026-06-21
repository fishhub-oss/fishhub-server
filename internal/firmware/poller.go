package firmware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ReleaseSource provides the latest known firmware release.
type ReleaseSource interface {
	LatestRelease() *Release // nil until the first successful fetch
}

// GitHubPoller polls the GitHub Releases API on a fixed interval and resolves
// each release to a Release by reading the manifest from the bucket.
type GitHubPoller struct {
	repoSlug    string
	token       string
	interval    time.Duration
	manifests   ManifestReader
	httpClient  *http.Client
	mu          sync.RWMutex
	latest      *Release
	logger      *slog.Logger
}

func NewGitHubPoller(repoSlug, token string, interval time.Duration, manifests ManifestReader, logger *slog.Logger) *GitHubPoller {
	if logger == nil {
		logger = slog.Default()
	}
	return &GitHubPoller{
		repoSlug:   repoSlug,
		token:      token,
		interval:   interval,
		manifests:  manifests,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		logger:     logger,
	}
}

// Run polls on interval until ctx is cancelled. Call in a goroutine.
// Performs one fetch immediately on start.
func (p *GitHubPoller) Run(ctx context.Context) {
	p.poll(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// LatestRelease returns the cached release. Safe for concurrent reads.
func (p *GitHubPoller) LatestRelease() *Release {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.latest
}

func (p *GitHubPoller) poll(ctx context.Context) {
	version, err := p.fetchLatestTag(ctx)
	if err != nil {
		p.logger.Warn("firmware poller: github fetch failed", "error", err)
		return
	}

	manifest, err := p.manifests.GetManifest(ctx, version)
	if err != nil {
		p.logger.Warn("firmware poller: manifest fetch failed", "version", version, "error", err)
		return
	}

	p.mu.Lock()
	p.latest = &Release{
		Version:   manifest.Version,
		SHA256:    manifest.SHA256,
		ObjectKey: manifest.ObjectKey,
	}
	p.mu.Unlock()

	p.logger.Info("firmware poller: updated", "version", manifest.Version)
}

type ghReleaseResponse struct {
	TagName string `json:"tag_name"`
}

func (p *GitHubPoller) fetchLatestTag(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", p.repoSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release ghReleaseResponse
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("parse github response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("github api: empty tag_name")
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}
