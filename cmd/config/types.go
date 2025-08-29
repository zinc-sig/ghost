package config

import "time"

// ContextConfig holds context-related flags
type ContextConfig struct {
	JSON string
	KV   []string
	File string
}

// UploadConfig holds upload-related flags
type UploadConfig struct {
	Provider   string
	Config     string
	ConfigKV   []string
	ConfigFile string
}

// CommonFlags holds commonly used flags across commands
type CommonFlags struct {
	Verbose    bool
	DryRun     bool
	TimeoutStr string
	Timeout    time.Duration
	Score      int
	ScoreSet   bool
}

// WebhookConfig holds webhook-related flags
type WebhookConfig struct {
	// Direct configuration flags
	URL        string
	Method     string // HTTP method (GET, POST, PUT, PATCH, DELETE)
	AuthType   string
	AuthToken  string
	Timeout    string
	Retries    int
	RetryDelay string

	// Alternative configuration methods
	Config     string   // JSON string configuration
	ConfigKV   []string // Key-value pairs
	ConfigFile string   // Path to JSON config file
}
