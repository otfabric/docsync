// SPDX-License-Identifier: MIT

package cli

import (
	"strings"
	"testing"

	"github.com/otfabric/docsync/internal/repofs"
)

// maxFileSize is the largest file docsync reads.
const maxFileSize = repofs.MaxFileSize

// TestReadBudget: a tree whose files together, or whose directory entries,
// exceed what one run reads fails with exit 10. The limits are lowered
// here; the real ones are far larger (repofs.Default).
func TestReadBudget(t *testing.T) {
	defer func(l repofs.Limits) { readLimits = l }(readLimits)
	files := map[string]string{"README.md": strings.Repeat("a", 600) + "\n", "docs/b.md": strings.Repeat("b", 600) + "\n"}
	for _, tc := range []struct {
		name string
		lim  repofs.Limits
		code int
	}{
		{"within every limit", repofs.Limits{File: 601, Total: 1202, Entries: 3}, ExitOK},
		{"one file over the file limit", repofs.Limits{File: 600, Total: 1 << 20, Entries: 100}, ExitInternal},
		{"the files over the run's limit", repofs.Limits{File: 1 << 20, Total: 1201, Entries: 100}, ExitInternal},
		{"the entries over the run's limit", repofs.Limits{File: 1 << 20, Total: 1 << 20, Entries: 2}, ExitInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := confinedTree(t, files)
			readLimits = tc.lim
			code, out, errOut := runBounded(t, "check", root)
			if code != tc.code {
				t.Fatalf("exit %d, want %d\n%s%s", code, tc.code, out, errOut)
			}
			if tc.code == ExitInternal && !strings.Contains(errOut, "docsync: ") {
				t.Errorf("no message: %q", errOut)
			}
		})
	}
}
