// SPDX-License-Identifier: MIT

package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestScratchGoMod: the scratch module keeps every replace directive of the
// checked module, single or in a block, with a local target made absolute
// and the comment dropped.
func TestScratchGoMod(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator)+"src", "m")
	gomod := "module example.com/m // the module\n\ngo 1.26\n\n" +
		"replace example.com/a => ../a // a local copy\n" +
		"replace (\n" +
		"\texample.com/b v1.2.0 => ./third_party/b\n" +
		"\t// a comment line\n" +
		"\texample.com/c => example.com/c-fork v1.0.1 // a fork\n" +
		")\n" +
		"replace\texample.com/d => example.com/d-fork v0.1.0\n" +
		"// replace example.com/e => ../e\n"
	got := string(scratchGoMod("1.26", "example.com/m", abs, map[string]string{"example.com/z": "v1.0.0", "example.com/a": "v0.3.0"}, []byte(gomod)))
	want := "module docsync.check\n\ngo 1.26\n\nrequire example.com/m v0.0.0\n\nreplace example.com/m => " + abs + "\n" +
		"require example.com/a v0.3.0\n" +
		"require example.com/z v1.0.0\n" +
		"replace example.com/a => " + filepath.Join(abs, "../a") + "\n" +
		"replace example.com/b v1.2.0 => " + filepath.Join(abs, "./third_party/b") + "\n" +
		"replace example.com/c => example.com/c-fork v1.0.1\n" +
		"replace example.com/d => example.com/d-fork v0.1.0\n"
	if got != want {
		t.Errorf("scratch go.mod:\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "//") {
		t.Errorf("a comment is left: %s", got)
	}
}
