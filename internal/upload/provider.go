package upload

import (
	"context"
	"io"
)

// Provider defines the interface for file upload providers
type Provider interface {
	// Upload uploads content from reader to the remote path
	Upload(ctx context.Context, reader io.Reader, remotePath string) error

	// Configure sets up the provider with the given configuration
	Configure(config map[string]any) error

	// Name returns the provider name
	Name() string
}
