// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParse: a configuration Parse accepts is valid and survives a round
// trip through JSON unchanged; anything else is an error.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"api": false, "errors_doc": "docs/ERR.md", "clis": [{"name": "x", "manifest": ".", "exit_doc": "E.md"}], "ignore": [{"rule": "*", "subject": "a.*", "reason": "why"}]}`))
	f.Add([]byte(`{"ignore": [{"rule": "x", "subject": "[", "reason": "r"}]}`))
	f.Add([]byte(`{"clis": [{"name": "x"}]}`))
	f.Add([]byte(`{} trailing`))
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*", ".otfabric", "docsync.json"))
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data)
		if err != nil {
			if c != nil {
				t.Fatal("a configuration with an error")
			}
			if !strings.HasPrefix(err.Error(), File+": ") {
				t.Fatalf("error without the file name: %v", err)
			}
			return
		}
		for _, ig := range c.Ignore {
			if ig.Rule == "" || ig.Subject == "" || strings.TrimSpace(ig.Reason) == "" {
				t.Fatalf("accepted ignore %+v", ig)
			}
		}
		for _, cli := range c.CLIs {
			if cli.Name == "" || cli.Manifest == "" {
				t.Fatalf("accepted cli %+v", cli)
			}
		}
		b1, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		c2, err := Parse(b1)
		if err != nil {
			t.Fatalf("the round trip %s does not parse: %v", b1, err)
		}
		b2, err := json.Marshal(c2)
		if err != nil || !bytes.Equal(b1, b2) {
			t.Fatalf("round trip changed it:\n%s\n%s", b1, b2)
		}
	})
}
