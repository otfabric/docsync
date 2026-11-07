// SPDX-License-Identifier: MIT

package gitinfo

import "testing"

func TestParse(t *testing.T) {
	v, ok := Parse("v1.2.3")
	if !ok || v != (Version{1, 2, 3}) || v.String() != "v1.2.3" {
		t.Fatal(v, ok)
	}
	if _, ok := Parse("v1.2.3-rc1"); ok {
		t.Error("a pre-release is not a version")
	}
	for _, p := range [][2]string{{"v1.0.0", "v2.0.0"}, {"v1.1.0", "v1.2.0"}, {"v1.1.1", "v1.1.2"}} {
		a, _ := Parse(p[0])
		b, _ := Parse(p[1])
		if a.Compare(b) >= 0 || b.Compare(a) <= 0 || a.Compare(a) != 0 {
			t.Errorf("Compare %v", p)
		}
	}
	var r *Repo
	if r.Tags() != nil || r.HasTag(Version{}) {
		t.Error("nil repo")
	}
	if _, ok := r.Latest(); ok {
		t.Error("nil repo has no latest")
	}
	if _, ok := r.Added("x", "y"); ok {
		t.Error("nil repo has no history")
	}
	if Open("/nonexistent-dir-docsync") != nil {
		t.Error("not a work tree")
	}
}
