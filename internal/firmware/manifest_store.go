package firmware

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fishhub-oss/fishhub-server/internal/s3"
)

// ManifestReader retrieves the firmware manifest for a given version.
type ManifestReader interface {
	GetManifest(ctx context.Context, version string) (Manifest, error)
}

type s3ManifestReader struct{ client s3.Client }

func NewManifestReader(client s3.Client) ManifestReader {
	return &s3ManifestReader{client: client}
}

func (r *s3ManifestReader) GetManifest(ctx context.Context, version string) (Manifest, error) {
	key := fmt.Sprintf("firmware/%s/manifest.json", version)
	data, err := r.client.GetObject(ctx, key)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest reader: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest reader: parse %q: %w", key, err)
	}
	return m, nil
}
