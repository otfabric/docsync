// SPDX-License-Identifier: MIT

package check

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/goapi"
	"github.com/otfabric/docsync/internal/markdown"
)

// API checks that every exported identifier of a public package appears in
// the API reference (DS-API).
func API(r *Repo) []finding.Finding {
	if !r.Config.HasAPI() || r.Module == nil {
		return nil
	}
	docs := r.DocsMatching(r.Config.APIDocGlobs())
	var out []finding.Finding
	public := r.publicPackages()
	if len(docs) == 0 {
		for _, p := range public {
			if len(p.Funcs)+len(p.Types)+len(p.Values) > 0 {
				return []finding.Finding{{Rule: "DS-API", File: "API.md", Subject: "API.md",
					Message: "no API reference: the module has exported identifiers but none of " + strings.Join(r.Config.APIDocGlobs(), ", ") + " exists"}}
			}
		}
		return nil
	}
	words := markdown.Words(docs...)
	names := docNames(docs)
	miss := func(pkg *goapi.Package, subject string, pos goapi.Pos, what string) {
		out = append(out, finding.Finding{Rule: "DS-API", File: pos.File, Line: pos.Line, Subject: subject,
			Message: fmt.Sprintf("%s %s is not in the API reference (%s)", what, subject, names)})
	}
	for _, p := range public {
		for _, n := range sortedKeys(p.Funcs) {
			if f := p.Funcs[n]; !f.Generated && !words[n] {
				miss(p, p.Name+"."+n, f.Pos, "function")
			}
		}
		for _, n := range sortedKeys(p.Types) {
			t := p.Types[n]
			if t.Generated {
				continue
			}
			if !words[n] {
				miss(p, p.Name+"."+n, t.Pos, "type")
			}
			for _, mn := range sortedKeys(t.Methods) {
				if mf := t.Methods[mn]; !mf.Generated && !stdMethods[mn] && !words[mn] {
					miss(p, p.Name+"."+n+"."+mn, mf.Pos, "method")
				}
			}
		}
		for _, n := range sortedKeys(p.Values) {
			v := p.Values[n]
			if v.Generated || words[n] || v.TypeName != "" && p.Types[v.TypeName] != nil {
				continue // a value of an enum type is documented with its type (reported there when it is not)
			}
			what := "variable"
			if v.Const {
				what = "constant"
			}
			miss(p, p.Name+"."+n, v.Pos, what)
		}
	}
	return out
}

func (r *Repo) publicPackages() []*goapi.Package {
	var out []*goapi.Package
	for _, p := range r.Module.Packages {
		if p.Public(r.skip) {
			out = append(out, p)
		}
	}
	return out
}

func docNames(docs []*markdown.Doc) string {
	var n []string
	for _, d := range docs {
		n = append(n, d.Path)
	}
	return strings.Join(n, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	slices.Sort(k)
	return k
}

var (
	qualRE = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.([A-Z][A-Za-z0-9_]*)`)
	typeRE = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)`)
)

// Refs checks that the identifiers the documents name exist (DS-REF):
// pkg.Name and Type.Member in inline code, ErrX in inline code and Go
// blocks, and pkg.ErrX of a module package in prose.
func Refs(r *Repo) []finding.Finding {
	if !r.Config.HasAPI() || r.Module == nil {
		return nil
	}
	ix := r.Index
	var out []finding.Finding
	seen := map[string]bool{}
	add := func(d *markdown.Doc, line int, subject, msg string) {
		k := d.Path + "\x00" + subject
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, finding.Finding{Rule: "DS-REF", File: d.Path, Line: line, Subject: subject, Message: msg})
	}
	for _, d := range r.Docs {
		for _, s := range d.Spans() {
			for _, m := range qualRE.FindAllStringSubmatchIndex(s.Text, -1) {
				if m[0] > 0 && (s.Text[m[0]-1] == '.' || s.Text[m[0]-1] == '/') {
					continue // part of a longer path: a.b.Name, example.com/x.Y
				}
				pkg, name := s.Text[m[2]:m[3]], s.Text[m[4]:m[5]]
				pkgs := ix.PkgNames[pkg]
				if ix.IsTest(pkg, name) {
					continue // a test, fuzz target, benchmark or example of a _test.go file
				}
				if len(pkgs) == 0 || placeholderName(name) || goapi.Has(pkgs, name) || ix.Members[name] || r.depHas(pkg, name) {
					continue // depHas: a dependency's package of the name (go-ocpp's client in a CLI with cmd/client)
				}
				add(d, s.Line, pkg+"."+name, fmt.Sprintf("%s.%s: package %s has no exported %s", pkg, name, pkg, name))
			}
			for _, m := range typeRE.FindAllStringSubmatchIndex(s.Text, -1) {
				if m[0] > 0 && s.Text[m[0]-1] == '.' || m[1] < len(s.Text) && s.Text[m[1]] == '.' {
					continue // a chain (pkg.Type.Member, A.B.C): an address path as often as Go
				}
				typ, mem := s.Text[m[2]:m[3]], s.Text[m[4]:m[5]]
				if strings.ToUpper(mem) == mem || placeholderName(mem) || placeholderName(typ) {
					continue // LN.DO: IEC 61850 notation, not Go
				}
				ts := ix.Types[typ]
				if len(ts) == 0 || hasMember(ix, ts, mem) {
					continue
				}
				add(d, s.Line, typ+"."+mem, fmt.Sprintf("%s.%s: type %s has no method or field %s", typ, mem, typ, mem))
			}
		}
		// ErrX names: in inline code and Go blocks, and in prose when a
		// module package qualifies them (lib.ErrX). An unqualified ErrX in
		// prose or a table is as often a protocol name (IEC 61850 ErrTerm).
		errs := func(line int, l string, prose bool) {
			for _, m := range errWordRE.FindAllStringIndex(l, -1) {
				name := l[m[0]:m[1]]
				if placeholderName(name) {
					continue
				}
				q := ""
				if m[0] > 0 && l[m[0]-1] == '.' {
					q = qualifier(l[:m[0]-1])
					if q == "" || len(ix.PkgNames[q]) == 0 {
						continue // another module's error: io.ErrX, mms.ErrX
					}
				}
				if prose && q == "" || ix.Members[name] {
					continue
				}
				add(d, line, name, name+" is not declared in the module")
			}
		}
		for _, s := range d.Spans() {
			errs(s.Line, s.Text, false)
		}
		for _, b := range d.Blocks {
			if b.Lang == "go" {
				for j, l := range b.Lines {
					errs(b.Start+j, l, false)
				}
			}
		}
		for i, l := range d.Lines {
			if !d.InBlock(i + 1) {
				errs(i+1, stripSpans(l), true)
			}
		}
	}
	return out
}

// depHas reports whether a package of another module that the module
// imports under the name pkg declares name. When go list cannot say where
// the package is, it is given the benefit of the doubt.
func (r *Repo) depHas(pkg, name string) bool {
	paths := r.Index.Imports[pkg]
	if len(paths) == 0 {
		return false
	}
	if r.depNames == nil {
		r.depNames = map[string]map[string]bool{}
	}
	for _, p := range paths {
		names, ok := r.depNames[p]
		if !ok {
			names = nil
			cmd := exec.Command(r.GoBin, "list", "-e", "-f", "{{.Dir}}", p)
			cmd.Dir = r.Dir
			cmd.Env = append(os.Environ(), "GOWORK=off") // read-only: never writes go.mod or go.sum
			if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
				names = goapi.ExportedNames(strings.TrimSpace(string(out)))
			}
			r.depNames[p] = names
		}
		if names == nil || names[name] {
			return true
		}
	}
	return false
}

// stripSpans blanks the inline code spans of a line (checked on their own).
func stripSpans(l string) string {
	if !strings.Contains(l, "`") {
		return l
	}
	b := []byte(l)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		end := strings.Index(l[i+n:], strings.Repeat("`", n))
		if end < 0 {
			i += n
			continue
		}
		for k := i; k < i+n+end+n; k++ {
			b[k] = ' '
		}
		i += n + end + n
	}
	return string(b)
}

func qualifier(before string) string {
	i := len(before)
	for i > 0 && (before[i-1] >= 'a' && before[i-1] <= 'z' || before[i-1] >= '0' && before[i-1] <= '9' || before[i-1] >= 'A' && before[i-1] <= 'Z' || before[i-1] == '_') {
		i--
	}
	return before[i:]
}

func hasMember(ix *goapi.Index, ts []*goapi.Type, name string) bool {
	return hasMemberDepth(ix, ts, name, 0)
}

func hasMemberDepth(ix *goapi.Index, ts []*goapi.Type, name string, depth int) bool {
	for _, t := range ts {
		if t.Methods[name] != nil || slices.Contains(t.Fields, name) {
			return true
		}
		if depth < 4 {
			for _, e := range t.Embedded {
				if hasMemberDepth(ix, ix.Types[e], name, depth+1) {
					return true
				}
			}
		}
	}
	return false
}

// Errors checks that every exported error sentinel and error type of a public
// package is in the error reference (DS-ERR).
func Errors(r *Repo) []finding.Finding {
	if !r.Config.HasAPI() || r.Module == nil {
		return nil
	}
	type item struct {
		subject string
		pos     goapi.Pos
		what    string
	}
	var items []item
	for _, p := range r.publicPackages() {
		for _, n := range sortedKeys(p.Values) {
			if v := p.Values[n]; v.Error && !v.Generated {
				items = append(items, item{p.Name + "." + n, v.Pos, "error sentinel"})
			}
		}
		for _, n := range sortedKeys(p.Types) {
			if t := p.Types[n]; t.IsError() && !t.Generated && !t.Interface {
				items = append(items, item{p.Name + "." + n, t.Pos, "error type"})
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	docPath := r.Config.ErrorsDocPath()
	d := r.Doc(docPath)
	if d == nil {
		return []finding.Finding{{Rule: "DS-ERR", File: docPath, Subject: docPath,
			Message: fmt.Sprintf("%s is missing: the module exports %d errors", docPath, len(items))}}
	}
	words := markdown.Words(d)
	var out []finding.Finding
	for _, it := range items {
		name := it.subject[strings.LastIndexByte(it.subject, '.')+1:]
		if !words[name] {
			out = append(out, finding.Finding{Rule: "DS-ERR", File: it.pos.File, Line: it.pos.Line, Subject: it.subject,
				Message: fmt.Sprintf("%s %s is not in %s", it.what, it.subject, docPath)})
		}
	}
	return out
}
