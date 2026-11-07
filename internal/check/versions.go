// SPDX-License-Identifier: MIT

package check

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/gitinfo"
	"github.com/otfabric/docsync/internal/markdown"
)

var (
	// "Requires Go 1.26 or later", "Go 1.26+", "needs Go 1.26.3".
	goClaimRE = regexp.MustCompile(`(?i)(?:\b(?:requires?|needs?|minimum|built with|build with)\b[^.\n]{0,20}?\bGo\s+(\d+\.\d+(?:\.\d+)?)\b|\bGo\s+(\d+\.\d+(?:\.\d+)?)(?:\s*\+|\s+or\s+(?:later|newer|above)|\s+and\s+later))`)
	depKeyRE  = regexp.MustCompile(`(?i)\b(built on|built with|requires?|needs?|depends on|based on|uses|pinned to)\b|v\d+\.\d+\.\d+\+`)
	// depNearRE is a requirement keyword right before the module name:
	// "requires go-x", "built on `go-x`", "depends on the go-x module".
	depNearRE = regexp.MustCompile(`(?i)\b(?:built on|built with|requires?|needs?|depends on|based on|uses|pinned to)\s+(?:(?:the|at least|module|library)\s+)*[` + "`" + `*\[]*(?:github\.com/)?(?:otfabric/)?$`)
	linkRE    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	semverRE  = regexp.MustCompile(`\bv(\d+\.\d+\.\d+)\b`)
)

// Versions checks the Go and OT Fabric dependency versions the documents
// state against go.mod (DS-VER).
func Versions(r *Repo) []finding.Finding {
	if r.Module == nil {
		return nil
	}
	var out []finding.Finding
	goVer := normGo(r.Module.GoVersion)
	deps := map[string]string{} // repository name -> version
	for p, v := range r.Module.Requires {
		if strings.HasPrefix(p, "github.com/otfabric/") {
			deps[path.Base(strings.TrimSuffix(p, "/v"+strings.TrimPrefix(semverMajor(v), "v")))] = v
		}
	}
	for _, d := range r.Docs {
		for i, l := range d.Lines {
			if d.InBlock(i + 1) {
				continue
			}
			if goVer != "" {
				for _, m := range goClaimRE.FindAllStringSubmatch(l, -1) {
					v := m[1] + m[2]
					if normGo(v) != goVer && v != r.Module.Toolchain {
						out = append(out, finding.Finding{Rule: "DS-VER", File: d.Path, Line: i + 1, Subject: "go " + v,
							Message: fmt.Sprintf("states Go %s; go.mod says go %s", v, r.Module.GoVersion)})
					}
				}
			}
			if !depKeyRE.MatchString(l) {
				continue
			}
			l := linkRE.ReplaceAllString(l, "$1") // "built on [otfabric/go-x](https://…) v1.2.3"
			for name, want := range deps {
				idx := strings.Index(l, name)
				for idx >= 0 {
					end := idx + len(name)
					if (idx == 0 || !isNameChar(l[idx-1])) && (end == len(l) || !isNameChar(l[end])) {
						rest := l[end:min(len(l), end+40)]
						mm := semverRE.FindStringSubmatchIndex(rest)
						// A claim: the keyword attached to the name, or "go-x v1.2.3+"
						// (not "fixed in go-x v1.2.3", nor "requires" elsewhere on the line).
						claim := mm != nil && (depNearRE.MatchString(l[max(0, idx-40):idx]) || mm[1] < len(rest) && rest[mm[1]] == '+')
						if claim && !strings.Contains(rest[:mm[0]], "go-") {
							got := "v" + rest[mm[2]:mm[3]]
							if got != want {
								out = append(out, finding.Finding{Rule: "DS-VER", File: d.Path, Line: i + 1, Subject: name + " " + got,
									Message: fmt.Sprintf("states %s %s; go.mod requires %s", name, got, want)})
							}
						}
					}
					n := strings.Index(l[end:], name)
					if n < 0 {
						break
					}
					idx = end + n
				}
			}
		}
	}
	return out
}

func isNameChar(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func semverMajor(v string) string {
	if i := strings.IndexByte(v, '.'); i > 0 {
		return v[:i]
	}
	return v
}

// normGo turns 1.26 and 1.26.0 into the same string.
func normGo(v string) string {
	if strings.Count(v, ".") == 1 {
		return v + ".0"
	}
	return v
}

var (
	emailRE     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@otfabric\.[a-z]+`)
	supportedRE = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)\.x(?:$|[\s(,;:])`)
	latestRE    = regexp.MustCompile(`(?i)^(?:the )?(?:latest|current|newest|most recent)(?: (?:release|version|minor))?$`)
	yesRE       = regexp.MustCompile(`^(?:(?i:yes|supported)\b|✓|✅|✔|:white_check_mark:)`)
)

// "0.6.6 and later", "0.6+", ">= 0.6", "v1.2.x or newer".
var (
	openEndRE   = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)(?:\.(\d+))?(?:\.x)?\s*(?:\+|(?:and|or)\s+(?:later|newer|above|up)|onwards)(?:$|[\s(,;:])`)
	openStartRE = regexp.MustCompile(`(?i)^(?:>=|≥)\s*v?(\d+)\.(\d+)(?:\.(\d+))?(?:\.x)?(?:$|[\s(,;:])`)
)

// openEnded reports whether ver is an open-ended range ("0.6.6 and later")
// that includes latest.
func openEnded(ver string, latest gitinfo.Version) bool {
	m := openEndRE.FindStringSubmatch(ver)
	if m == nil {
		m = openStartRE.FindStringSubmatch(ver)
	}
	if m == nil {
		return false
	}
	var from gitinfo.Version
	from.Major, _ = strconv.Atoi(m[1])
	from.Minor, _ = strconv.Atoi(m[2])
	from.Patch, _ = strconv.Atoi(m[3]) // "" is 0
	return latest.Compare(from) >= 0
}

var versionHeadRE = regexp.MustCompile(`(?i)^versions?$`)

// cellText is a table cell without emphasis and code marks.
func cellText(c string) string {
	return strings.TrimSpace(strings.NewReplacer("`", "", "*", "", "_", "").Replace(c))
}

// SecurityAddress is the only address for private security reports.
const SecurityAddress = "security@otfabric.io"

// Security checks the security contact and the supported versions table
// (DS-SEC).
func Security(r *Repo) []finding.Finding {
	var out []finding.Finding
	for _, d := range r.Docs {
		for i, l := range d.Lines {
			for _, e := range emailRE.FindAllString(l, -1) {
				if strings.HasSuffix(e, "@otfabric.io") {
					continue
				}
				out = append(out, finding.Finding{Rule: "DS-SEC", File: d.Path, Line: i + 1, Subject: e,
					Message: fmt.Sprintf("%s: OT Fabric addresses are @otfabric.io (security reports: %s)", e, SecurityAddress)})
			}
		}
	}
	d := r.ReadDoc("SECURITY.md")
	if d == nil {
		return out
	}
	if !strings.Contains(d.Text(), SecurityAddress) {
		out = append(out, finding.Finding{Rule: "DS-SEC", File: d.Path, Line: 1, Subject: "contact",
			Message: "SECURITY.md does not name " + SecurityAddress})
	}
	var tables [][]markdown.Row
	for _, t := range d.Tables() {
		if len(t[0].Cells) >= 2 && versionHeadRE.MatchString(cellText(t[0].Cells[0])) {
			tables = append(tables, t)
		}
	}
	if len(tables) == 0 {
		return append(out, finding.Finding{Rule: "DS-SEC", File: d.Path, Line: 1, Subject: "supported versions",
			Message: "SECURITY.md has no supported-versions table (| Version | Supported |)"})
	}
	latest, ok := r.Git.Latest()
	if !ok {
		return out
	}
	want := fmt.Sprintf("%d.%d.x", latest.Major, latest.Minor)
	found := false
	for _, t := range tables {
		for _, row := range t[1:] {
			if len(row.Cells) < 2 {
				continue
			}
			ver := cellText(row.Cells[0]) // "0.8.x", "0.8.x (latest: v0.8.2)"
			m := supportedRE.FindStringSubmatch(ver)
			if (m != nil && m[1]+"."+m[2]+".x" == want || latestRE.MatchString(ver) || openEnded(ver, latest)) && yesRE.MatchString(strings.TrimSpace(row.Cells[1])) {
				found = true // "Latest release | Yes" names the latest minor, whatever it is
			}
		}
	}
	if !found {
		out = append(out, finding.Finding{Rule: "DS-SEC", File: d.Path, Line: tables[0][0].Line, Subject: "supported " + want,
			Message: fmt.Sprintf("the supported-versions table does not support %s (latest release %s)", want, latest)})
	}
	return out
}

var (
	releaseRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	bumpRE    = regexp.MustCompile(`(?i)^(built on|built with|bump|go-[a-z0-9-]+ v\d)`)
)

// Now is the clock of the release check (tests replace it).
var Now = time.Now

// Release checks RELEASE.md (DS-REL): a section that repeats most of the
// previous release's bullets word for word, and a newest section that is still not tagged a day
// after it was committed.
func Release(r *Repo) []finding.Finding {
	d := r.ReadDoc("RELEASE.md")
	if d == nil {
		return nil
	}
	type section struct {
		version string
		line    int
		bullets map[string]int
	}
	var secs []*section
	var cur *section
	for i, l := range d.Lines {
		if d.InBlock(i + 1) {
			continue
		}
		if strings.HasPrefix(l, "## ") {
			v := strings.TrimSpace(l[3:])
			if f := strings.Fields(v); len(f) > 0 && releaseRE.MatchString(f[0]) {
				cur = &section{version: f[0], line: i + 1, bullets: map[string]int{}}
				secs = append(secs, cur)
				continue
			}
			cur = nil
		}
		if cur != nil && strings.HasPrefix(l, "- ") {
			b := strings.Join(strings.Fields(l[2:]), " ")
			if _, dup := cur.bullets[b]; !dup {
				cur.bullets[b] = i + 1
			}
		}
	}
	// A section that copies most of the previous one was pasted and not
	// rewritten. Single repeated bullets are status lines ("None.",
	// "opcua-interop v0.13.0 (unchanged).", the Go version) and stay.
	var out []finding.Finding
	for i := 0; i+1 < len(secs); i++ {
		newer, older := secs[i], secs[i+1]
		first, repeated, total := 0, 0, 0
		for b, line := range newer.bullets {
			if bumpRE.MatchString(b) {
				continue
			}
			total++
			if _, ok := older.bullets[b]; ok {
				repeated++
				if first == 0 || line < first {
					first = line
				}
			}
		}
		if repeated >= 3 && 2*repeated > total {
			out = append(out, finding.Finding{Rule: "DS-REL", File: d.Path, Line: first, Subject: newer.version,
				Message: fmt.Sprintf("%s repeats %d of its %d bullets from %s word for word", newer.version, repeated, total, older.version)})
		}
	}
	if len(secs) > 0 && r.Git != nil && len(r.Git.Tags()) > 0 {
		top := secs[0]
		v, _ := gitinfo.Parse(top.version)
		if !r.Git.HasTag(v) {
			if at, ok := r.Git.Added(d.Path, "## "+top.version); ok && Now().Sub(at) > 24*time.Hour {
				out = append(out, finding.Finding{Rule: "DS-REL", File: d.Path, Line: top.line, Subject: top.version,
					Message: fmt.Sprintf("%s has release notes since %s but is not tagged: release it or fold the notes into the next release", top.version, at.UTC().Format(time.DateOnly))})
			}
		}
	}
	return out
}
