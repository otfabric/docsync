// SPDX-License-Identifier: MIT

package markdown

import (
	"slices"
	"testing"
)

const doc = "# T\n\nText `a.B` and ``c `d` e``.\n\n- item:\n  ```Go {.x}\n  x := 1\n  ```\n\n~~~\nraw `no`\n~~~\n\n| A | B |\n|---|:--:|\n| `x|y` | b\\|c |\n\n```\nunclosed\n"

func TestParse(t *testing.T) {
	d := Parse("d.md", []byte(doc))
	if len(d.Blocks) != 3 || d.Blocks[0].Lang != "go" || d.Blocks[0].Start != 7 || d.Blocks[0].Lines[0] != "x := 1" || d.Blocks[1].Lang != "" {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	if !d.InBlock(6) || d.InBlock(3) || d.InBlock(0) || d.InBlock(99) {
		t.Error("InBlock")
	}
	var spans []string
	for _, s := range d.Spans() {
		spans = append(spans, s.Text)
	}
	if !slices.Equal(spans, []string{"a.B", "c `d` e", "x|y"}) {
		t.Errorf("spans %q", spans)
	}
	tables := d.Tables()
	if len(tables) != 1 || !slices.Equal(tables[0][1].Cells, []string{"`x|y`", "b|c"}) {
		t.Errorf("tables %+v", tables)
	}
	w := Words(d)
	if !w["item"] || !w["x"] || w["1"] {
		t.Error("words")
	}
}
