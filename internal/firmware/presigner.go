package firmware

import (
	"context"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/s3"
)

// URLPresigner generates time-limited download URLs for firmware artifacts.
type URLPresigner interface {
	PresignDownloadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error)
}

type s3URLPresigner struct{ client s3.Client }

func NewURLPresigner(client s3.Client) URLPresigner {
	return &s3URLPresigner{client: client}
}

func (p *s3URLPresigner) PresignDownloadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	return p.client.PresignGetURL(ctx, objectKey, expiry)
}
