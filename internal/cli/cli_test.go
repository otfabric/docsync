// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/docsync/internal/check"
	"github.com/otfabric/docsync/internal/finding"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s:\n--- got\n%s\n--- want\n%s", p, got, want)
	}
}

// TestLibraryFixture runs every library check on testdata/lib.
func TestLibraryFixture(t *testing.T) {
	code, out, errOut := run(t, "check", "../../testdata/lib")
	if code != ExitFindings {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	golden(t, "lib.txt", out)
}

// TestCLIFixture runs the CLI checks on testdata/cli.
func TestCLIFixture(t *testing.T) {
	code, out, errOut := run(t, "check", "../../testdata/cli")
	if code != ExitFindings {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	golden(t, "cli.txt", out)
}

func TestFormats(t *testing.T) {
	for _, f := range []string{"json", "github", "markdown"} {
		code, out, _ := run(t, "check", "--format", f, "--rules", "DS-EXIT", "../../testdata/cli")
		if code != ExitFindings {
			t.Fatalf("%s: exit %d", f, code)
		}
		golden(t, "cli-"+f+".txt", out)
	}
	var fs []finding.Finding
	_, out, _ := run(t, "check", "--format", "json", "--rules", "DS-EXIT", "../../testdata/cli")
	if err := json.Unmarshal([]byte(out), &fs); err != nil || len(fs) == 0 {
		t.Fatalf("json output: %v, %d findings", err, len(fs))
	}
}

func TestCleanModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/clean\n\ngo 1.26.0\n")
	writeFile(t, dir, "clean.go", "// Package clean is clean.\npackage clean\n\n// Hello says hello.\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, dir, "API.md", "# API\n\n```go\nfunc Hello() string\n```\n\n```go\nfmt.Println(clean.Hello())\n```\n")
	writeFile(t, dir, "README.md", "# clean\n\nRequires Go 1.26 or later.\n")
	for _, f := range []string{"text", "json", "markdown", "github"} {
		code, out, errOut := run(t, "check", "--format", f, dir)
		if code != ExitOK {
			t.Fatalf("%s: exit %d: %s %s", f, code, out, errOut)
		}
		if f == "json" && strings.TrimSpace(out) != "[]" {
			t.Errorf("json: %q", out)
		}
	}
}

func TestNoGoModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "Mail info@otfabric.com.\n")
	code, out, _ := run(t, "check", dir)
	if code != ExitFindings || !strings.Contains(out, "DS-SEC") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"check", "--format", "xml"}, {"check", "a", "b"}, {"check", "--nope"}, {"template", "a", "b"}} {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, ExitUsage)
		}
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"version"}, {"rules"}} {
		if code, out, _ := run(t, args...); code != ExitOK || out == "" {
			t.Errorf("%v: exit %d, output %q", args, code, out)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".otfabric/docsync.json", `{"ignore":[{"rule":"DS-API","subject":"x"}]}`)
	if code, _, errOut := run(t, "check", dir); code != ExitInvalidInput {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	writeFile(t, dir, ".otfabric/docsync.json", `{"bogus": 1}`)
	if code, _, _ := run(t, "check", dir); code != ExitInvalidInput {
		t.Fatalf("unknown field: exit %d", code)
	}
}

func TestBrokenGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "go 1.26.0\n")
	if code, _, _ := run(t, "check", dir); code != ExitInternal {
		t.Fatalf("exit %d, want %d", code, ExitInternal)
	}
}

func TestTemplate(t *testing.T) {
	code, out, _ := run(t, "template", "cmd")
	if code != ExitOK || !strings.Contains(out, "\npackage cmd\n") || !strings.Contains(out, ManifestMarker) ||
		!strings.Contains(out, "// docsync:root") || !strings.Contains(out, "func TestDocsyncManifest") {
		t.Fatalf("template: exit %d\n%s", code, out)
	}
}

// TestGitChecks covers the checks that read tags and history: the
// supported-versions table and an untagged release section.
func TestGitChecks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	writeFile(t, dir, "SECURITY.md", "Report to security@otfabric.io.\n\n| Version | Supported |\n|---|---|\n| 0.1.x | Yes |\n")
	writeFile(t, dir, "RELEASE.md", "# Releases\n\n## v0.2.0\n\n- New.\n\n---\n\n## v0.1.0\n\n- First.\n")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	git("tag", "v0.1.0")
	git("tag", "v0.2.0-rc1") // not a release
	git("tag", "v0.3.0")
	defer func(f func() time.Time) { check.Now = f }(check.Now)
	check.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	_, out, _ := run(t, "check", dir)
	for _, want := range []string{"does not support 0.3.x (latest release v0.3.0)", "v0.2.0 has release notes since 2026-01-01 but is not tagged"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	git("tag", "v0.2.0")
	writeFile(t, dir, "SECURITY.md", "Report to security@otfabric.io.\n\n| Version | Supported |\n|---|---|\n| 0.3.x | ✅ |\n")
	if code, out, _ := run(t, "check", dir); code != ExitOK {
		t.Errorf("after the fix: exit %d\n%s", code, out)
	}
	// A version cell may carry a note after the version.
	for _, cell := range []string{"0.3.x (latest: v0.3.0)", "`0.3.x` (latest release, v0.3.0)"} {
		writeFile(t, dir, "SECURITY.md", "Report to security@otfabric.io.\n\n| Version | Supported |\n|---|---|\n| "+cell+" | Yes |\n")
		if code, out, _ := run(t, "check", dir); code != ExitOK {
			t.Errorf("%s: exit %d\n%s", cell, code, out)
		}
	}
	writeFile(t, dir, "SECURITY.md", "Report to security@otfabric.io.\n\n| Version | Supported |\n|---|---|\n| 0.3.xy | Yes |\n")
	if _, out, _ := run(t, "check", dir); !strings.Contains(out, "does not support 0.3.x") {
		t.Errorf("0.3.xy is not 0.3.x:\n%s", out)
	}
	// A "Latest release" row supports whichever minor is the latest.
	writeFile(t, dir, "SECURITY.md", "Report to security@otfabric.io.\n\n| Version | Supported |\n|---|---|\n| Latest release | Yes |\n| Older releases | No |\n")
	if code, out, _ := run(t, "check", dir); code != ExitOK {
		t.Errorf("latest-release row: exit %d\n%s", code, out)
	}
}

func TestAPIReference(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/ref\n\ngo 1.26\n")
	writeFile(t, dir, "ref.go", "// Package ref is the fixture of docsync api.\npackage ref\n\n// Max is the largest size.\nconst Max = 8\n\n// T is a thing. See [New] and [T.Size].\ntype T struct {\n\tName string // its name\n\tsecret int\n}\n\n// New returns a [T].\nfunc New() *T { return &T{} }\n\n// Size reports the size.\nfunc (t *T) Size() int { return Max }\n")
	writeFile(t, dir, "ref_linux.go", "package ref\n\n// Platform names the platform on Linux.\nfunc Platform() string { return \"linux\" }\n")
	writeFile(t, dir, "ref_windows.go", "package ref\n\n// Platform names the platform on Windows.\nfunc Platform() string { return \"windows\" }\n\n// WinOnly exists for one platform.\nfunc WinOnly() {}\n")
	writeFile(t, dir, "sub/sub.go", "// Package sub is a subpackage.\npackage sub\n\n// F does nothing.\nfunc F() {}\n")
	writeFile(t, dir, "internal/x/x.go", "package x\n\n// Hidden is internal.\nfunc Hidden() {}\n")
	if code, out, errOut := run(t, "api", dir); code != ExitOK {
		t.Fatalf("api: exit %d\n%s%s", code, out, errOut)
	}
	b, err := os.ReadFile(filepath.Join(dir, "docs/api/ref.md"))
	if err != nil {
		t.Fatal(err)
	}
	ref := string(b)
	for _, want := range []string{"<!-- Code generated by docsync api. DO NOT EDIT. -->", "# package ref", "const Max = 8", "### type T",
		"Name string // its name", "#### func New", "#### func (T) Size", "[New](#func-new)", "[T.Size](#func-t-size)", "on Linux", "### func WinOnly"} {
		if !strings.Contains(ref, want) {
			t.Errorf("ref.md lacks %q:\n%s", want, ref)
		}
	}
	for _, not := range []string{"secret", "on Windows", "Hidden"} {
		if strings.Contains(ref, not) {
			t.Errorf("ref.md has %q", not)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "docs/api/sub.md")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs/api/internal-x.md")); err == nil {
		t.Error("internal package documented")
	}
	if code, out, _ := run(t, "check", dir); code != ExitOK {
		t.Errorf("check after api: exit %d\n%s", code, out)
	}
	// The code changes: DS-GEN until docsync api runs again.
	writeFile(t, dir, "sub/sub.go", "// Package sub is a subpackage.\npackage sub\n\n// F does something now.\nfunc F() {}\n")
	if _, out, _ := run(t, "check", dir); !strings.Contains(out, "DS-GEN: docs/api/sub.md is out of date") {
		t.Errorf("no DS-GEN:\n%s", out)
	}
	if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := run(t, "check", dir); !strings.Contains(out, "documents a package that is gone") {
		t.Errorf("no DS-GEN for a removed package:\n%s", out)
	}
	if _, out, _ := run(t, "api", dir); !strings.Contains(out, "removed docs/api/sub.md") {
		t.Errorf("api did not remove sub.md:\n%s", out)
	}
	if code, out, _ := run(t, "check", dir); code != ExitOK {
		t.Errorf("check after the second api: exit %d\n%s", code, out)
	}
	if code, _, _ := run(t, "api", dir, "extra"); code != ExitUsage {
		t.Errorf("api with two directories: exit %d", code)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
