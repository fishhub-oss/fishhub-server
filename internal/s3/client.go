package s3

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client is a generic S3-compatible bucket client.
type Client interface {
	GetObject(ctx context.Context, key string) ([]byte, error)
	PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}

type awsClient struct {
	s3      *awss3.Client
	presign *awss3.PresignClient
	bucket  string
}

// NewClient constructs a Client backed by the AWS SDK v2.
// endpoint must be the full S3-compatible URL (e.g. Railway Bucket endpoint).
func NewClient(endpoint, region, bucket, accessKey, secretKey string) (Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	svc := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return &awsClient{
		s3:      svc,
		presign: awss3.NewPresignClient(svc),
		bucket:  bucket,
	}, nil
}

func (c *awsClient) GetObject(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: get object %q: %w", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3: read object %q: %w", key, err)
	}
	return data, nil
}

func (c *awsClient) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, awss3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3: presign %q: %w", key, err)
	}
	return req.URL, nil
}

type noopClient struct{}

func NewNoOp() Client { return &noopClient{} }

func (n *noopClient) GetObject(_ context.Context, key string) ([]byte, error) {
	return nil, fmt.Errorf("s3: not configured (key %q)", key)
}

func (n *noopClient) PresignGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("s3: not configured (key %q)", key)
}
