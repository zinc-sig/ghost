//go:build !linux

package sandbox

// ApplySandbox is a no-op on non-Linux platforms.
func ApplySandbox(workDir string) error {
	return nil
}

// EnforceMaxPids is a no-op on non-Linux platforms.
func EnforceMaxPids(maxPids uint64) error {
	return nil
}

// EnforceMaxFileBytes is a no-op on non-Linux platforms.
func EnforceMaxFileBytes(maxBytes uint64) error {
	return nil
}

// LandlockAvailable is always false on non-Linux platforms.
func LandlockAvailable() bool { return false }
