// SPDX-License-Identifier: MIT

// Command docsync checks that the documents of a Go module say what its code
// does. See README.md.
package main

import (
	"io"
	"os"
	"runtime/debug"

	"github.com/otfabric/docsync/internal/cli"
)

// version is set by the release build (-X main.version=...).
var version = "dev"

// exit is os.Exit (tests replace it).
var exit = os.Exit

func main() {
	exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	switch bi, ok := debug.ReadBuildInfo(); {
	case version != "dev":
		cli.Version = version
	case ok && bi.Main.Version != "" && bi.Main.Version != "(devel)":
		cli.Version = bi.Main.Version // go run github.com/otfabric/docsync@vX.Y.Z
	}
	return cli.Run(args, stdout, stderr)
}
