// SPDX-License-Identifier: MIT

package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDocs adds the Markdown documents of the fixture modules to the
// seed corpus.
func fixtureDocs(f *testing.F) {
	f.Helper()
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*", "*.md"))
	more, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*", "docs", "*.md"))
	for _, p := range append(paths, more...) {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
}

// FuzzParse: any input splits into lines, blocks, spans and table rows that
// lie inside it.
func FuzzParse(f *testing.F) {
	f.Add([]byte(doc))
	f.Add([]byte("```go\nx := 1\n```\n| a | `b|c` |\n|---|---|\n| \\| | d |\n"))
	f.Add([]byte("~~~~\n```\n~~~\n~~~~\n``` `` `\n"))
	fixtureDocs(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		d := Parse("f.md", data)
		n := len(d.Lines)
		if n == 0 || len(d.inBlock) != n {
			t.Fatalf("%d lines, %d block flags", n, len(d.inBlock))
		}
		if got := strings.Count(d.Text(), "\n") + 1; got != n {
			t.Fatalf("Text has %d lines, the document %d", got, n)
		}
		for _, b := range d.Blocks {
			if b.Start < 2 || b.Start > n+1 || b.Start-1+len(b.Lines) > n {
				t.Fatalf("block at %d with %d lines outside %d lines", b.Start, len(b.Lines), n)
			}
			for i, l := range b.Lines {
				if !strings.HasSuffix(d.Lines[b.Start-1+i], l) {
					t.Fatalf("block line %q is not the end of document line %q", l, d.Lines[b.Start-1+i])
				}
				if !d.InBlock(b.Start + i) {
					t.Fatalf("line %d of a block is not in a block", b.Start+i)
				}
			}
		}
		for _, s := range d.Spans() {
			if s.Line < 1 || s.Line > n || d.InBlock(s.Line) || !strings.Contains(d.Lines[s.Line-1], s.Text) {
				t.Fatalf("span %q at line %d is not in a line outside the blocks", s.Text, s.Line)
			}
		}
		for _, tbl := range d.Tables() {
			if len(tbl) < 2 {
				t.Fatalf("a table of %d rows", len(tbl))
			}
			for _, r := range tbl {
				if r.Line < 1 || r.Line > n || d.InBlock(r.Line) || len(r.Cells) == 0 {
					t.Fatalf("row at line %d with %d cells", r.Line, len(r.Cells))
				}
				size := 0
				for _, c := range r.Cells {
					if c != strings.TrimSpace(c) {
						t.Fatalf("cell %q is not trimmed", c)
					}
					size += len(c)
				}
				if size > len(d.Lines[r.Line-1]) {
					t.Fatalf("cells of %d bytes from a line of %d", size, len(d.Lines[r.Line-1]))
				}
			}
		}
	})
}
