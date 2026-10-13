package main

import (
	"runtime/debug"

	"github.com/dorkitude/tsctl/cmd"
)

// Set by GoReleaser via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if version == "dev" {
		// `go install ...@vX.Y.Z` builds carry the module version instead.
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
	cmd.SetVersion(version)
	cmd.Execute()
}
