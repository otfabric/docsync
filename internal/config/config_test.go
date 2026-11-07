// SPDX-License-Identifier: MIT

package config

import "testing"

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		g, p string
		want bool
	}{
		{"cmd/**", "cmd", true}, {"cmd/**", "cmd/x/y", true}, {"cmd/**", "cmdx", false},
		{"**/internal/**", "a/internal/b", true}, {"**/internal/**", "internal", true}, {"**/adr/**", "docs/adr/1.md", true},
		{"docs/*.md", "docs/a.md", true}, {"docs/*.md", "docs/a/b.md", false}, {"a?.md", "ab.md", true}, {"**", "x/y", true},
	} {
		if got := Match(tc.g, tc.p); got != tc.want {
			t.Errorf("Match(%q, %q) = %v", tc.g, tc.p, got)
		}
	}
}

func TestParse(t *testing.T) {
	c, err := Parse([]byte(`{"api": false, "errors_doc": "docs/ERR.md", "clis": [{"name": "x", "manifest": ".", "exit_doc": "E.md", "json_doc": "J.md", "docs": ["README.md"]}],
		"ignore": [{"rule": "*", "subject": "a.*", "reason": "why"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.HasAPI() || c.ErrorsDocPath() != "docs/ERR.md" || c.CLIs[0].ExitDocPath() != "E.md" || c.CLIs[0].JSONDocPath() != "J.md" || c.CLIs[0].DocGlobs()[0] != "README.md" {
		t.Errorf("%+v", c)
	}
	if !c.Ignored("DS-API", "a.b") || c.Ignored("DS-API", "b") {
		t.Error("Ignored")
	}
	var zero Config
	if !zero.HasAPI() || zero.ErrorsDocPath() != "ERRORS.md" || len(zero.APIDocGlobs()) == 0 || len(zero.SkipDirGlobs()) == 0 {
		t.Error("defaults")
	}
	for _, bad := range []string{`{"clis": [{"name": "x"}]}`, `{"ignore": [{"rule": "x", "subject": "[", "reason": "r"}]}`, `nope`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if _, err := Load("/nonexistent-dir-docsync"); err != nil {
		t.Errorf("a missing file is the zero config: %v", err)
	}
}
