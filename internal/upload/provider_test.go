package upload

import (
	"context"
	"io"
	"strings"
	"testing"
)

// MockProvider implements Provider for testing
type MockProvider struct {
	name       string
	configured bool
	uploadErr  error
	uploads    []mockUpload
}

type mockUpload struct {
	content    string
	remotePath string
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name:    name,
		uploads: []mockUpload{},
	}
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Configure(config map[string]any) error {
	m.configured = true
	return nil
}

func (m *MockProvider) Upload(ctx context.Context, reader io.Reader, remotePath string) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	m.uploads = append(m.uploads, mockUpload{
		content:    string(content),
		remotePath: remotePath,
	})

	return nil
}

func TestProviderRegistry(t *testing.T) {
	// Test registering a provider
	testProviderName := "test-provider"
	RegisterProvider(testProviderName, func() Provider {
		return NewMockProvider(testProviderName)
	})

	// Test creating a registered provider
	provider, err := NewProvider(testProviderName)
	if err != nil {
		t.Fatalf("Failed to create registered provider: %v", err)
	}

	if provider.Name() != testProviderName {
		t.Errorf("Expected provider name %s, got %s", testProviderName, provider.Name())
	}

	// Test creating an unregistered provider
	_, err = NewProvider("unknown-provider")
	if err == nil {
		t.Error("Expected error for unknown provider, got nil")
	}
}

func TestMockProviderUpload(t *testing.T) {
	provider := NewMockProvider("test")

	// Configure the provider
	config := map[string]any{
		"test": "config",
	}
	if err := provider.Configure(config); err != nil {
		t.Fatalf("Failed to configure provider: %v", err)
	}

	if !provider.configured {
		t.Error("Provider should be configured")
	}

	// Test upload
	ctx := context.Background()
	content := "test content"
	remotePath := "path/to/file.txt"

	reader := strings.NewReader(content)
	if err := provider.Upload(ctx, reader, remotePath); err != nil {
		t.Fatalf("Failed to upload: %v", err)
	}

	// Verify upload
	if len(provider.uploads) != 1 {
		t.Fatalf("Expected 1 upload, got %d", len(provider.uploads))
	}

	upload := provider.uploads[0]
	if upload.content != content {
		t.Errorf("Expected content %q, got %q", content, upload.content)
	}
	if upload.remotePath != remotePath {
		t.Errorf("Expected remote path %q, got %q", remotePath, upload.remotePath)
	}
}

func TestMinioProviderName(t *testing.T) {
	provider := NewMinioProvider()
	if provider.Name() != "minio" {
		t.Errorf("Expected provider name 'minio', got %s", provider.Name())
	}
}

func TestMinioProviderConfigValidation(t *testing.T) {
	provider := NewMinioProvider()

	tests := []struct {
		name      string
		config    map[string]any
		expectErr bool
		errMsg    string
	}{
		{
			name:      "missing endpoint",
			config:    map[string]any{},
			expectErr: true,
			errMsg:    "endpoint is required",
		},
		{
			name: "missing access_key",
			config: map[string]any{
				"endpoint": "localhost:9000",
			},
			expectErr: true,
			errMsg:    "access_key is required",
		},
		{
			name: "missing secret_key",
			config: map[string]any{
				"endpoint":   "localhost:9000",
				"access_key": "minioadmin",
			},
			expectErr: true,
			errMsg:    "secret_key is required",
		},
		{
			name: "missing bucket",
			config: map[string]any{
				"endpoint":   "localhost:9000",
				"access_key": "minioadmin",
				"secret_key": "minioadmin",
			},
			expectErr: true,
			errMsg:    "bucket is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.Configure(tt.config)
			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
