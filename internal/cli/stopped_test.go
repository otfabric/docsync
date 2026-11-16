// SPDX-License-Identifier: MIT

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/docsync/internal/check"
	"github.com/otfabric/docsync/internal/finding"
)

// TestCheckCouldNotRun: a check whose tool cannot run fails the run with
// exit 10 when the run reports a rule it stops; an ignore entry for its
// DS-CONF finding, or --rules without those rules, lets the run go on.
func TestCheckCouldNotRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/clean\n\ngo 1.26.0\n")
	writeFile(t, dir, "clean.go", "// Package clean is clean.\npackage clean\n\n// Hello says hello.\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, dir, "API.md", "# API\n\n```go\nfunc Hello() string\n```\n\n```go\nfmt.Println(clean.Hello())\n```\n")
	writeFile(t, dir, "README.md", "# clean\n\nRequires Go 1.26 or later.\n")
	t.Setenv("DOCSYNC_GO", filepath.Join(dir, "no-such-go"))
	for _, tc := range []struct {
		name   string
		config string
		args   []string
		code   int
		stderr string
	}{
		{"go list cannot run", "", nil, ExitInternal, "DS-CODE could not run: typecheck: "},
		{"--rules DS-CODE", "", []string{"--rules", "DS-CODE"}, ExitInternal, "DS-CODE could not run: typecheck: "},
		{"--rules DS-CONF", "", []string{"--rules", "DS-CONF"}, ExitInternal, "DS-CODE could not run: typecheck: "},
		{"--rules without DS-CODE", "", []string{"--rules", "DS-SEC"}, ExitOK, ""},
		{"accepted in ignore", `{"ignore": [{"rule": "DS-CONF", "subject": "typecheck", "reason": "no go command"}]}`, nil, ExitOK, ""},
		{"the manifest test cannot run", `{"api": false, "clis": [{"name": "x", "manifest": "."}], "exclude_docs": ["API.md"]}`, []string{"--rules", "DS-FLAG"}, ExitInternal, "DS-FLAG could not run: x manifest: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, dir, ".otfabric/docsync.json", "{}")
			if tc.config != "" {
				writeFile(t, dir, ".otfabric/docsync.json", tc.config)
			}
			code, out, errOut := run(t, append(append([]string{"check"}, tc.args...), dir)...)
			if code != tc.code {
				t.Errorf("exit %d, want %d\n%s%s", code, tc.code, out, errOut)
			}
			if tc.stderr != "" && !strings.Contains(errOut, "docsync: "+tc.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.stderr, errOut)
			}
			if tc.stderr == "" && errOut != "" {
				t.Errorf("stderr: %s", errOut)
			}
		})
	}
}

// TestInternalError: an unexpected internal error of a check exits 10, with
// the message and the stack on stderr, control characters made visible.
func TestInternalError(t *testing.T) {
	old := check.All
	check.All = []check.Check{func(*check.Repo) []finding.Finding { panic("unexpected \x1b[2J state") }}
	t.Cleanup(func() { check.All = old })
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# x\n")
	code, out, errOut := run(t, "check", dir)
	if code != ExitInternal || out != "" {
		t.Errorf("exit %d, stdout %q", code, out)
	}
	if !strings.HasPrefix(errOut, "docsync: internal error: unexpected \\x1b[2J state\n\tgoroutine ") || strings.Contains(errOut, "\x1b") {
		t.Errorf("stderr:\n%q", errOut)
	}
}
