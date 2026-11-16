// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// typecheckModule is a module whose API.md has a block that type-checks and
// one that imports dep.
func typecheckModule(t *testing.T, dep string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/tc\n\ngo 1.26.0\n")
	writeFile(t, dir, "tc.go", "// Package tc is a fixture.\npackage tc\n\n// Hello says hello.\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, dir, "README.md", "# tc\n\nRequires Go 1.26 or later.\n")
	writeFile(t, dir, "API.md", "# API\n\n```go\nfunc Hello() string\n```\n\n```go\nfmt.Println(tc.Hello())\n```\n\n"+
		"```go\npackage main\n\nimport \""+dep+"\"\n\nfunc main() { pkg.Do() }\n```\n")
	return dir
}

// TestUncheckableBlock: a Go block that imports a package go list cannot
// build is a DS-CODE finding that names the package, not a block that
// passes unchecked.
func TestUncheckableBlock(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "")
	dir := typecheckModule(t, "example.com/unreachable/pkg")
	code, out, errOut := run(t, "check", "--rules", "DS-CODE", dir)
	if code != ExitFindings || strings.Count(out, "DS-CODE") != 1 ||
		!strings.Contains(out, "API.md:12: DS-CODE: Go block cannot be type-checked: it imports example.com/unreachable/pkg: ") {
		t.Errorf("exit %d\n%s%s", code, out, errOut)
	}
}

// fakeGoEnv makes the test binary a go command that lists the standard
// library and fails everything else (TestScratchModuleRemoved).
const fakeGoEnv = "DOCSYNC_TEST_FAKE_GO"

func init() {
	if os.Getenv(fakeGoEnv) == "" {
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "list" && os.Args[2] == "std" {
		fmt.Println("fmt")
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "go: this go command lists only std")
	os.Exit(1)
}

// TestScratchModuleRemoved: the scratch module of the type check is removed
// when go list fails, as when it succeeds.
func TestScratchModuleRemoved(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"go list succeeds", nil, ""},
		{"go list fails", map[string]string{"DOCSYNC_GO": self, fakeGoEnv: "1"}, "go.mod: DS-CONF: cannot load the module to type-check Go blocks: go list: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			dir := typecheckModule(t, "fmt")
			code, out, errOut := run(t, "check", "--rules", "DS-CODE,DS-CONF", dir)
			if tc.want == "" && (code != ExitOK || out != "") || !strings.Contains(out, tc.want) {
				t.Errorf("exit %d, output lacks %q:\n%s%s", code, tc.want, out, errOut)
			}
			left, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range left {
				if strings.HasPrefix(e.Name(), "docsync-") {
					t.Errorf("left in the temporary directory: %s", filepath.Join(tmp, e.Name()))
				}
			}
		})
	}
}
