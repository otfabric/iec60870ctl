// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/otfabric/iec104ctl/cmd"
)

// Injected at link time (see Makefile): -X main.version=... etc.
var (
	version   = "dev"
	tag       = ""
	commit    = ""
	buildDate = ""
)

// exit is os.Exit; a test replaces it to see the code main ends with.
var exit = os.Exit

func main() {
	cmd.SetBuildMeta(version, tag, commit, buildDate)
	exit(cmd.Execute())
}
