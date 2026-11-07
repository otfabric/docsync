// SPDX-License-Identifier: MIT

// Package cli is docsync's command line.
package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/otfabric/docsync/internal/apidoc"
	"github.com/otfabric/docsync/internal/check"
	"github.com/otfabric/docsync/internal/config"
	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/gitinfo"
	"github.com/otfabric/docsync/internal/goapi"
	"github.com/otfabric/docsync/internal/markdown"
)

// Exit codes (organisation table, standards/api-design.md).
const (
	ExitOK           = 0
	ExitFindings     = 1
	ExitUsage        = 2
	ExitInvalidInput = 3
	ExitInternal     = 10
)

// Version is set by the release build.
var Version = "dev"

// ManifestTemplate is the TestDocsyncManifest a cobra CLI copies (docsync
// template).
//
//go:embed manifest_test.go.tmpl
var ManifestTemplate string

// ManifestMarker is the line of the template the policy check looks for.
const ManifestMarker = "docsync manifest v1"

const usage = `docsync checks that the documents of a Go module say what its code does.

Usage:
  docsync check [--format text|github|json|markdown] [--rules DS-API,...] [dir]
  docsync api [dir]            write the API reference to docs/api/
  docsync template [package]   print TestDocsyncManifest for a cobra CLI
  docsync rules
  docsync version

check exits 0 when the documents agree with the code, 1 with findings,
2 on a usage error, 3 when the configuration (.otfabric/docsync.json) is
invalid and 10 on an internal error.
`

// Run runs docsync with args (without the program name) and returns the exit
// code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "api":
		return runAPI(args[1:], stdout, stderr)
	case "template":
		pkg := "main"
		if len(args) > 2 {
			_, _ = fmt.Fprint(stderr, usage)
			return ExitUsage
		}
		if len(args) == 2 {
			pkg = args[1]
		}
		_, _ = fmt.Fprint(stdout, strings.Replace(ManifestTemplate, "package main", "package "+pkg, 1))
		return ExitOK
	case "rules":
		for _, r := range finding.Rules {
			_, _ = fmt.Fprintf(stdout, "%-8s %s\n", r.ID, r.Title)
		}
		return ExitOK
	case "version", "--version":
		_, _ = fmt.Fprintln(stdout, "docsync", Version)
		return ExitOK
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return ExitOK
	default:
		_, _ = fmt.Fprintf(stderr, "docsync: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text, github, json or markdown")
	rules := fs.String("rules", "", "comma-separated rules to report (default: all)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		_, _ = fmt.Fprintln(stderr, "docsync check: at most one directory")
		return ExitUsage
	}
	switch *format {
	case "text", "github", "json", "markdown":
	default:
		_, _ = fmt.Fprintf(stderr, "docsync check: unknown format %q\n", *format)
		return ExitUsage
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	repo, err := Load(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "docsync:", err)
		var ce *configError
		if errors.As(err, &ce) {
			return ExitInvalidInput
		}
		return ExitInternal
	}
	fsAll := check.Run(repo)
	if *rules != "" {
		want := map[string]bool{}
		for _, r := range strings.Split(*rules, ",") {
			want[strings.TrimSpace(r)] = true
		}
		kept := fsAll[:0]
		for _, f := range fsAll {
			if want[f.Rule] {
				kept = append(kept, f)
			}
		}
		fsAll = kept
	}
	if err := write(stdout, *format, fsAll); err != nil {
		_, _ = fmt.Fprintln(stderr, "docsync:", err)
		return ExitInternal
	}
	if len(fsAll) > 0 {
		return ExitFindings
	}
	return ExitOK
}

// runAPI writes the generated API reference and removes generated files of
// packages that are gone.
func runAPI(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 || len(args) == 1 && strings.HasPrefix(args[0], "-") {
		_, _ = fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	repo, err := Load(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "docsync:", err)
		var ce *configError
		if errors.As(err, &ce) {
			return ExitInvalidInput
		}
		return ExitInternal
	}
	if repo.Module == nil {
		_, _ = fmt.Fprintln(stderr, "docsync api: no go.mod in", dir)
		return ExitInvalidInput
	}
	files, err := check.Reference(repo)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "docsync:", err)
		return ExitInternal
	}
	for _, d := range repo.Generated {
		if _, ok := files[d.Path]; !ok {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(d.Path))); err != nil {
				_, _ = fmt.Fprintln(stderr, "docsync:", err)
				return ExitInternal
			}
			_, _ = fmt.Fprintln(stdout, "removed", d.Path)
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if old, err := os.ReadFile(p); err == nil && string(old) == files[n] {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			_, _ = fmt.Fprintln(stderr, "docsync:", err)
			return ExitInternal
		}
		if err := os.WriteFile(p, []byte(files[n]), 0o644); err != nil {
			_, _ = fmt.Fprintln(stderr, "docsync:", err)
			return ExitInternal
		}
		_, _ = fmt.Fprintln(stdout, "wrote", n)
	}
	return ExitOK
}

type configError struct{ err error }

func (e *configError) Error() string { return e.err.Error() }
func (e *configError) Unwrap() error { return e.err }

// Load reads everything the checks need from dir.
func Load(dir string) (*check.Repo, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, &configError{err}
	}
	r := &check.Repo{Dir: dir, Config: cfg, GoBin: "go", Git: gitinfo.Open(dir)}
	if gb := os.Getenv("DOCSYNC_GO"); gb != "" {
		r.GoBin = gb
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		if r.Module, err = goapi.Load(dir); err != nil {
			return nil, err
		}
		r.Index = goapi.NewIndex(r.Module)
	}
	paths, err := markdown.List(dir)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if config.MatchAny(cfg.ExcludeGlobs(), p) {
			continue
		}
		d, err := markdown.Read(dir, p)
		if err != nil {
			return nil, err
		}
		if apidoc.IsGenerated(d.Text()) {
			r.Generated = append(r.Generated, d)
			continue
		}
		r.Docs = append(r.Docs, d)
	}
	return r, nil
}

func write(w io.Writer, format string, fs []finding.Finding) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if fs == nil {
			fs = []finding.Finding{}
		}
		return enc.Encode(fs)
	case "github":
		for _, f := range fs {
			if _, err := fmt.Fprintf(w, "::error file=%s,line=%d,title=%s::%s\n", f.File, max(f.Line, 1), f.Rule, ghEscape(f.Message)); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "docsync: %d finding(s)\n", len(fs))
		return err
	case "markdown":
		if len(fs) == 0 {
			_, err := fmt.Fprintln(w, "### docsync\n\nThe documents agree with the code.")
			return err
		}
		_, _ = fmt.Fprintf(w, "### docsync: %d finding(s)\n\n| Rule | Where | Finding |\n|---|---|---|\n", len(fs))
		for _, f := range fs {
			if _, err := fmt.Fprintf(w, "| %s | `%s` | %s |\n", f.Rule, where(f), strings.ReplaceAll(f.Message, "|", `\|`)); err != nil {
				return err
			}
		}
		return nil
	default:
		for _, f := range fs {
			if _, err := fmt.Fprintf(w, "%s: %s: %s\n", where(f), f.Rule, f.Message); err != nil {
				return err
			}
		}
		if len(fs) > 0 {
			_, err := fmt.Fprintf(w, "docsync: %d finding(s)\n", len(fs))
			return err
		}
		return nil
	}
}

func where(f finding.Finding) string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return f.File
}

func ghEscape(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	return strings.ReplaceAll(s, "\n", "%0A")
}
