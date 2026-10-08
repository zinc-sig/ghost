package sandbox

import (
	"fmt"
	"os"
	"strconv"
)

// EnvLandlockBestEffort names the environment variable that lets ghost run a
// sandboxed command on a kernel that does not enforce Landlock. When it is
// unset or false, ApplySandbox refuses to run the command there, because
// Landlock is the filesystem boundary of the sandbox and also what stops a
// command from tracing processes outside its own domain. Set it to true only
// on a development host without Landlock; the command then runs without
// filesystem restrictions.
const EnvLandlockBestEffort = "GHOST_LANDLOCK_BEST_EFFORT"

// LandlockAvailable reports whether the kernel enforces Landlock (ABI 1 or
// later) for this process. It is false when the kernel lacks the Landlock
// LSM and also when a seccomp filter denies the Landlock syscalls.
func LandlockAvailable() bool {
	v, err := landlockABI()
	return err == nil && v >= 1
}

// RequireLandlock returns nil when the kernel enforces Landlock, or when
// EnvLandlockBestEffort is set to true. Otherwise it returns an error that
// names the cause and both remedies.
func RequireLandlock() error {
	v, err := landlockABI()
	if err == nil && v >= 1 {
		return nil
	}
	allowed, perr := landlockBestEffort()
	if perr != nil {
		return perr
	}
	if allowed {
		return nil
	}
	reason := fmt.Sprintf("ABI version %d", v)
	if err != nil {
		reason = err.Error()
	}
	return fmt.Errorf("sandbox: Landlock is unavailable (%s), and the command would run without filesystem restrictions; "+
		"enable the Landlock LSM and allow the landlock syscalls in the seccomp profile, "+
		"or set %s=true to run without Landlock on a development host", reason, EnvLandlockBestEffort)
}

// landlockBestEffort reads EnvLandlockBestEffort. An unset or empty value is
// false; a value strconv.ParseBool rejects is an error, so a mistyped
// setting does not silently keep or drop the requirement.
func landlockBestEffort() (bool, error) {
	v := os.Getenv(EnvLandlockBestEffort)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("sandbox: invalid %s %q: %w", EnvLandlockBestEffort, v, err)
	}
	return b, nil
}
