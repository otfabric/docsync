// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The bounds a refused run must stay within: it returns at once, without
// reading the file it refuses.
const (
	refuseDeadline = 60 * time.Second
	refuseAlloc    = 16 << 20
)

// runBounded runs docsync and fails the test unless it returns within
// refuseDeadline and allocates less than refuseAlloc bytes.
func runBounded(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	type result struct {
		code        int
		out, errOut string
		alloc       uint64
	}
	done := make(chan result, 1)
	go func() {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		runtime.ReadMemStats(&after)
		done <- result{code, stdout.String(), stderr.String(), after.TotalAlloc - before.TotalAlloc}
	}()
	select {
	case r := <-done:
		if r.alloc > refuseAlloc {
			t.Errorf("docsync %v allocated %d bytes, more than %d", args, r.alloc, refuseAlloc)
		}
		return r.code, r.out, r.errOut
	case <-time.After(refuseDeadline):
		t.Fatalf("docsync %v did not return within %v", args, refuseDeadline)
	}
	return 0, "", ""
}

// confinedTree makes a checked directory root holding files, and a sibling
// directory outside it with outside.md, outside.go and docsync.json.
func confinedTree(t *testing.T, files map[string]string) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root, outside = filepath.Join(base, "root"), filepath.Join(base, "outside")
	writeFile(t, outside, "outside.md", "# Outside\n\nMail info@otfabric.com.\n")
	writeFile(t, outside, "outside.go", "package outside\n")
	writeFile(t, outside, "docsync.json", `{"api": false}`)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writeFile(t, root, name, content)
	}
	return root, outside
}

const refusalModule = "module example.com/m\n\ngo 1.26.0\n"

// refusalCase is a checked tree with a file that docsync refuses to read.
type refusalCase struct {
	name  string
	files map[string]string
	setup func(t *testing.T, root, outside string)
	path  string // the path the message names
}

// checkRefusals: each tree fails the run with exit 10, at once and without
// reading the refused file, and the message names the file, escaped.
func checkRefusals(t *testing.T, cases []refusalCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, outside := confinedTree(t, tc.files)
			if tc.setup != nil {
				tc.setup(t, root, outside)
			}
			for _, format := range []string{"text", "github"} {
				code, out, errOut := runBounded(t, "check", "--format", format, root)
				if code != ExitInternal {
					t.Fatalf("%s: exit %d, want %d\nstdout %s\nstderr %s", format, code, ExitInternal, out, errOut)
				}
				if !strings.Contains(errOut, strconv.Quote(tc.path)) {
					t.Errorf("%s: stderr does not name %s:\n%s", format, strconv.Quote(tc.path), errOut)
				}
				if strings.ContainsAny(errOut+out, "\x1b\x00") {
					t.Errorf("%s: raw control characters in the output: %q %q", format, out, errOut)
				}
			}
			if code, _, errOut := runBounded(t, "api", root); code != ExitInternal && code != ExitInvalidInput {
				t.Errorf("api: exit %d\n%s", code, errOut)
			}
		})
	}
}

// TestRefusedFiles: a directory where a file is expected and a file over
// the size limit fail the run with exit 10 (links and named pipes:
// TestRefusedLinksAndPipes).
func TestRefusedFiles(t *testing.T) {
	checkRefusals(t, []refusalCase{
		{"the configuration is a directory", map[string]string{".otfabric/docsync.json/x": ""}, nil, ".otfabric/docsync.json"},
		{"SECURITY.md is a directory", map[string]string{"SECURITY.md/x": ""}, nil, "SECURITY.md"},
		{"RELEASE.md is a directory", map[string]string{"RELEASE.md/x": ""}, nil, "RELEASE.md"},
		{"a document is larger than the limit", nil, func(t *testing.T, root, _ string) {
			sparse(t, filepath.Join(root, "README.md"), maxFileSize+1)
		}, "README.md"},
		{"a Go file is larger than the limit", map[string]string{"go.mod": refusalModule}, func(t *testing.T, root, _ string) {
			sparse(t, filepath.Join(root, "m.go"), maxFileSize+1)
		}, "m.go"},
	})
}

// TestRefusedGoSum: the type check copies go.sum into its scratch module;
// a go.sum it refuses to read fails the run with exit 10.
func TestRefusedGoSum(t *testing.T) {
	root, _ := confinedTree(t, map[string]string{"go.mod": refusalModule, "m.go": "package m\n", "README.md": "```go\nfmt.Println(1)\n```\n", "go.sum/x": ""})
	code, out, errOut := runBounded(t, "check", root)
	if code != ExitInternal || !strings.Contains(errOut, `"go.sum"`) {
		t.Fatalf("exit %d, want %d\n%s%s", code, ExitInternal, out, errOut)
	}
}

func sparse(t *testing.T, name string, size int64) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// snapshotTree records every entry below dir: a file's content, a link's
// target, a directory.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
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

// outsideAPI drops the entries under docs/api/ of a snapshot.
func outsideAPI(s map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range s {
		if k != "docs/api" && !strings.HasPrefix(k, "docs/api/") {
			out[k] = v
		}
	}
	return out
}

// apiFiles is the module of the docsync api tests.
var apiFiles = map[string]string{
	"go.mod":        "module example.com/ref\n\ngo 1.26\n",
	"ref.go":        "// Package ref is a fixture.\npackage ref\n\n// F does nothing.\nfunc F() {}\n",
	"README.md":     "# ref\n",
	"notes/keep.md": "# Kept by hand\n",
}

// apiCase is a tree in which docsync api cannot prove that a path it would
// write or remove is a regular file inside docs/api/.
type apiCase struct {
	name  string
	setup func(t *testing.T, root, outside string)
	path  string // the path the message names
}

// checkAPIRefusals: docsync api exits 10 and changes nothing, in the tree
// or outside it.
func checkAPIRefusals(t *testing.T, cases []apiCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, outside := confinedTree(t, apiFiles)
			tc.setup(t, root, outside)
			before, beforeOut := snapshotTree(t, root), snapshotTree(t, outside)
			code, out, errOut := runBounded(t, "api", root)
			if code != ExitInternal {
				t.Errorf("exit %d, want %d\n%s%s", code, ExitInternal, out, errOut)
			}
			if !strings.Contains(errOut, strconv.Quote(tc.path)) {
				t.Errorf("stderr does not name %s:\n%s", strconv.Quote(tc.path), errOut)
			}
			if after := snapshotTree(t, root); !maps.Equal(before, after) {
				t.Errorf("the tree changed:\n%v\n%v", before, after)
			}
			if after := snapshotTree(t, outside); !maps.Equal(beforeOut, after) {
				t.Errorf("files outside the tree changed:\n%v\n%v", beforeOut, after)
			}
		})
	}
}

// TestAPIConfined: docsync api changes nothing outside docs/api/, byte for
// byte, and exits 10 without changing anything when a path it would write
// or remove is not a regular file inside docs/api/ (links:
// TestAPIConfinedLinks).
func TestAPIConfined(t *testing.T) {
	t.Run("writes only docs/api", func(t *testing.T) {
		root, outside := confinedTree(t, apiFiles)
		writeFile(t, root, "docs/guide.md", "# Guide\n")
		before, beforeOut := snapshotTree(t, root), snapshotTree(t, outside)
		if code, out, errOut := run(t, "api", root); code != ExitOK || !strings.Contains(out, "wrote docs/api/ref.md") {
			t.Fatalf("exit %d\n%s%s", code, out, errOut)
		}
		writeFile(t, root, "docs/api/old.md", "<!-- Code generated by docsync api. DO NOT EDIT. -->\n\n# gone\n")
		if code, out, errOut := run(t, "api", root); code != ExitOK || !strings.Contains(out, "removed docs/api/old.md") {
			t.Fatalf("second run: exit %d\n%s%s", code, out, errOut)
		}
		if after := outsideAPI(snapshotTree(t, root)); !maps.Equal(outsideAPI(before), after) {
			t.Errorf("files outside docs/api changed:\n%v\n%v", before, after)
		}
		if after := snapshotTree(t, outside); !maps.Equal(beforeOut, after) {
			t.Errorf("files outside the tree changed")
		}
	})
	checkAPIRefusals(t, []apiCase{
		{"a generated file is a directory", func(t *testing.T, root, _ string) {
			writeFile(t, root, "docs/api/ref.md/x", "")
		}, "docs/api/ref.md"},
		{"docs/api is a file", func(t *testing.T, root, _ string) {
			writeFile(t, root, "docs/api", "")
		}, "docs/api"},
		{"a marker file outside docs/api", func(t *testing.T, root, _ string) {
			writeFile(t, root, "notes/old-api.md", "<!-- Code generated by docsync api. DO NOT EDIT. -->\n\n# Old\n")
		}, "notes/old-api.md"},
	})
}
