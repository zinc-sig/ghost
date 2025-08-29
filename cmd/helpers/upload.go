package helpers

import (
	"context"
	"fmt"
	"os"

	"github.com/zinc-sig/ghost/cmd/config"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/upload"
)

// BuildUploadConfig builds upload configuration from all sources
func BuildUploadConfig(cfg *config.UploadConfig) (map[string]any, error) {
	// Use the new generic builder with GHOST_UPLOAD_CONFIG prefix
	result, err := contextparser.BuildContextWithPrefix(
		"GHOST_UPLOAD_CONFIG",
		cfg.Config,
		cfg.ConfigKV,
		cfg.ConfigFile,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build upload config: %w", err)
	}

	if result == nil {
		return make(map[string]any), nil
	}

	if m, ok := result.(map[string]any); ok {
		return m, nil
	}

	return nil, fmt.Errorf("upload config must be an object/map")
}

// parseUploadEnv and toLowerSnakeCase are no longer needed - using ParseEnvWithPrefix

// SetupUploadProvider creates and configures an upload provider
func SetupUploadProvider(cfg *config.UploadConfig) (upload.Provider, map[string]any, error) {
	if cfg.Provider == "" {
		return nil, nil, nil
	}

	uploadConf, err := BuildUploadConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build upload config: %w", err)
	}

	provider, err := upload.NewProvider(cfg.Provider)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create upload provider: %w", err)
	}

	if err := provider.Configure(uploadConf); err != nil {
		return nil, nil, fmt.Errorf("failed to configure upload provider: %w", err)
	}

	return provider, uploadConf, nil
}

// HandleUploads uploads files using the provider
func HandleUploads(provider upload.Provider, files map[string]string, verbose bool) error {
	if provider == nil {
		return nil
	}

	ctx := context.Background()
	for localPath, remotePath := range files {
		reader, err := os.Open(localPath)
		if err != nil {
			return fmt.Errorf("failed to open %s for upload: %w", localPath, err)
		}
		defer func() { _ = reader.Close() }()

		if err := provider.Upload(ctx, reader, remotePath); err != nil {
			return fmt.Errorf("failed to upload to %s: %w", remotePath, err)
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "✓ Uploaded to: %s\n", remotePath)
		}
	}
	return nil
}

// PrintUploadInfo prints upload configuration in verbose mode
func PrintUploadInfo(provider upload.Provider, config map[string]any, outputPath, stderrPath string) {
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, "Upload Configuration")
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintf(os.Stderr, "Provider:       %s\n", provider.Name())

	// Print relevant config based on provider type
	if provider.Name() == "minio" {
		if endpoint, ok := config["endpoint"]; ok {
			fmt.Fprintf(os.Stderr, "Endpoint:       %v\n", endpoint)
		}
		if bucket, ok := config["bucket"]; ok {
			fmt.Fprintf(os.Stderr, "Bucket:         %v\n", bucket)
		}
		if prefix, ok := config["prefix"]; ok && prefix != "" {
			fmt.Fprintf(os.Stderr, "Prefix:         %v\n", prefix)
		}
	}

	fmt.Fprintf(os.Stderr, "Output Path:    %s\n", outputPath)
	fmt.Fprintf(os.Stderr, "Stderr Path:    %s\n", stderrPath)
	fmt.Fprintln(os.Stderr, "----------------------------------------")
}
