// SPDX-License-Identifier: MIT

// Package check holds docsync's checks. Each check reads a Repo and returns
// its findings.
package check

import (
	"regexp"

	"github.com/otfabric/docsync/internal/config"
	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/gitinfo"
	"github.com/otfabric/docsync/internal/goapi"
	"github.com/otfabric/docsync/internal/markdown"
)

// Repo is everything the checks read.
type Repo struct {
	Dir    string
	Config *config.Config
	Module *goapi.Module
	Index  *goapi.Index
	// Docs are the documents not excluded by the configuration, without the
	// generated API reference.
	Docs []*markdown.Doc
	// Generated are the files docsync api wrote (docs/api/**): the API
	// reference for DS-API, compared by DS-GEN, read by no other check.
	Generated []*markdown.Doc
	Git       *gitinfo.Repo
	// GoBin is the go command (tests replace it).
	GoBin string

	depNames map[string]map[string]bool // import path -> exported names (nil: unknown)
}

// Doc returns the document at rel, or nil.
func (r *Repo) Doc(rel string) *markdown.Doc {
	for _, d := range r.Docs {
		if d.Path == rel {
			return d
		}
	}
	return nil
}

// ReadDoc returns the document at rel even when the configuration excludes it
// from the other checks (RELEASE.md), or nil when it does not exist.
func (r *Repo) ReadDoc(rel string) *markdown.Doc {
	if d := r.Doc(rel); d != nil {
		return d
	}
	d, err := markdown.Read(r.Dir, rel)
	if err != nil {
		return nil
	}
	return d
}

// DocsMatching returns the documents matching one of the globs.
func (r *Repo) DocsMatching(globs []string) []*markdown.Doc {
	var out []*markdown.Doc
	for _, d := range append(append([]*markdown.Doc{}, r.Docs...), r.Generated...) {
		if config.MatchAny(globs, d.Path) {
			out = append(out, d)
		}
	}
	return out
}

// skip reports whether a package directory needs no reference entries.
func (r *Repo) skip(rel string) bool {
	return config.MatchAny(r.Config.SkipDirGlobs(), rel)
}

// Check is one check.
type Check func(*Repo) []finding.Finding

// All are the checks in the order they run.
var All = []Check{API, Gen, Refs, Errors, Code, Versions, Security, Release, CLI}

// Run runs every check and drops the findings the configuration accepts.
func Run(r *Repo) []finding.Finding {
	var out []finding.Finding
	for _, c := range All {
		for _, f := range c(r) {
			if !r.Config.Ignored(f.Rule, f.Subject) {
				out = append(out, f)
			}
		}
	}
	finding.Sort(out)
	return out
}

// stdMethods are interface methods whose implementations need no reference
// entry: the interface documents them.
var stdMethods = map[string]bool{
	"String": true, "GoString": true, "Error": true, "Format": true, "Unwrap": true, "Is": true, "As": true,
	"MarshalJSON": true, "UnmarshalJSON": true, "MarshalText": true, "UnmarshalText": true,
	"MarshalBinary": true, "UnmarshalBinary": true, "MarshalYAML": true, "UnmarshalYAML": true,
	"LogValue": true, "Len": true, "Less": true, "Swap": true, "Read": true, "Write": true, "Close": true,
	"Scan": true, "Value": true, "ServeHTTP": true, "Timeout": true, "Temporary": true,
}

var errWordRE = regexp.MustCompile(`\bErr[A-Z][A-Za-z0-9_]*\b`)
