// SPDX-License-Identifier: MIT

package check

import (
	"fmt"

	"github.com/otfabric/docsync/internal/apidoc"
	"github.com/otfabric/docsync/internal/finding"
)

// Reference renders the API reference of the module's public packages.
func Reference(r *Repo) (map[string]string, error) {
	var pkgs []apidoc.Package
	for _, p := range r.publicPackages() {
		pkgs = append(pkgs, apidoc.Package{Rel: p.Rel, ImportPath: p.ImportPath})
	}
	return apidoc.Generate(r.Dir, r.Module.Path, pkgs)
}

// Gen checks that the generated API reference is what docsync api writes
// now (DS-GEN). A module without a generated reference has nothing to check.
func Gen(r *Repo) []finding.Finding {
	if len(r.Generated) == 0 || r.Module == nil || !r.Config.HasAPI() {
		return nil
	}
	want, err := Reference(r)
	if err != nil {
		return []finding.Finding{{Rule: "DS-GEN", File: apidoc.Dir, Subject: apidoc.Dir, Message: "the API reference cannot be generated: " + err.Error()}}
	}
	var out []finding.Finding
	have := map[string]bool{}
	for _, d := range r.Generated {
		have[d.Path] = true
		w, ok := want[d.Path]
		switch {
		case !ok:
			out = append(out, finding.Finding{Rule: "DS-GEN", File: d.Path, Subject: d.Path, Message: d.Path + " documents a package that is gone: run docsync api"})
		case w != d.Text()+"\n":
			out = append(out, finding.Finding{Rule: "DS-GEN", File: d.Path, Line: firstDiff(w, d.Text()+"\n"), Subject: d.Path, Message: d.Path + " is out of date: run docsync api"})
		}
	}
	for p := range want {
		if !have[p] {
			out = append(out, finding.Finding{Rule: "DS-GEN", File: p, Subject: p, Message: fmt.Sprintf("%s is missing: run docsync api", p)})
		}
	}
	return out
}

func firstDiff(a, b string) int {
	line := 1
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return line
		}
		if a[i] == '\n' {
			line++
		}
	}
	return line
}
