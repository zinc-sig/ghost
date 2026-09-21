//go:build !linux

package agent

// disableDumpable is a no-op on non-Linux platforms.
func disableDumpable() error { return nil }
