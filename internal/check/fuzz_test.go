// SPDX-License-Identifier: MIT

package check

import (
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/docsync/internal/markdown"
)

// fixtureBlocks adds the code blocks of the fixture modules' documents whose
// language is one of langs to the seed corpus.
func fixtureBlocks(f *testing.F, langs map[string]bool) {
	f.Helper()
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*", "*.md"))
	more, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*", "docs", "*.md"))
	for _, p := range append(paths, more...) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, blk := range markdown.Parse(p, b).Blocks {
			if langs[blk.Lang] {
				f.Add(strings.Join(blk.Lines, "\n"))
			}
		}
	}
}

// FuzzShellWords: a command line splits into words made of its own bytes,
// or is refused as a whole; the same line always splits the same way.
func FuzzShellWords(f *testing.F) {
	for _, s := range []string{`a "b c" 'd e' f\ g`, `a | b && c; d || e & f`, `a 2>&1 # rest`, `a "\"q\""`, `a "open`, `a 'open`, `x --flag=1 2>&- &`} {
		f.Add(s)
	}
	fixtureBlocks(f, shellLangs)
	f.Fuzz(func(t *testing.T, s string) {
		words, ok := shellWords(s)
		if !ok {
			if words != nil {
				t.Fatalf("refused with words %q", words)
			}
			return
		}
		size := 0
		for _, w := range words {
			size += len(w)
		}
		if size > len(s) {
			t.Fatalf("%d bytes of words from %d bytes", size, len(s))
		}
		if again, ok2 := shellWords(s); !ok2 || !slices.Equal(again, words) {
			t.Fatalf("a second split differs: %q, %q", again, words)
		}
		_ = cliInvocations("x", s)
	})
}

// FuzzPrepare: a Go block either does not parse or becomes a file whose
// every position maps to a line of the block.
func FuzzPrepare(f *testing.F) {
	for _, s := range []string{
		"package p\n\nfunc F() {}\n",
		"x := client.Read(ctx, ...)\nif err != nil {\n\treturn err\n}\n",
		"type T struct { ... }\nfunc (t *T) M(...) error\n",
		"import \"fmt\"\n\nfmt.Println(1)\n",
		"var (\n\tErrA // a\n\tErrB\n)\n",
		"Name: ...,\nAddr: \"x\",\n",
		"Read(ctx context.Context) error\n",
		"func F() {\n",
	} {
		f.Add(s)
	}
	fixtureBlocks(f, map[string]bool{"go": true, "golang": true})
	f.Fuzz(func(t *testing.T, src string) {
		d := markdown.Parse("f.md", []byte("```go\n"+src+"\n```\n"))
		if len(d.Blocks) == 0 {
			return
		}
		b := d.Blocks[0]
		g, err := prepare(d, b)
		if err != nil {
			if g != nil {
				t.Fatal("a block with an error")
			}
			return
		}
		if g.file == nil {
			t.Fatal("no file")
		}
		last := b.Start + max(len(b.Lines)-1, 0)
		ast.Inspect(g.file, func(n ast.Node) bool {
			if n == nil || !n.Pos().IsValid() {
				return true
			}
			if l := g.line(n.Pos()); l < b.Start || l > last {
				t.Fatalf("a position maps to line %d, outside the block's lines %d-%d", l, b.Start, last)
			}
			return true
		})
	})
}
