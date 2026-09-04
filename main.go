// Ghost is a command orchestration CLI. It runs an external command and
// reports structured execution metadata.
package main

import "github.com/zinc-sig/ghost/cmd"

// Version is the build version, injected by the release workflow via
// -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	cmd.SetVersion(Version)
	cmd.Execute()
}
