// SPDX-License-Identifier: MIT

// Package markdown splits a Markdown document into what docsync checks:
// fenced code blocks, inline code spans, table rows and plain lines.
package markdown

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Doc is one Markdown document.
type Doc struct {
	// Path is slash-separated and relative to the checked directory.
	Path string
	// Lines are the document's lines; Lines[0] is line 1.
	Lines []string
	// Blocks are the fenced code blocks in document order.
	Blocks []Block
	// inBlock[i] is true when line i+1 belongs to a fenced block (fences
	// included).
	inBlock []bool
}

// Block is one fenced code block.
type Block struct {
	// Lang is the first word of the info string, lower case.
	Lang string
	// Start is the line number of the first content line.
	Start int
	// Lines are the content lines, with the fence's indentation removed.
	Lines []string
}

var fenceRE = regexp.MustCompile("^( *)(`{3,}|~{3,})\\s*([^`]*)$")

// Parse splits data into a Doc.
func Parse(path string, data []byte) *Doc {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	d := &Doc{Path: path, Lines: lines, inBlock: make([]bool, len(lines))}
	for i := 0; i < len(lines); i++ {
		m := fenceRE.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		indent, fence := len(m[1]), m[2]
		lang := ""
		if f := strings.Fields(m[3]); len(f) > 0 {
			lang = strings.Trim(strings.ToLower(f[0]), "{}.")
		}
		b := Block{Lang: lang, Start: i + 2}
		d.inBlock[i] = true
		j := i + 1
		for ; j < len(lines); j++ {
			d.inBlock[j] = true
			t := strings.TrimLeft(lines[j], " ")
			if strings.HasPrefix(t, fence[:1]) && strings.Trim(t, fence[:1]+" ") == "" && len(strings.TrimRight(t, " ")) >= len(fence) {
				break
			}
			l := lines[j]
			for k := 0; k < indent && strings.HasPrefix(l, " "); k++ {
				l = l[1:]
			}
			b.Lines = append(b.Lines, l)
		}
		d.Blocks = append(d.Blocks, b)
		i = j
	}
	return d
}

// Read parses the file root/rel.
func Read(root, rel string) (*Doc, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	return Parse(rel, data), nil
}

// InBlock reports whether line n (1-based) is part of a fenced block.
func (d *Doc) InBlock(n int) bool { return n >= 1 && n <= len(d.inBlock) && d.inBlock[n-1] }

// Span is an inline code span.
type Span struct {
	Line int
	Text string
}

// Spans returns the inline code spans outside fenced blocks.
func (d *Doc) Spans() []Span {
	var out []Span
	for i, l := range d.Lines {
		if d.inBlock[i] {
			continue
		}
		for _, s := range spans(l) {
			out = append(out, Span{Line: i + 1, Text: s})
		}
	}
	return out
}

// spans returns the code spans of one line: a run of backticks up to the next
// run of the same length (CommonMark).
func spans(l string) []string {
	var out []string
	run := func(i int) int {
		j := i
		for j < len(l) && l[j] == '`' {
			j++
		}
		return j - i
	}
	for i := 0; i < len(l); {
		if l[i] != '`' {
			i++
			continue
		}
		n := run(i)
		closed := false
		for j := i + n; j < len(l); {
			if l[j] != '`' {
				j++
				continue
			}
			m := run(j)
			if m == n {
				out = append(out, strings.TrimSpace(l[i+n:j]))
				i, closed = j+m, true
				break
			}
			j += m
		}
		if !closed {
			i += n
		}
	}
	return out
}

// Text returns every line of the document, blocks included.
func (d *Doc) Text() string { return strings.Join(d.Lines, "\n") }

// Row is one table row outside fenced blocks.
type Row struct {
	Line  int
	Cells []string
}

var sepRE = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)

// Tables returns the data rows of the tables, each table a slice of rows,
// headers first.
func (d *Doc) Tables() [][]Row {
	var out [][]Row
	var cur []Row
	flush := func() {
		if len(cur) >= 2 {
			out = append(out, cur)
		}
		cur = nil
	}
	for i, l := range d.Lines {
		t := strings.TrimSpace(l)
		if d.inBlock[i] || !strings.HasPrefix(t, "|") {
			flush()
			continue
		}
		if sepRE.MatchString(t) {
			continue
		}
		cur = append(cur, Row{Line: i + 1, Cells: splitRow(t)})
	}
	flush()
	return out
}

func splitRow(t string) []string {
	t = strings.TrimSuffix(strings.TrimPrefix(t, "|"), "|")
	var cells []string
	var b strings.Builder
	inCode := false
	for i := 0; i < len(t); i++ {
		switch c := t[i]; {
		case c == '\\' && i+1 < len(t) && t[i+1] == '|':
			b.WriteByte('|')
			i++
		case c == '`':
			inCode = !inCode
			b.WriteByte(c)
		case c == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

// Words returns the set of identifier-like words of the documents.
func Words(docs ...*Doc) map[string]bool {
	w := map[string]bool{}
	for _, d := range docs {
		for _, l := range d.Lines {
			for _, f := range wordRE.FindAllString(l, -1) {
				w[f] = true
			}
		}
	}
	return w
}

var wordRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// List returns the Markdown files below root, slash-separated and sorted.
// Hidden directories, vendor, node_modules, testdata and nested modules
// (directories with their own go.mod) are skipped.
func List(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			name := e.Name()
			if rel != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			if rel != "." {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".md") {
			out = append(out, rel)
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}
