// SPDX-License-Identifier: MIT

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The tests that need symbolic links and named pipes.

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

func fifo(t *testing.T, name string) {
	t.Helper()
	if err := syscall.Mkfifo(name, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRefusedLinksAndPipes: a link that leaves the checked directory and a
// named pipe fail the run with exit 10, at once, naming the file.
func TestRefusedLinksAndPipes(t *testing.T) {
	checkRefusals(t, []refusalCase{
		{"a document links out of the tree", nil, func(t *testing.T, root, _ string) {
			link(t, "../outside/outside.md", filepath.Join(root, "README.md"))
		}, "README.md"},
		{"a document links to an absolute path", nil, func(t *testing.T, root, outside string) {
			link(t, filepath.Join(outside, "outside.md"), filepath.Join(root, "docs", "guide.md"))
		}, "docs/guide.md"},
		{"SECURITY.md links out of the tree", map[string]string{".otfabric/docsync.json": `{"exclude_docs": ["SECURITY.md"]}`}, func(t *testing.T, root, _ string) {
			link(t, "../outside/outside.md", filepath.Join(root, "SECURITY.md"))
		}, "SECURITY.md"},
		{"a Go file links out of the tree", map[string]string{"go.mod": refusalModule}, func(t *testing.T, root, _ string) {
			link(t, "../outside/outside.go", filepath.Join(root, "m.go"))
		}, "m.go"},
		{"the configuration links out of the tree", nil, func(t *testing.T, root, _ string) {
			link(t, "../../outside/docsync.json", filepath.Join(root, ".otfabric", "docsync.json"))
		}, ".otfabric/docsync.json"},
		{"exit_codes leaves the tree", map[string]string{".otfabric/docsync.json": `{"api": false, "clis": [{"name": "x", "manifest": ".", "exit_codes": "../outside"}]}`}, nil, "../outside"},
		{"a document is a named pipe", nil, func(t *testing.T, root, _ string) {
			fifo(t, filepath.Join(root, "README.md"))
		}, "README.md"},
		{"a Go file is a named pipe", map[string]string{"go.mod": refusalModule}, func(t *testing.T, root, _ string) {
			fifo(t, filepath.Join(root, "m.go"))
		}, "m.go"},
		{"a named pipe with a control character in its name", nil, func(t *testing.T, root, _ string) {
			fifo(t, filepath.Join(root, "a\x1b[31m.md"))
		}, "a\x1b[31m.md"},
	})
}

// TestConfinedTreePasses: a tree whose links resolve inside it still
// passes.
func TestConfinedTreePasses(t *testing.T) {
	root, _ := confinedTree(t, map[string]string{
		"go.mod":       refusalModule,
		"m.go":         "// Package m is m.\npackage m\n\n// Hello says hello.\nfunc Hello() string { return \"hi\" }\n",
		"API.md":       "# API\n\n```go\nfunc Hello() string\n```\n",
		"README.md":    "# m\n\nRequires Go 1.26 or later.\n",
		"docs/x/y.txt": "",
	})
	link(t, "../README.md", filepath.Join(root, "docs", "guide.md"))
	link(t, "x", filepath.Join(root, "docs", "dir"))
	code, out, errOut := runBounded(t, "check", root)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
}

// TestAPIConfinedLinks: docsync api follows no link: a linked docs or
// docs/api, or a generated file that is a link, exits 10 and changes
// nothing.
func TestAPIConfinedLinks(t *testing.T) {
	checkAPIRefusals(t, []apiCase{
		{"docs/api links out of the tree", func(t *testing.T, root, outside string) {
			link(t, outside, filepath.Join(root, "docs", "api"))
		}, "docs/api"},
		{"docs links out of the tree", func(t *testing.T, root, outside string) {
			link(t, outside, filepath.Join(root, "docs"))
		}, "docs"},
		{"docs/api links into the tree", func(t *testing.T, root, _ string) {
			link(t, "../notes", filepath.Join(root, "docs", "api"))
		}, "docs/api"},
		{"a generated file is a link into the tree", func(t *testing.T, root, _ string) {
			link(t, "../../README.md", filepath.Join(root, "docs", "api", "ref.md"))
		}, "docs/api/ref.md"},
	})
}
