// SPDX-License-Identifier: MIT

// Package e2e builds the docsync binary (with -race when the tests run with
// it) and runs it as a user does: on the fixture modules of testdata/, whose
// expected output is the golden files of internal/cli, and on trees the
// tests make.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// bin is the docsync binary under test.
var bin string

func TestMain(m *testing.M) {
	os.Exit(build(m))
}

func build(m *testing.M) int {
	dir, err := os.MkdirTemp("", "docsync-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	bin = filepath.Join(dir, "docsync")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	args := []string{"build", "-buildvcs=false", "-ldflags", "-X main.version=v0.0.0-e2e", "-o", bin}
	if raceEnabled {
		args = append(args, "-race")
	}
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go %s: %v\n%s", strings.Join(args, " "), err, out)
		return 1
	}
	return m.Run()
}

// docsync runs the binary and returns its exit code and output.
func docsync(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, stdout.String(), stderr.String()
	case errors.As(err, &ee):
		return ee.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("docsync %v: %v", args, err)
	return 0, "", ""
}

// golden returns a golden file of internal/cli, with line ends normalised
// (a Windows checkout writes CRLF).
func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "internal", "cli", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCommands(t *testing.T) {
	if code, out, _ := docsync(t, "version"); code != 0 || out != "docsync v0.0.0-e2e\n" {
		t.Errorf("version: exit %d, %q", code, out)
	}
	code, out, _ := docsync(t, "rules")
	if code != 0 || strings.Count(out, "\n") != 15 || !strings.HasPrefix(out, "DS-API ") || !strings.Contains(out, "\nDS-CONF ") {
		t.Errorf("rules: exit %d\n%s", code, out)
	}
	if code, out, _ := docsync(t, "help"); code != 0 || !strings.Contains(out, "Usage:") {
		t.Errorf("help: exit %d", code)
	}
	if code, out, _ := docsync(t, "template", "cmd"); code != 0 || !strings.Contains(out, "\npackage cmd\n") || !strings.Contains(out, "docsync manifest v1") {
		t.Errorf("template: exit %d", code)
	}
	for _, args := range [][]string{nil, {"bogus"}, {"check", "--format", "xml"}, {"check", "a", "b"}, {"api", "--x"}} {
		if code, _, errOut := docsync(t, args...); code != 2 || errOut == "" {
			t.Errorf("%v: exit %d, want 2 with a message", args, code)
		}
	}
}

// TestFixtures: the binary reports on the fixture modules exactly what the
// in-process tests record in the golden files.
func TestFixtures(t *testing.T) {
	for _, tc := range []struct {
		golden string
		args   []string
	}{
		{"lib.txt", []string{"check", fixture(t, "lib")}},
		{"cli.txt", []string{"check", fixture(t, "cli")}},
		{"cli-json.txt", []string{"check", "--format", "json", "--rules", "DS-EXIT", fixture(t, "cli")}},
		{"cli-github.txt", []string{"check", "--format", "github", "--rules", "DS-EXIT", fixture(t, "cli")}},
		{"cli-markdown.txt", []string{"check", "--format", "markdown", "--rules", "DS-EXIT", fixture(t, "cli")}},
	} {
		code, out, errOut := docsync(t, tc.args...)
		if code != 1 {
			t.Errorf("%s: exit %d, want 1\n%s", tc.golden, code, errOut)
		}
		if want := golden(t, tc.golden); out != want {
			t.Errorf("%s: the binary's output differs:\n--- got\n%s\n--- want\n%s", tc.golden, out, want)
		}
	}
	_, out, _ := docsync(t, "check", "--format", "json", fixture(t, "cli"))
	var fs []map[string]any
	if err := json.Unmarshal([]byte(out), &fs); err != nil || len(fs) == 0 {
		t.Errorf("json: %v, %d findings", err, len(fs))
	}
}

// TestExitCodes: the codes of docs/exitcodes.md, from the binary.
func TestExitCodes(t *testing.T) {
	clean := t.TempDir()
	writeFile(t, clean, "go.mod", "module example.com/clean\n\ngo 1.26.0\n")
	writeFile(t, clean, "clean.go", "// Package clean is clean.\npackage clean\n\n// Hello says hello.\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, clean, "API.md", "# API\n\n```go\nfunc Hello() string\n```\n")
	if code, out, errOut := docsync(t, "check", clean); code != 0 || out != "" {
		t.Errorf("clean module: exit %d\n%s%s", code, out, errOut)
	}
	if code, out, _ := docsync(t, "check", "--format", "json", clean); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("clean module, json: exit %d, %q", code, out)
	}

	findings := t.TempDir()
	writeFile(t, findings, "README.md", "Mail info@otfabric.com.\n")
	if code, out, _ := docsync(t, "check", findings); code != 1 || !strings.Contains(out, "DS-SEC") {
		t.Errorf("findings: exit %d\n%s", code, out)
	}

	config := t.TempDir()
	writeFile(t, config, ".otfabric/docsync.json", `{"bogus": 1}`)
	if code, _, errOut := docsync(t, "check", config); code != 3 || errOut == "" {
		t.Errorf("invalid configuration: exit %d", code)
	}

	broken := t.TempDir()
	writeFile(t, broken, "go.mod", "go 1.26.0\n")
	if code, _, errOut := docsync(t, "check", broken); code != 10 || errOut == "" {
		t.Errorf("unreadable module: exit %d", code)
	}

	// A document that links out of the checked directory is refused.
	base := t.TempDir()
	writeFile(t, base, "outside.md", "# Outside\n")
	if err := os.MkdirAll(filepath.Join(base, "root"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "outside.md"), filepath.Join(base, "root", "README.md")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		t.Log("no symbolic links here: the refusal is not run")
		return
	}
	if code, _, errOut := docsync(t, "check", filepath.Join(base, "root")); code != 10 || !strings.Contains(errOut, `"README.md"`) {
		t.Errorf("a document outside the tree: exit %d\n%s", code, errOut)
	}
}

// TestAPI: docsync api writes the reference that docsync check then finds
// current.
func TestAPI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/ref\n\ngo 1.26\n")
	writeFile(t, dir, "ref.go", "// Package ref is a fixture.\npackage ref\n\n// F does nothing.\nfunc F() {}\n")
	if code, out, errOut := docsync(t, "api", dir); code != 0 || !strings.Contains(out, "wrote docs/api/ref.md") {
		t.Fatalf("api: exit %d\n%s%s", code, out, errOut)
	}
	if code, out, errOut := docsync(t, "check", dir); code != 0 {
		t.Errorf("check after api: exit %d\n%s%s", code, out, errOut)
	}
	if code, out, _ := docsync(t, "api", dir); code != 0 || out != "" {
		t.Errorf("a second api run changes nothing: exit %d, %q", code, out)
	}
}
