package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv(contract.EnvTemporalAddress, "temporal:7233")
	t.Setenv(contract.EnvTaskQueue, "ghost-run-55")
	t.Setenv(contract.EnvStorageEndpoint, "minio:9000")
	t.Setenv(contract.EnvStorageAccessKey, "access")
	t.Setenv(contract.EnvStorageSecretKey, "secret")
	// Keep staging in the test sandbox.
	t.Setenv(EnvStagingDir, t.TempDir())
}

func TestLoadConfigDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TemporalAddress != "temporal:7233" {
		t.Errorf("TemporalAddress = %q", cfg.TemporalAddress)
	}
	if cfg.TemporalNamespace != "default" {
		t.Errorf("TemporalNamespace = %q, want %q", cfg.TemporalNamespace, "default")
	}
	if cfg.TaskQueue != "ghost-run-55" {
		t.Errorf("TaskQueue = %q", cfg.TaskQueue)
	}
	if cfg.Workdir != "/workspace" {
		t.Errorf("Workdir = %q, want %q", cfg.Workdir, "/workspace")
	}
	if cfg.StorageSecure {
		t.Error("StorageSecure = true, want false by default")
	}
	if !cfg.Sandbox {
		t.Error("Sandbox = false, want true by default")
	}
	if cfg.MaxPids != 32 {
		t.Errorf("MaxPids = %d, want 32", cfg.MaxPids)
	}
	if cfg.MaxConcurrentExecs != defaultMaxConcurrentExecs {
		t.Errorf("MaxConcurrentExecs = %d, want %d", cfg.MaxConcurrentExecs, defaultMaxConcurrentExecs)
	}
	if cfg.DefaultTimeout != 60*time.Second {
		t.Errorf("DefaultTimeout = %v, want 60s", cfg.DefaultTimeout)
	}
	if cfg.DefaultOutputLimit != 64<<20 {
		t.Errorf("DefaultOutputLimit = %d, want 64 MiB", cfg.DefaultOutputLimit)
	}
	if cfg.StagingDir == "" {
		t.Error("StagingDir must be set")
	}
	if cfg.GhostPath == "" {
		t.Error("GhostPath must default to the running executable")
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv(contract.EnvTemporalNamespace, "grading")
	t.Setenv(contract.EnvWorkdir, "/run-workspace")
	t.Setenv(contract.EnvStorageSecure, "true")
	t.Setenv(contract.EnvStorageSessionToken, "sts-token")
	t.Setenv(contract.EnvTemporalAuthToken, "auth-token")
	t.Setenv(EnvSandbox, "false")
	t.Setenv(EnvMaxPids, "128")
	t.Setenv(EnvMaxConcurrentExecs, "8")
	t.Setenv(EnvDefaultTimeout, "5m")
	t.Setenv(EnvDefaultOutputLimit, "1048576")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TemporalNamespace != "grading" {
		t.Errorf("TemporalNamespace = %q", cfg.TemporalNamespace)
	}
	if cfg.Workdir != "/run-workspace" {
		t.Errorf("Workdir = %q", cfg.Workdir)
	}
	if !cfg.StorageSecure {
		t.Error("StorageSecure = false, want true")
	}
	if cfg.StorageSessionToken != "sts-token" {
		t.Errorf("StorageSessionToken = %q", cfg.StorageSessionToken)
	}
	if cfg.AuthToken != "auth-token" {
		t.Errorf("AuthToken = %q", cfg.AuthToken)
	}
	if cfg.Sandbox {
		t.Error("Sandbox = true, want false")
	}
	if cfg.MaxPids != 128 {
		t.Errorf("MaxPids = %d, want 128", cfg.MaxPids)
	}
	if cfg.MaxConcurrentExecs != 8 {
		t.Errorf("MaxConcurrentExecs = %d, want 8", cfg.MaxConcurrentExecs)
	}
	if cfg.DefaultTimeout != 5*time.Minute {
		t.Errorf("DefaultTimeout = %v, want 5m", cfg.DefaultTimeout)
	}
	if cfg.DefaultOutputLimit != 1<<20 {
		t.Errorf("DefaultOutputLimit = %d, want 1 MiB", cfg.DefaultOutputLimit)
	}
}

func TestLoadConfigInvalidOutputLimit(t *testing.T) {
	setRequiredEnv(t)
	for _, v := range []string{"-1", "64MB", "x"} {
		t.Setenv(EnvDefaultOutputLimit, v)
		if _, err := LoadConfig(); err == nil {
			t.Errorf("LoadConfig accepted %s=%q, want error", EnvDefaultOutputLimit, v)
		}
	}
}

func TestLoadConfigOutputLimitZeroDisables(t *testing.T) {
	// The documented emergency escape: 0 disables the default cap entirely
	// (no --max-file-bytes flag is passed for specs without an explicit
	// limit). Pin it so a future "0 is invalid" tightening is a deliberate
	// decision, not an accident.
	setRequiredEnv(t)
	t.Setenv(EnvDefaultOutputLimit, "0")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig rejected %s=0: %v", EnvDefaultOutputLimit, err)
	}
	if cfg.DefaultOutputLimit != 0 {
		t.Errorf("DefaultOutputLimit = %d, want 0 (cap disabled)", cfg.DefaultOutputLimit)
	}
}

func TestLoadConfigMissingRequired(t *testing.T) {
	required := []string{
		contract.EnvTemporalAddress,
		contract.EnvTaskQueue,
		contract.EnvStorageEndpoint,
		contract.EnvStorageAccessKey,
		contract.EnvStorageSecretKey,
	}
	for _, missing := range required {
		t.Run(missing, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(missing, "")
			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("LoadConfig succeeded without %s", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %q does not name the missing variable %s", err, missing)
			}
		})
	}
}
