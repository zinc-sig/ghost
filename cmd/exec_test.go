package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestExecSuperviseSurface guards the subcommand redesign: exec and supervise
// expose the --landlock isolation flag and not the legacy bundled --sandbox
// flag or the grading uploader flags.
func TestExecSuperviseSurface(t *testing.T) {
	for _, c := range []struct {
		name        string
		cmd         *cobra.Command
		wantFlags   []string
		unwantFlags []string
	}{
		{
			name: "exec",
			cmd:  execCmd,
			// No --timeout: exec replaces the process via execve, so no parent
			// survives to enforce a deadline (it was a silent no-op). supervise
			// keeps --timeout because it forks and waits.
			wantFlags:   []string{"input", "output", "stderr", "landlock", "workdir", "max-pids"},
			unwantFlags: []string{"sandbox", "sandbox-workdir", "supervise", "exec", "webhook-url", "upload-provider", "result-file", "max-output-bytes", "timeout", "isolate-network"},
		},
		{
			name:        "supervise",
			cmd:         superviseCmd,
			wantFlags:   []string{"input", "output", "stderr", "landlock", "workdir", "max-pids", "timeout", "result-file", "max-output-bytes"},
			unwantFlags: []string{"sandbox", "sandbox-workdir", "supervise", "exec", "webhook-url", "upload-provider", "isolate-network"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, f := range c.wantFlags {
				if c.cmd.Flags().Lookup(f) == nil {
					t.Errorf("%s: missing expected flag --%s", c.name, f)
				}
			}
			for _, f := range c.unwantFlags {
				if c.cmd.Flags().Lookup(f) != nil {
					t.Errorf("%s: unexpected flag --%s present", c.name, f)
				}
			}
		})
	}
}

// TestRunRetainsLegacyFlags confirms run keeps its legacy grading surface
// (webhook/upload/context/sandbox) after the exec/supervise modes moved off it.
func TestRunRetainsLegacyFlags(t *testing.T) {
	for _, f := range []string{"sandbox", "sandbox-workdir", "webhook-url", "upload-provider", "context", "score"} {
		if runCmd.Flags().Lookup(f) == nil {
			t.Errorf("run: legacy flag --%s went missing", f)
		}
	}
	// --max-pids was only enforced by exec/supervise; run never applied it, so it
	// is no longer advertised on run (would be a silent no-op).
	for _, f := range []string{"exec", "supervise", "result-file", "max-output-bytes", "isolate-network", "landlock", "max-pids"} {
		if runCmd.Flags().Lookup(f) != nil {
			t.Errorf("run: flag --%s should no longer be on run", f)
		}
	}
}
