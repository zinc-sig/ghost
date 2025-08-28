package cmd

import (
	"context"
	"fmt"
	"os"

	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/upload"
)

// BuildUploadConfig builds upload configuration from all sources
func BuildUploadConfig(config *UploadConfig) (map[string]any, error) {
	// Parse environment variables with GHOST_UPLOAD_CONFIG prefix
	uploadEnv := parseUploadEnv()

	// Use context parser to build config from all sources
	contexts := []any{}

	// 1. Environment variables (lowest priority)
	if uploadEnv != nil {
		contexts = append(contexts, uploadEnv)
	}

	// 2. Config file
	if config.ConfigFile != "" {
		fileConfig, err := contextparser.ParseFile(config.ConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to parse upload config file: %w", err)
		}
		contexts = append(contexts, fileConfig)
	}

	// 3. JSON string
	if config.Config != "" {
		jsonConfig, err := contextparser.ParseJSON(config.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to parse upload config JSON: %w", err)
		}
		contexts = append(contexts, jsonConfig)
	}

	// 4. Key-value pairs (highest priority)
	if len(config.ConfigKV) > 0 {
		kvConfig := make(map[string]any)
		for _, kv := range config.ConfigKV {
			key, value, err := contextparser.ParseKV(kv)
			if err != nil {
				return nil, fmt.Errorf("failed to parse upload config KV: %w", err)
			}
			kvConfig[key] = value
		}
		contexts = append(contexts, kvConfig)
	}

	result := contextparser.MergeContexts(contexts...)
	if result == nil {
		return make(map[string]any), nil
	}

	if m, ok := result.(map[string]any); ok {
		return m, nil
	}

	return nil, fmt.Errorf("upload config must be an object/map")
}

// parseUploadEnv parses GHOST_UPLOAD_CONFIG* environment variables
func parseUploadEnv() map[string]any {
	config := make(map[string]any)

	// Check for GHOST_UPLOAD_CONFIG JSON string
	if jsonStr := os.Getenv("GHOST_UPLOAD_CONFIG"); jsonStr != "" {
		if parsed, err := contextparser.ParseJSON(jsonStr); err == nil {
			if m, ok := parsed.(map[string]any); ok {
				for k, v := range m {
					config[k] = v
				}
			}
		}
	}

	// Check for GHOST_UPLOAD_CONFIG_* variables
	environ := os.Environ()
	for _, env := range environ {
		const prefix = "GHOST_UPLOAD_CONFIG_"
		if len(env) > len(prefix) && env[:len(prefix)] == prefix {
			parts := [2]string{}
			idx := 0
			for i, ch := range env {
				if ch == '=' && idx == 0 {
					parts[0] = env[:i]
					parts[1] = env[i+1:]
					idx = 1
					break
				}
			}
			if idx == 1 && len(parts[1]) > 0 {
				key := parts[0][len(prefix):]
				key = toLowerSnakeCase(key)
				// Apply type inference to env var values
				_, value, _ := contextparser.ParseKV(key + "=" + parts[1])
				config[key] = value
			}
		}
	}

	if len(config) == 0 {
		return nil
	}
	return config
}

// toLowerSnakeCase converts UPPER_SNAKE_CASE to lower_snake_case
func toLowerSnakeCase(s string) string {
	result := ""
	for _, ch := range s {
		if ch == '_' {
			result += "_"
		} else if ch >= 'A' && ch <= 'Z' {
			result += string(ch - 'A' + 'a')
		} else {
			result += string(ch)
		}
	}
	return result
}

// SetupUploadProvider creates and configures an upload provider
func SetupUploadProvider(config *UploadConfig) (upload.Provider, map[string]any, error) {
	if config.Provider == "" {
		return nil, nil, nil
	}

	uploadConf, err := BuildUploadConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build upload config: %w", err)
	}

	provider, err := upload.NewProvider(config.Provider)
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
