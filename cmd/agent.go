package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/agent"
	"github.com/zinc-sig/ghost/internal/reaper"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run as a Temporal worker inside a grading container",
	Long: `Run ghost in agent mode (RFD 0015 grading runtime): a long-lived Temporal
worker that joins a per-run task queue and serves exactly two activities:
ghost-fetch-submission and ghost-run-exec. Each exec spec is run in a
sandboxed child process ('ghost exec'); the agent itself is never
sandboxed.

Configuration is read from GHOST_AGENT_* environment variables (see the
agent contract). The agent scrubs every GHOST_AGENT_* variable from the
environment of the commands it spawns.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// As container init (PID 1) the agent must reap zombies left by
		// killed process trees. No-op when not PID 1.
		reaper.Start()

		cfg, err := agent.LoadConfig()
		if err != nil {
			return err
		}
		cfg.AgentVersion = appVersion

		fmt.Fprintf(os.Stderr, "ghost agent %s: task queue %q, workspace %q, temporal %s (namespace %q)\n",
			appVersion, cfg.TaskQueue, cfg.Workdir, cfg.TemporalAddress, cfg.TemporalNamespace)

		return agent.Run(cfg)
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
}
