// Package reaper reaps orphaned child processes when ghost runs as PID 1 in a
// container. On Linux a SIGCHLD handler drains zombies with Wait4; on other
// platforms the package is a no-op.
package reaper
