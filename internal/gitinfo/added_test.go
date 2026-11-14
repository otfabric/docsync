// SPDX-License-Identifier: MIT

package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestAdded dates a heading from the commit that added it as it is now: a
// heading that was added, removed and added again dates from the last
// addition, and one that is back but not committed has no date.
func TestAdded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	top := t.TempDir()
	// The checked directory is a nested module, as in a CLI under cmd/.
	dir := filepath.Join(top, "cmd", "tool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(date string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = top
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "RELEASE.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(date, msg string) {
		t.Helper()
		git(date, "add", ".")
		git(date, "commit", "-qm", msg)
	}
	const head = "## v1.1.0"
	git("2026-01-01T00:00:00Z", "init", "-q")
	write("# Releases\n\n## v1.0.0\n\n- First.\n")
	commit("2026-01-01T00:00:00Z", "v1.0.0")
	r := Open(dir)
	if _, ok := r.Added("RELEASE.md", head); ok {
		t.Fatal("never added: no date")
	}

	write("# Releases\n\n## v1.1.0\n\n- Second.\n\n## v1.0.0\n\n- First.\n")
	commit("2026-01-02T00:00:00Z", "add v1.1.0")
	want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if at, ok := r.Added("RELEASE.md", head); !ok || !at.Equal(want) {
		t.Fatalf("added once: %v %v, want %v", at, ok, want)
	}

	// Folded away, then back without a commit.
	write("# Releases\n\n## v1.0.0\n\n- First.\n")
	commit("2026-01-03T00:00:00Z", "fold v1.1.0")
	write("# Releases\n\n## v1.1.0\n\n- Third.\n\n## v1.0.0\n\n- First.\n")
	if at, ok := r.Added("RELEASE.md", head); ok {
		t.Fatalf("back but not committed: %v, want no date", at)
	}

	commit("2026-01-09T00:00:00Z", "add v1.1.0 again")
	want = time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)
	if at, ok := r.Added("RELEASE.md", head); !ok || !at.Equal(want) {
		t.Fatalf("added again: %v %v, want %v (the last addition, not the first)", at, ok, want)
	}

	// An edit that keeps the heading does not move its date.
	write("# Releases\n\n## v1.1.0\n\n- Third, reworded.\n\n## v1.0.0\n\n- First.\n")
	commit("2026-01-10T00:00:00Z", "reword")
	if at, ok := r.Added("RELEASE.md", head); !ok || !at.Equal(want) {
		t.Fatalf("after an edit: %v %v, want %v", at, ok, want)
	}
}
