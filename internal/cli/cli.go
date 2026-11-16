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
	"runtime/debug"
	"strings"
	"unicode/utf8"

	"github.com/otfabric/docsync/internal/apidoc"
	"github.com/otfabric/docsync/internal/check"
	"github.com/otfabric/docsync/internal/config"
	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/gitinfo"
	"github.com/otfabric/docsync/internal/goapi"
	"github.com/otfabric/docsync/internal/markdown"
	"github.com/otfabric/docsync/internal/repofs"
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
// code. An unexpected internal error exits 10 with its stack on stderr.
func Run(args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if v := recover(); v != nil {
			fail(stderr, fmt.Errorf("internal error: %v", v))
			for _, l := range strings.Split(strings.TrimSpace(string(debug.Stack())), "\n") {
				_, _ = fmt.Fprintln(stderr, "\t"+clean(strings.TrimSpace(l), false))
			}
			code = ExitInternal
		}
	}()
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
	var want map[string]bool
	if *rules != "" {
		// An unknown rule is a usage error: a mistyped one would report
		// nothing and pass.
		want = map[string]bool{}
		for _, r := range strings.Split(*rules, ",") {
			id := strings.TrimSpace(r)
			if id == "" {
				continue
			}
			if !knownRule(id) {
				_, _ = fmt.Fprintf(stderr, "docsync check: unknown rule %q (docsync rules lists them)\n", id)
				return ExitUsage
			}
			want[id] = true
		}
		if len(want) == 0 {
			_, _ = fmt.Fprintln(stderr, "docsync check: --rules names no rule")
			return ExitUsage
		}
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	repo, err := Load(dir)
	if err != nil {
		return loadFailed(stderr, err)
	}
	defer func() { _ = repo.FS.Close() }()
	fsAll := check.Run(repo)
	if want != nil {
		kept := fsAll[:0]
		for _, f := range fsAll {
			if want[f.Rule] {
				kept = append(kept, f)
			}
		}
		fsAll = kept
	}
	if err := write(stdout, *format, fsAll); err != nil {
		fail(stderr, err)
		return ExitInternal
	}
	if refused(stderr, repo) {
		return ExitInternal
	}
	if stopped(stderr, repo, *rules) {
		return ExitInternal
	}
	if len(fsAll) > 0 {
		return ExitFindings
	}
	return ExitOK
}

// knownRule reports whether id is a rule docsync has (docsync rules).
func knownRule(id string) bool {
	for _, r := range finding.Rules {
		if r.ID == id {
			return true
		}
	}
	return false
}

// loadFailed reports an error of Load and returns its exit code: 3 for a
// configuration that cannot be used, 10 for everything else, a file docsync
// refuses to read included.
func loadFailed(stderr io.Writer, err error) int {
	fail(stderr, err)
	var re *repofs.RefusedError
	var ce *configError
	if !errors.As(err, &re) && errors.As(err, &ce) {
		return ExitInvalidInput
	}
	return ExitInternal
}

// refused reports the files the checks met and did not read (repofs): a
// check could not run without them, so the run fails.
func refused(stderr io.Writer, repo *check.Repo) bool {
	for _, e := range repo.FS.Refused() {
		fail(stderr, fmt.Errorf("a check could not run: %w", e))
	}
	return len(repo.FS.Refused()) > 0
}

// stopped reports the checks that could not run (check.Stops) for a rule
// the run reports: the run fails, as it does for a refused file. An ignore
// entry for the DS-CONF finding, or --rules without the rules it stops,
// lets the run go on.
func stopped(stderr io.Writer, repo *check.Repo, rules string) bool {
	want := map[string]bool{}
	for _, r := range strings.Split(rules, ",") {
		want[strings.TrimSpace(r)] = true
	}
	stop := false
	for _, f := range repo.Stopped {
		for _, rule := range check.Stops(f) {
			if rules == "" || want[rule] || want[f.Rule] {
				fail(stderr, fmt.Errorf("%s could not run: %s: %s", rule, f.Subject, f.Message))
				stop = true
				break
			}
		}
	}
	return stop
}

// fail prints an error on one line, with control characters made visible.
func fail(stderr io.Writer, err error) {
	_, _ = fmt.Fprintln(stderr, "docsync:", clean(err.Error(), false))
}

// runAPI writes the generated API reference and removes generated files of
// packages that are gone. It changes nothing outside docs/api/ (repofs).
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
		return loadFailed(stderr, err)
	}
	defer func() { _ = repo.FS.Close() }()
	if repo.Module == nil {
		_, _ = fmt.Fprintln(stderr, "docsync api: no go.mod in", clean(dir, false))
		return ExitInvalidInput
	}
	files, err := check.Reference(repo)
	if err != nil {
		fail(stderr, err)
		return ExitInternal
	}
	if refused(stderr, repo) {
		return ExitInternal
	}
	write := map[string][]byte{}
	for p, content := range files {
		name, ok := strings.CutPrefix(p, apidoc.Dir+"/")
		if !ok {
			fail(stderr, &repofs.RefusedError{Op: "write", Path: p, Reason: "outside " + apidoc.Dir + ", which is all docsync api changes"})
			return ExitInternal
		}
		write[name] = []byte(content)
	}
	var remove []string
	for _, d := range repo.Generated {
		if _, ok := files[d.Path]; ok {
			continue
		}
		name, ok := strings.CutPrefix(d.Path, apidoc.Dir+"/")
		if !ok {
			fail(stderr, &repofs.RefusedError{Op: "remove", Path: d.Path, Reason: "it starts with the marker of docsync api, but it is outside " + apidoc.Dir + ", which is all docsync api changes"})
			return ExitInternal
		}
		remove = append(remove, name)
	}
	wrote, removed, err := repo.FS.Update(apidoc.Dir, write, remove)
	for _, p := range removed {
		_, _ = fmt.Fprintln(stdout, "removed", clean(p, false))
	}
	for _, p := range wrote {
		_, _ = fmt.Fprintln(stdout, "wrote", clean(p, false))
	}
	if err != nil {
		fail(stderr, err)
		return ExitInternal
	}
	return ExitOK
}

type configError struct{ err error }

func (e *configError) Error() string { return e.err.Error() }
func (e *configError) Unwrap() error { return e.err }

// readLimits bound what one run reads (repofs.Default; tests lower them).
var readLimits = repofs.Default

// Load reads everything the checks need from dir, through a repofs.FS that
// the returned Repo holds and its caller closes.
func Load(dir string) (*check.Repo, error) {
	fsys, err := repofs.Open(dir, readLimits)
	if err != nil {
		return nil, err
	}
	r, err := load(fsys)
	if err != nil {
		_ = fsys.Close()
		return nil, err
	}
	return r, nil
}

func load(fsys *repofs.FS) (*check.Repo, error) {
	dir := fsys.Dir()
	cfg, err := config.Load(fsys)
	if err != nil {
		var re *repofs.RefusedError
		if errors.As(err, &re) {
			return nil, err
		}
		return nil, &configError{err}
	}
	r := &check.Repo{Dir: dir, FS: fsys, Config: cfg, GoBin: "go", Git: gitinfo.Open(dir)}
	if gb := os.Getenv("DOCSYNC_GO"); gb != "" {
		r.GoBin = gb
	}
	if fsys.Exists("go.mod") {
		if r.Module, err = goapi.Load(fsys); err != nil {
			return nil, err
		}
		r.Index = goapi.NewIndex(r.Module)
	}
	paths, err := markdown.List(fsys)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if config.MatchAny(cfg.ExcludeGlobs(), p) {
			continue
		}
		d, err := markdown.Read(fsys, p)
		if err != nil {
			return nil, err
		}
		// The marker counts only where docsync api writes: anywhere else the
		// file is an ordinary document.
		if strings.HasPrefix(p, apidoc.Dir+"/") && apidoc.IsGenerated(d.Text()) {
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
			if _, err := fmt.Fprintf(w, "::error file=%s,line=%d,title=%s::%s\n", ghProperty(clean(f.File, false)), max(f.Line, 1), ghProperty(f.Rule), ghData(clean(f.Message, true))); err != nil {
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
			if _, err := fmt.Fprintf(w, "| %s | `%s` | %s |\n", f.Rule, where(f), strings.ReplaceAll(clean(f.Message, false), "|", `\|`)); err != nil {
				return err
			}
		}
		return nil
	default:
		for _, f := range fs {
			if _, err := fmt.Fprintf(w, "%s: %s: %s\n", where(f), f.Rule, clean(f.Message, false)); err != nil {
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
		return fmt.Sprintf("%s:%d", clean(f.File, false), f.Line)
	}
	return clean(f.File, false)
}

// ghData escapes the message of a workflow command (GitHub's escapeData).
func ghData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	return strings.ReplaceAll(s, "\n", "%0A")
}

// ghProperty escapes a property value of a workflow command, such as file=
// (GitHub's escapeProperty).
func ghProperty(s string) string {
	s = ghData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	return strings.ReplaceAll(s, ",", "%2C")
}

// clean makes visible what a terminal or a log viewer would act on: C0
// control characters other than tab (and other than line feed and carriage
// return when keepNL is set, for a format that escapes them), DEL, C1
// control characters, the Unicode bidirectional controls and bytes that are
// not UTF-8 become \xNN or \uNNNN. Anything else is unchanged.
func clean(s string, keepNL bool) string {
	unsafe := func(r rune) bool {
		switch {
		case r == '\t':
			return false
		case r == '\n' || r == '\r':
			return !keepNL
		case r < 0x20, r >= 0x7f && r < 0xa0:
			return true
		case r == 0x061c, r == 0x200e, r == 0x200f, r == 0x2028, r == 0x2029, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return true
		}
		return false
	}
	if strings.IndexFunc(s, unsafe) < 0 && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case !unsafe(r):
			b.WriteString(s[i : i+n])
		case r < 0x80:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
		i += n
	}
	return b.String()
}
