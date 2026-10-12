package sandbox

import (
	"errors"
	"strings"
	"testing"
)

// withLandlockABI stands in for the kernel's Landlock ABI probe for the
// duration of the test.
func withLandlockABI(t *testing.T, v int, err error) {
	t.Helper()
	saved := landlockABI
	landlockABI = func() (int, error) { return v, err }
	t.Cleanup(func() { landlockABI = saved })
}

// TestRequireLandlock asserts that a kernel without Landlock is refused
// unless EnvLandlockBestEffort is true, that the refusal names the cause
// and the variable, and that a kernel with Landlock is accepted whatever
// the variable says. Accepting a missing Landlock silently would run the
// command with no filesystem boundary and no guard between commands.
func TestRequireLandlock(t *testing.T) {
	probeErr := errors.New("operation not supported")
	tests := []struct {
		name       string
		abi        int
		probeErr   error
		bestEffort string
		wantErr    []string
	}{
		{name: "available", abi: 5},
		{name: "available ignores an invalid opt-out", abi: 1, bestEffort: "maybe"},
		{name: "unsupported kernel is refused", probeErr: probeErr, wantErr: []string{"Landlock is unavailable", "operation not supported", EnvLandlockBestEffort}},
		{name: "ABI zero is refused", abi: 0, wantErr: []string{"ABI version 0"}},
		{name: "false keeps the requirement", probeErr: probeErr, bestEffort: "false", wantErr: []string{"Landlock is unavailable"}},
		{name: "true runs without Landlock", probeErr: probeErr, bestEffort: "true"},
		{name: "an invalid opt-out is an error", probeErr: probeErr, bestEffort: "maybe", wantErr: []string{"invalid " + EnvLandlockBestEffort}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withLandlockABI(t, tt.abi, tt.probeErr)
			t.Setenv(EnvLandlockBestEffort, tt.bestEffort)

			err := RequireLandlock()
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("RequireLandlock() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("RequireLandlock() = nil, want an error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("RequireLandlock() = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestApplySandboxRefusesWithoutLandlock asserts that ApplySandbox returns
// the RequireLandlock error before it builds a ruleset, so exec, supervise,
// and run all refuse on a kernel without Landlock.
func TestApplySandboxRefusesWithoutLandlock(t *testing.T) {
	withLandlockABI(t, 0, errors.New("function not implemented"))
	t.Setenv(EnvLandlockBestEffort, "")

	err := ApplySandbox(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Landlock is unavailable") {
		t.Fatalf("ApplySandbox() = %v, want the Landlock refusal", err)
	}
}
