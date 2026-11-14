// SPDX-License-Identifier: MIT

// Package gitinfo reads the release tags and history docsync needs from git.
package gitinfo

import (
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Repo is a git work tree. A nil *Repo has no tags and no history: the
// checks that need git are then skipped.
type Repo struct {
	Dir  string
	tags []Version
}

// Open returns the repository holding dir, or nil when dir is not in a git
// work tree or git is not installed.
func Open(dir string) *Repo {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil
	}
	r := &Repo{Dir: dir}
	out, err = exec.Command("git", "-C", dir, "tag", "-l", "v*").Output()
	if err == nil {
		for _, t := range strings.Fields(string(out)) {
			if v, ok := Parse(t); ok {
				r.tags = append(r.tags, v)
			}
		}
	}
	slices.SortFunc(r.tags, Version.Compare)
	return r
}

// Tags returns the release tags vX.Y.Z, oldest first.
func (r *Repo) Tags() []Version {
	if r == nil {
		return nil
	}
	return r.tags
}

// Latest returns the newest release tag.
func (r *Repo) Latest() (Version, bool) {
	t := r.Tags()
	if len(t) == 0 {
		return Version{}, false
	}
	return t[len(t)-1], true
}

// HasTag reports whether v is tagged.
func (r *Repo) HasTag(v Version) bool { return slices.Contains(r.Tags(), v) }

// Added returns the time of the commit that added s to file as it is now:
// the newest commit that changed how often s occurs in file, when file holds
// s at that commit. A string that was added, removed and added again dates
// from the last addition. file is relative to the repository's directory.
// Added returns false when no commit added s, or when the newest such commit
// removed it (s is back in the work tree but not committed yet).
func (r *Repo) Added(file, s string) (time.Time, bool) {
	if r == nil {
		return time.Time{}, false
	}
	out, err := exec.Command("git", "-C", r.Dir, "log", "-1", "--format=%H %ct", "-S"+s, "--", file).Output()
	if err != nil {
		return time.Time{}, false
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	// "./" makes the path relative to r.Dir, not to the top of the work tree.
	blob, err := exec.Command("git", "-C", r.Dir, "show", f[0]+":./"+file).Output()
	if err != nil || !strings.Contains(string(blob), s) {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// Version is a release version X.Y.Z.
type Version struct{ Major, Minor, Patch int }

var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Parse reads vX.Y.Z (the v is optional). Pre-releases are not versions.
func Parse(s string) (Version, bool) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return Version{a, b, c}, true
}

// Compare orders versions.
func (v Version) Compare(w Version) int {
	switch {
	case v.Major != w.Major:
		return v.Major - w.Major
	case v.Minor != w.Minor:
		return v.Minor - w.Minor
	default:
		return v.Patch - w.Patch
	}
}

func (v Version) String() string {
	return "v" + strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}
