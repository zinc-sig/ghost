package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/reaper"
)

var (
	heartbeatInterval time.Duration
	heartbeatFile     string
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "PID 1 keepalive that writes timestamps to a file",
	Long: `Run a keepalive loop that periodically writes Unix timestamps to a file.
Intended for use as a container ENTRYPOINT to signal liveness.

The process handles SIGTERM and SIGINT for graceful shutdown.`,
	Example: `  ghost heartbeat
  ghost heartbeat --interval 5s --file /output/.heartbeat`,
	RunE: heartbeatCommand,
}

func init() {
	heartbeatCmd.Flags().DurationVar(&heartbeatInterval, "interval", 10*time.Second, "interval between heartbeat writes")
	heartbeatCmd.Flags().StringVar(&heartbeatFile, "file", "/output/.heartbeat", "file to write heartbeat timestamps to")
}

func heartbeatCommand(cmd *cobra.Command, args []string) error {
	// Reap zombie children so fork bomb corpses free their PID slots promptly.
	reaper.Start()

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// Write an initial heartbeat immediately
	if err := writeHeartbeat(heartbeatFile); err != nil {
		fmt.Fprintf(os.Stderr, "heartbeat: initial write failed: %v\n", err)
	}

	consecutiveErrors := 0
	const maxConsecutiveErrors = 5

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "heartbeat: shutting down")
			return nil
		case <-ticker.C:
			if err := writeHeartbeat(heartbeatFile); err != nil {
				consecutiveErrors++
				fmt.Fprintf(os.Stderr, "heartbeat: write failed (%d/%d): %v\n", consecutiveErrors, maxConsecutiveErrors, err)
				if consecutiveErrors >= maxConsecutiveErrors {
					return fmt.Errorf("heartbeat: too many consecutive write errors (%d)", consecutiveErrors)
				}
			} else {
				consecutiveErrors = 0
			}
		}
	}
}

func writeHeartbeat(path string) error {
	// Create parent dirs so a nested --file path works (matches run/supervise).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// 0600 for hygiene. Note: a same-UID child can still overwrite this to forge
	// liveness; authoritative recycling must be corroborated host-side (daemon
	// container state), so core treats .heartbeat as a hint only.
	//
	// Open+fchmod rather than os.WriteFile so a PRE-EXISTING broader-mode
	// .heartbeat is tightened too (O_CREAT's mode applies only on creation).
	// OWNERSHIP CAVEAT: fchmod succeeds only on a file ghost owns. The driver
	// pre-creates /output/.heartbeat root-owned 0666 while ghost runs non-root, so
	// fchmod returns EPERM there — tolerated as a no-op (tightening a root-owned
	// file is core's job). When ghost owns the file it is guaranteed 0600.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ENOSYS) {
		f.Close()
		return err
	}
	if _, err := f.Write([]byte(ts)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
