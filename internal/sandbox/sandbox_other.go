//go:build !linux

package sandbox

import "errors"

// landlockABI reports that Landlock is unavailable: it is a Linux feature.
var landlockABI = func() (int, error) {
	return 0, errors.New("the Landlock LSM is available only on Linux")
}

// ApplySandbox applies no restrictions on non-Linux platforms, so it
// returns the RequireLandlock error unless EnvLandlockBestEffort is set.
func ApplySandbox(workDir string) error {
	return RequireLandlock()
}

// EnforceMaxPids is a no-op on non-Linux platforms.
func EnforceMaxPids(maxPids uint64) error {
	return nil
}

// EnforceMaxFileBytes is a no-op on non-Linux platforms.
func EnforceMaxFileBytes(maxBytes uint64) error {
	return nil
}
