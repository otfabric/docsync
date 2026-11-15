// SPDX-License-Identifier: MIT

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package repofs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The tests that need symbolic links, named pipes and devices.

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// replaceWithLink replaces the directory dir with a link to target.
func replaceWithLink(t *testing.T, target, dir string) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	symlink(t, target, dir)
}

func mkfifo(t *testing.T, p string) {
	t.Helper()
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadFile(t *testing.T) {
	root, outside := tree(t, map[string]string{"a.md": "a\n", "docs/b.md": "b\n", "dir.md/x": ""})
	symlink(t, "../a.md", filepath.Join(root, "docs", "in.md"))
	symlink(t, "../outside/outside.md", filepath.Join(root, "out.md"))
	symlink(t, filepath.Join(outside, "outside.md"), filepath.Join(root, "abs.md"))
	symlink(t, "missing.md", filepath.Join(root, "dangling.md"))
	f := open(t, root, Default)

	for rel, want := range map[string]string{"a.md": "a\n", "docs/b.md": "b\n", "docs/in.md": "a\n", "docs/../a.md": "a\n"} {
		if got, err := f.ReadFile(rel); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", rel, got, err)
		}
	}
	for _, rel := range []string{"missing.md", "dangling.md"} {
		if _, err := f.ReadFile(rel); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v, want fs.ErrNotExist", rel, err)
		}
	}
	if len(f.Refused()) != 0 {
		t.Fatalf("refused %v", f.Refused())
	}
	for _, rel := range []string{"out.md", "abs.md", "../outside/outside.md", filepath.ToSlash(filepath.Join(outside, "outside.md")), "dir.md", "a.md/x"} {
		_, err := f.ReadFile(rel)
		refusal(t, err, rel)
	}
	if n := len(f.Refused()); n != 6 {
		t.Errorf("%d refusals recorded, want 6", n)
	}
}

func TestReadFileNamedPipe(t *testing.T) {
	root, _ := tree(t, nil)
	mkfifo(t, filepath.Join(root, "pipe.md"))
	f := open(t, root, Default)
	_, err := f.ReadFile("pipe.md")
	refusal(t, err, "pipe.md")
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("reason: %v", err)
	}
}

func TestReadFileDevice(t *testing.T) {
	f := open(t, "/dev", Default)
	_, err := f.ReadFile("null")
	refusal(t, err, "null")
}

func TestWalk(t *testing.T) {
	root, _ := tree(t, map[string]string{"b.md": "", "a/x.md": "", "a/skip/y.md": "", "c/z.md": ""})
	symlink(t, "../c", filepath.Join(root, "a", "link"))
	f := open(t, root, Default)
	var got []string
	err := f.Walk(func(rel string, e fs.DirEntry) error {
		got = append(got, rel)
		if e.IsDir() && e.Name() == "skip" {
			return fs.SkipDir
		}
		return nil
	})
	want := []string{"a", "a/link", "a/skip", "a/x.md", "b.md", "c", "c/z.md"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("walk %q, %v; want %q", got, err, want)
	}
	if _, err := f.ReadDir("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing directory: %v", err)
	}
	_, err = f.ReadDir("b.md")
	refusal(t, err, "b.md")
	if !f.Exists("a/link") || f.Exists("nope") {
		t.Error("Exists")
	}
}

// TestUpdateRefusesLinks: Update follows no link, whether it leaves the
// tree or not, and changes nothing.
func TestUpdateRefusesLinks(t *testing.T) {
	checkUpdateRefusals(t, []updateCase{
		{"docs/api links out", func(t *testing.T, root, outside string) {
			replaceWithLink(t, outside, filepath.Join(root, "docs", "api"))
		}, "a.md", "", "docs/api"},
		{"docs links out", func(t *testing.T, root, outside string) {
			replaceWithLink(t, outside, filepath.Join(root, "docs"))
		}, "a.md", "", "docs"},
		{"docs/api links inside the tree", func(t *testing.T, root, _ string) {
			replaceWithLink(t, "../notes", filepath.Join(root, "docs", "api"))
		}, "a.md", "", "docs/api"},
		{"a file is a link", func(t *testing.T, root, _ string) {
			symlink(t, "../../README.md", filepath.Join(root, "docs", "api", "a.md"))
		}, "a.md", "", "docs/api/a.md"},
		{"a removal is a link", func(t *testing.T, root, _ string) {
			symlink(t, "../../README.md", filepath.Join(root, "docs", "api", "old.md"))
		}, "a.md", "old.md", "docs/api/old.md"},
	})
}
