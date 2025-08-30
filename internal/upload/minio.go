package upload

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioProvider implements the Provider interface for MinIO/S3 storage
type MinioProvider struct {
	client *minio.Client
	bucket string
	prefix string
}

// NewMinioProvider creates a new MinioProvider
func NewMinioProvider() *MinioProvider {
	return &MinioProvider{}
}

// Name returns the provider name
func (m *MinioProvider) Name() string {
	return "minio"
}

// Configure sets up the MinIO client with the given configuration
func (m *MinioProvider) Configure(config map[string]any) error {
	// Extract required configuration
	endpoint, ok := getStringValue(config, "endpoint")
	if !ok {
		return fmt.Errorf("minio: endpoint is required")
	}

	accessKey, ok := getStringValue(config, "access_key")
	if !ok {
		return fmt.Errorf("minio: access_key is required")
	}

	secretKey, ok := getStringValue(config, "secret_key")
	if !ok {
		return fmt.Errorf("minio: secret_key is required")
	}

	bucket, ok := getStringValue(config, "bucket")
	if !ok {
		return fmt.Errorf("minio: bucket is required")
	}

	// Parse endpoint to check for protocol
	var secure bool
	u, err := url.Parse(endpoint)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		// Valid URL with HTTP/HTTPS protocol
		secure = (u.Scheme == "https")
		endpoint = u.Host
		// If Host is empty (e.g., "http://"), it's an invalid endpoint
		if endpoint == "" {
			return fmt.Errorf("minio: invalid endpoint URL")
		}
	} else {
		// No protocol or not a valid URL - use config/default
		secure = getBoolValue(config, "secure", true)
	}

	// Optional configuration with defaults
	region := getStringValueWithDefault(config, "region", "us-east-1")
	prefix := getStringValueWithDefault(config, "prefix", "")

	// Create MinIO client
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return fmt.Errorf("minio: failed to create client: %w", err)
	}

	m.client = client
	m.bucket = bucket
	m.prefix = prefix

	// Check if bucket exists
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("minio: failed to check bucket existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("minio: bucket %s does not exist", bucket)
	}

	return nil
}

// Upload uploads content from reader to MinIO
func (m *MinioProvider) Upload(ctx context.Context, reader io.Reader, remotePath string) error {
	if m.client == nil {
		return fmt.Errorf("minio: provider not configured")
	}

	// Combine prefix with remote path
	objectName := remotePath
	if m.prefix != "" {
		objectName = filepath.Join(m.prefix, remotePath)
	}

	// Upload the content
	// -1 means unknown size, MinIO will handle streaming
	_, err := m.client.PutObject(ctx, m.bucket, objectName, reader, -1, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio: failed to upload to %s: %w", objectName, err)
	}

	return nil
}

// Helper functions to extract values from config map
func getStringValue(config map[string]any, key string) (string, bool) {
	if val, ok := config[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

func getStringValueWithDefault(config map[string]any, key, defaultValue string) string {
	if val, ok := getStringValue(config, key); ok {
		return val
	}
	return defaultValue
}

func getBoolValue(config map[string]any, key string, defaultValue bool) bool {
	if val, ok := config[key]; ok {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				return b
			}
		}
	}
	return defaultValue
}
