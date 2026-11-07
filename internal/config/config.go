// SPDX-License-Identifier: MIT

// Package config reads .otfabric/docsync.json, the optional per-repository
// settings of docsync.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// File is where the configuration lives, relative to the checked directory.
const File = ".otfabric/docsync.json"

// Config is the repository's docsync configuration. Every field is optional.
type Config struct {
	// API is false for a module without a public Go API (a CLI): the
	// API, error and reference checks are then off.
	API *bool `json:"api,omitempty"`
	// APIDocs are the documents that make up the API reference (globs).
	APIDocs []string `json:"api_docs,omitempty"`
	// ErrorsDoc is the error reference.
	ErrorsDoc string `json:"errors_doc,omitempty"`
	// ExcludeDocs are documents no check reads: history and plans (globs).
	ExcludeDocs []string `json:"exclude_docs,omitempty"`
	// SkipDirs are package directories whose identifiers need no reference
	// entry (globs, relative to the module).
	SkipDirs []string `json:"skip_dirs,omitempty"`
	// CLIs are the command-line tools of the module.
	CLIs []CLI `json:"clis,omitempty"`
	// Ignore lists accepted findings, each with its reason.
	Ignore []Ignore `json:"ignore,omitempty"`
}

// CLI is one command-line tool.
type CLI struct {
	// Name is the command name, as a user types it.
	Name string `json:"name"`
	// Manifest is the Go package holding TestDocsyncManifest ("." or "./cmd").
	Manifest string `json:"manifest"`
	// ExitCodes is the directory of the exit-code constants.
	ExitCodes string `json:"exit_codes,omitempty"`
	// ExitDoc is the exit-code table (default docs/exitcodes.md).
	ExitDoc string `json:"exit_doc,omitempty"`
	// JSONTypes is the directory of the types written as JSON output.
	JSONTypes string `json:"json_types,omitempty"`
	// JSONDoc documents the JSON output (default docs/json-output.md).
	JSONDoc string `json:"json_doc,omitempty"`
	// Docs are the documents of the CLI (globs; default README.md and docs/**).
	Docs []string `json:"docs,omitempty"`
}

// Ignore accepts the findings of Rule whose subject matches Subject (a glob).
type Ignore struct {
	Rule    string `json:"rule"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// Defaults.
var (
	DefaultAPIDocs     = []string{"API.md", "docs/API.md", "docs/api/**"}
	DefaultExcludeDocs = []string{"RELEASE.md", "CHANGELOG.md", "PLAN.md", "ROADMAP.md", "PROGRESS.md", "**/adr/**", "**/DECISIONS.md", "**/RESEARCH.md", "**/*_DESIGN.md", "**/HANDOFF-*.md"}
	DefaultSkipDirs    = []string{"internal/**", "**/internal/**", "cmd/**", "examples/**", "example/**", "e2e/**", "interop/**", "conformance/**", "tools/**", "scripts/**", "test/**", "tests/**", "testdata/**", "**/testdata/**"}
	DefaultCLIDocs     = []string{"README.md", "docs/**"}
)

// Load reads the configuration of dir. A missing file is the zero Config.
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, File))
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes and validates a configuration.
func Parse(data []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	for i, ig := range c.Ignore {
		if ig.Rule == "" || ig.Subject == "" || strings.TrimSpace(ig.Reason) == "" {
			return nil, fmt.Errorf("%s: ignore[%d] needs rule, subject and reason", File, i)
		}
		if _, err := path.Match(ig.Subject, ""); err != nil {
			return nil, fmt.Errorf("%s: ignore[%d] subject: %w", File, i, err)
		}
	}
	for i, cli := range c.CLIs {
		if cli.Name == "" || cli.Manifest == "" {
			return nil, fmt.Errorf("%s: clis[%d] needs name and manifest", File, i)
		}
	}
	return &c, nil
}

// HasAPI reports whether the API checks run.
func (c *Config) HasAPI() bool { return c.API == nil || *c.API }

// APIDocGlobs returns the API reference globs.
func (c *Config) APIDocGlobs() []string { return or(c.APIDocs, DefaultAPIDocs) }

// ExcludeGlobs returns the globs of documents no check reads.
func (c *Config) ExcludeGlobs() []string { return or(c.ExcludeDocs, DefaultExcludeDocs) }

// SkipDirGlobs returns the package directories without reference entries.
func (c *Config) SkipDirGlobs() []string { return or(c.SkipDirs, DefaultSkipDirs) }

// ErrorsDocPath returns the error reference.
func (c *Config) ErrorsDocPath() string {
	if c.ErrorsDoc != "" {
		return c.ErrorsDoc
	}
	return "ERRORS.md"
}

// Ignored reports whether a finding of rule about subject is accepted.
func (c *Config) Ignored(rule, subject string) bool {
	for _, ig := range c.Ignore {
		if ig.Rule != rule && ig.Rule != "*" {
			continue
		}
		if ok, _ := path.Match(ig.Subject, subject); ok {
			return true
		}
	}
	return false
}

// DocGlobs returns the documents of the CLI.
func (c CLI) DocGlobs() []string { return or(c.Docs, DefaultCLIDocs) }

// ExitDocPath returns the exit-code table of the CLI.
func (c CLI) ExitDocPath() string {
	if c.ExitDoc != "" {
		return c.ExitDoc
	}
	return "docs/exitcodes.md"
}

// JSONDocPath returns the JSON output document of the CLI.
func (c CLI) JSONDocPath() string {
	if c.JSONDoc != "" {
		return c.JSONDoc
	}
	return "docs/json-output.md"
}

func or(v, def []string) []string {
	if len(v) > 0 {
		return v
	}
	return def
}

var globCache = map[string]*regexp.Regexp{}

// Match reports whether the slash-separated path p matches the glob g, where
// ** matches any number of directories (dir/** matches everything below dir)
// and * any run of characters other than '/'.
func Match(g, p string) bool {
	re, ok := globCache[g]
	if !ok {
		var b strings.Builder
		b.WriteString("^")
		for i := 0; i < len(g); i++ {
			switch c := g[i]; {
			case c == '*' && i+1 < len(g) && g[i+1] == '*':
				i++
				switch {
				case i+1 < len(g) && g[i+1] == '/':
					i++
					b.WriteString("(?:.*/)?")
				case i == len(g)-1 && b.Len() > 1 && strings.HasSuffix(b.String(), "/"):
					// dir/** matches dir and everything below it.
					s := strings.TrimSuffix(b.String(), "/")
					b.Reset()
					b.WriteString(s + "(?:/.*)?")
				default:
					b.WriteString(".*")
				}
			case c == '*':
				b.WriteString("[^/]*")
			case c == '?':
				b.WriteString("[^/]")
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		b.WriteString("$")
		re = regexp.MustCompile(b.String())
		globCache[g] = re
	}
	return re.MatchString(p)
}

// MatchAny reports whether p matches one of the globs.
func MatchAny(globs []string, p string) bool {
	for _, g := range globs {
		if Match(g, p) {
			return true
		}
	}
	return false
}
