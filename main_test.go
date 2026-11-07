// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/otfabric/docsync/internal/cli"
)

func TestRun(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "v9.9.9"
	var out bytes.Buffer
	if code := run([]string{"version"}, &out, &out); code != cli.ExitOK || !strings.Contains(out.String(), "v9.9.9") {
		t.Fatalf("exit %d: %s", code, out.String())
	}
}

func TestMainExits(t *testing.T) {
	defer func(f func(int)) { exit = f }(exit)
	got := -1
	exit = func(c int) { got = c }
	main() // go test's own arguments are not a docsync command
	if got != cli.ExitUsage {
		t.Fatalf("exit %d", got)
	}
}
