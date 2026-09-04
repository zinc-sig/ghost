package helpers

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/cmd/config"
)

// TestBuildWebhookConfigFlagPrecedence verifies that an explicit direct flag
// overrides a config value even when the flag's value equals its default.
// Comparing the flag's value against its default, instead of checking whether
// the flag changed, would treat this explicit-but-default-valued case as unset.
func TestBuildWebhookConfigFlagPrecedence(t *testing.T) {
	t.Run("explicit default-valued method overrides config", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Config:  `{"method":"PUT"}`,
			Method:  DefaultWebhookMethod, // "POST", the default value
			Changed: map[string]bool{"webhook-method": true},
		}
		out, err := BuildWebhookConfig(cfg)
		if err != nil {
			t.Fatalf("BuildWebhookConfig: %v", err)
		}
		if out["method"] != DefaultWebhookMethod {
			t.Errorf("method = %v, want %q (explicit flag must win over config)", out["method"], DefaultWebhookMethod)
		}
	})

	t.Run("unchanged method leaves config value intact", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Config: `{"method":"PUT"}`,
			Method: DefaultWebhookMethod, // value present but flag not changed
			// Changed is nil/empty
		}
		out, err := BuildWebhookConfig(cfg)
		if err != nil {
			t.Fatalf("BuildWebhookConfig: %v", err)
		}
		if out["method"] != "PUT" {
			t.Errorf("method = %v, want \"PUT\" (config wins when flag not set)", out["method"])
		}
	})

	t.Run("explicit default-valued retries overrides config", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Config:  `{"retries":0}`,
			Retries: DefaultWebhookRetries, // 3, the default
			Changed: map[string]bool{"webhook-retries": true},
		}
		out, err := BuildWebhookConfig(cfg)
		if err != nil {
			t.Fatalf("BuildWebhookConfig: %v", err)
		}
		if out["retries"] != DefaultWebhookRetries {
			t.Errorf("retries = %v, want %d (explicit flag must win)", out["retries"], DefaultWebhookRetries)
		}
	})

	t.Run("unchanged retries leaves config value intact", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Config:  `{"retries":0}`,
			Retries: DefaultWebhookRetries,
		}
		out, err := BuildWebhookConfig(cfg)
		if err != nil {
			t.Fatalf("BuildWebhookConfig: %v", err)
		}
		// JSON numbers decode to float64.
		if got, ok := out["retries"].(float64); !ok || got != 0 {
			t.Errorf("retries = %v (%T), want 0 (config wins when flag not set)", out["retries"], out["retries"])
		}
	})
}

// TestRecordWebhookChanges verifies only flags actually set on the command line
// are recorded as changed.
func TestRecordWebhookChanges(t *testing.T) {
	cfg := &config.WebhookConfig{}
	cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
	SetupWebhookFlags(cmd, cfg)
	cmd.SetArgs([]string{"--webhook-method", "PUT"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	RecordWebhookChanges(cmd, cfg)

	if !cfg.Changed["webhook-method"] {
		t.Error("webhook-method should be recorded as changed")
	}
	if cfg.Changed["webhook-retries"] {
		t.Error("webhook-retries was not set, must not be recorded as changed")
	}
}
