// SPDX-License-Identifier: MIT

package repofs

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// tree makes a directory root with the files of files (slash-separated
// name -> content) and a sibling directory outside it holding outside.md.
// It returns root and the outside directory.
func tree(t *testing.T, files map[string]string) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root, outside = filepath.Join(base, "root"), filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "outside.md"), []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, outside
}

func open(t *testing.T, dir string, lim Limits) *FS {
	t.Helper()
	f, err := Open(dir, lim)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func refusal(t *testing.T, err error, path string) {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) || re.Path != path {
		t.Fatalf("got %v, want a refusal of %q", err, path)
	}
}

// TestLimits covers the size of one file, the bytes of a run and the
// directory entries of a run, each at the limit and one past it.
func TestLimits(t *testing.T) {
	root, _ := tree(t, map[string]string{"a": "1234", "b": "12345", "d/x": "", "d/y": ""})
	lim := Limits{File: 4, Total: 1 << 20, Entries: 1 << 20}
	f := open(t, root, lim)
	if _, err := f.ReadFile("a"); err != nil {
		t.Errorf("a file of the limit: %v", err)
	}
	_, err := f.ReadFile("b")
	refusal(t, err, "b")

	f = open(t, root, Limits{File: 1 << 20, Total: 9, Entries: 1 << 20})
	if _, err := f.ReadFile("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFile("b"); err != nil {
		t.Errorf("a run of the limit: %v", err)
	}
	_, err = f.ReadFile("a")
	refusal(t, err, "a")

	// The root lists 3 entries, d 2 more.
	f = open(t, root, Limits{File: 1 << 20, Total: 1 << 20, Entries: 5})
	if err := f.Walk(func(string, fs.DirEntry) error { return nil }); err != nil {
		t.Errorf("a walk of the limit: %v", err)
	}
	_, err = f.ReadDir(".")
	refusal(t, err, ".")
}

// TestLargeFileNotRead: a file over the limit is refused from its size,
// before it is read.
func TestLargeFileNotRead(t *testing.T) {
	root, _ := tree(t, nil)
	p := filepath.Join(root, "big.md")
	fh, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := fh.Truncate(MaxFileSize + 1); err != nil { // sparse
		t.Fatal(err)
	}
	_ = fh.Close()
	f := open(t, root, Default)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = f.ReadFile("big.md")
	runtime.ReadMemStats(&after)
	refusal(t, err, "big.md")
	if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
		t.Errorf("refusing a file of %d bytes allocated %d bytes", MaxFileSize+1, n)
	}
}

// snapshot records every entry below dir: a file's content, a link's target.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			out[rel] = "link " + l
			return err
		case e.IsDir():
			out[rel] = "dir"
		default:
			b, err := os.ReadFile(p)
			out[rel] = "file " + string(b)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUpdate(t *testing.T) {
	root, _ := tree(t, map[string]string{"README.md": "r\n", "docs/api/old.md": "old\n", "docs/api/same.md": "same\n"})
	f := open(t, root, Default)
	wrote, removed, err := f.Update("docs/api", map[string][]byte{"new.md": []byte("new\n"), "same.md": []byte("same\n")}, []string{"old.md", "gone.md"})
	if err != nil || !slices.Equal(wrote, []string{"docs/api/new.md"}) || !slices.Equal(removed, []string{"docs/api/old.md"}) {
		t.Fatalf("wrote %q, removed %q, %v", wrote, removed, err)
	}
	got := snapshot(t, root)
	want := map[string]string{".": "dir", "README.md": "file r\n", "docs": "dir", "docs/api": "dir", "docs/api/new.md": "file new\n", "docs/api/same.md": "file same\n"}
	for k, v := range want {
		if got[filepath.FromSlash(k)] != v {
			t.Errorf("%s: %q, want %q", k, got[filepath.FromSlash(k)], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("tree %v", got)
	}

	// A missing directory is created.
	root, _ = tree(t, nil)
	f = open(t, root, Default)
	if wrote, _, err := f.Update("docs/api", map[string][]byte{"a.md": []byte("a")}, nil); err != nil || len(wrote) != 1 {
		t.Fatalf("%q %v", wrote, err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "docs", "api", "a.md")); err != nil || string(b) != "a" {
		t.Errorf("%q %v", b, err)
	}
}

// updateCase is a tree in which Update cannot prove that a write or
// removal stays a regular file inside docs/api.
type updateCase struct {
	name   string
	setup  func(t *testing.T, root, outside string)
	write  string
	remove string
	path   string // the path the refusal names
}

// checkUpdateRefusals: Update refuses, and nothing changes, inside the tree
// or outside it.
func checkUpdateRefusals(t *testing.T, cases []updateCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, outside := tree(t, map[string]string{"README.md": "readme\n", "notes/keep.md": "keep\n", "docs/api/b.md": "b\n"})
			tc.setup(t, root, outside)
			before, beforeOut := snapshot(t, root), snapshot(t, outside)
			f := open(t, root, Default)
			var remove []string
			if tc.remove != "" {
				remove = []string{tc.remove}
			}
			wrote, removed, err := f.Update("docs/api", map[string][]byte{tc.write: []byte("new\n")}, remove)
			refusal(t, err, tc.path)
			if len(wrote)+len(removed) != 0 {
				t.Errorf("changed %q %q", wrote, removed)
			}
			if after := snapshot(t, root); !maps.Equal(before, after) {
				t.Errorf("the tree changed:\n%v\n%v", before, after)
			}
			if after := snapshot(t, outside); !maps.Equal(beforeOut, after) {
				t.Errorf("outside changed:\n%v\n%v", beforeOut, after)
			}
		})
	}
}

// TestUpdateRefuses: a write or removal that cannot be proven to stay
// inside docs/api, or that meets anything but a regular file there, is
// refused, and nothing changes (links: TestUpdateRefusesLinks).
func TestUpdateRefuses(t *testing.T) {
	checkUpdateRefusals(t, []updateCase{
		{"a file is a directory", func(t *testing.T, root, _ string) {
			if err := os.MkdirAll(filepath.Join(root, "docs", "api", "a.md"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "a.md", "", "docs/api/a.md"},
		{"docs/api is a file", func(t *testing.T, root, _ string) {
			if err := os.RemoveAll(filepath.Join(root, "docs", "api")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "docs", "api"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "a.md", "", "docs/api"},
		{"a name with a separator", func(*testing.T, string, string) {}, "../x.md", "", "docs/x.md"},
		{"a removal outside", func(*testing.T, string, string) {}, "a.md", "../../README.md", "README.md"},
	})
}
