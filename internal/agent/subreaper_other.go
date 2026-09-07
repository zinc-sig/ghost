//go:build !linux

package agent

// enableChildSubreaper is a no-op on non-Linux platforms.
func enableChildSubreaper() error { return nil }
