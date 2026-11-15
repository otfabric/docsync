// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/otfabric/docsync/internal/finding"
)

// TestOutputEscaping: file names and messages reach the annotations and the
// terminal with GitHub's workflow-command escaping and with control and
// bidirectional characters made visible; each finding stays one line. JSON
// carries them unchanged (it escapes them itself).
func TestOutputEscaping(t *testing.T) {
	fs := []finding.Finding{{
		Rule:    "DS-REF",
		File:    "docs/a,b:c\x1b[2J.md",
		Line:    3,
		Subject: "x",
		Message: "first\nsecond\r %0A \x1b[31mred\x07 \u202egnp.exe \u009b \x9b \xff end",
	}}
	raw := []string{"\x1b", "\x07", "\r", "\u202e", "\u009b", "\x9b", "\xff"}
	for _, tc := range []struct {
		format string
		lines  int // output lines for one finding
		want   []string
	}{
		{"github", 2, []string{
			"::error file=docs/a%2Cb%3Ac\\x1b[2J.md,line=3,title=DS-REF::first%0Asecond%0D %250A \\x1b[31mred\\x07 \\u202egnp.exe \\u009b \\x9b \\xff end\n",
		}},
		{"text", 2, []string{"docs/a,b:c\\x1b[2J.md:3: DS-REF: first\\x0asecond\\x0d %0A \\x1b[31mred\\x07 \\u202egnp.exe \\u009b \\x9b \\xff end\n"}},
		{"markdown", 5, []string{"| DS-REF | `docs/a,b:c\\x1b[2J.md:3` | first\\x0asecond\\x0d %0A \\x1b[31mred\\x07 \\u202egnp.exe \\u009b \\x9b \\xff end |\n"}},
	} {
		var b bytes.Buffer
		if err := write(&b, tc.format, fs); err != nil {
			t.Fatal(err)
		}
		out := b.String()
		if n := strings.Count(out, "\n"); n != tc.lines {
			t.Errorf("%s: %d lines, want %d:\n%s", tc.format, n, tc.lines, out)
		}
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output lacks\n%q\nin\n%q", tc.format, w, out)
			}
		}
		for _, r := range raw {
			if strings.Contains(out, r) {
				t.Errorf("%s: raw %q in the output", tc.format, r)
			}
		}
	}
	var b bytes.Buffer
	if err := write(&b, "json", fs); err != nil {
		t.Fatal(err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// JSON carries valid UTF-8: a byte that is not becomes U+FFFD.
	fs[0].Message = strings.ToValidUTF8(fs[0].Message, "\ufffd")
	if !reflect.DeepEqual(got, fs) {
		t.Errorf("json: %+v, want %+v", got, fs)
	}
	// Plain text is unchanged.
	b.Reset()
	plain := []finding.Finding{{Rule: "DS-REF", File: "docs/ä.md", Line: 1, Message: "tab\tand ünïcode, 100% `x`"}}
	if err := write(&b, "text", plain); err != nil || b.String() != "docs/ä.md:1: DS-REF: tab\tand ünïcode, 100% `x`\ndocsync: 1 finding(s)\n" {
		t.Errorf("plain text: %q, %v", b.String(), err)
	}
}
