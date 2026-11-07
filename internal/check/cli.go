// SPDX-License-Identifier: MIT

package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/otfabric/docsync/internal/config"
	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/markdown"
)

// ManifestEnv names the file TestDocsyncManifest writes the command tree to.
const ManifestEnv = "DOCSYNC_MANIFEST"

// Manifest is the command tree a CLI's TestDocsyncManifest writes.
type Manifest struct {
	Version  int       `json:"version"`
	Name     string    `json:"name"`
	Commands []Command `json:"commands"`
}

// Command is one command of the tree.
type Command struct {
	// Path is the command path without the root name ("client read");
	// empty for the root.
	Path     string   `json:"path"`
	Aliases  []string `json:"aliases,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`
	Runnable bool     `json:"runnable"`
	// Subcommands are the names of the direct subcommands.
	Subcommands []string `json:"subcommands,omitempty"`
	Flags       []Flag   `json:"flags,omitempty"`
}

// Flag is one flag a command accepts, its own or inherited.
type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	// Bool is true for a flag that takes no value.
	Bool     bool `json:"bool,omitempty"`
	Required bool `json:"required,omitempty"`
	Hidden   bool `json:"hidden,omitempty"`
	// Local is true where the flag is declared (not inherited).
	Local bool `json:"local,omitempty"`
}

// CLI runs the checks of every configured command-line tool.
func CLI(r *Repo) []finding.Finding {
	var out []finding.Finding
	for _, c := range r.Config.CLIs {
		out = append(out, checkCLI(r, c)...)
	}
	return out
}

func checkCLI(r *Repo, c config.CLI) []finding.Finding {
	var out []finding.Finding
	docs := r.DocsMatching(c.DocGlobs())
	m, err := loadManifest(r, c)
	if err != nil {
		out = append(out, finding.Finding{Rule: "DS-CONF", File: config.File, Subject: c.Name + " manifest", Message: err.Error()})
	} else {
		out = append(out, commandsAndFlags(c, m, docs)...)
		out = append(out, examples(c, m, docs)...)
	}
	out = append(out, exitCodes(r, c)...)
	out = append(out, jsonFields(r, c)...)
	return out
}

func loadManifest(r *Repo, c config.CLI) (*Manifest, error) {
	f, err := os.CreateTemp("", "docsync-manifest-*.json")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(name) }()
	pkg := c.Manifest
	if !strings.HasPrefix(pkg, ".") {
		pkg = "./" + pkg
	}
	cmd := exec.Command(r.GoBin, "test", "-count=1", "-run", "^TestDocsyncManifest$", pkg)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "GOWORK=off", ManifestEnv+"="+name)
	var outb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outb, &outb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("TestDocsyncManifest in %s failed: %w: %s", pkg, err, tail(outb.String(), 600))
	}
	data, err := os.ReadFile(name)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("%s has no TestDocsyncManifest that writes $%s (copy the template from otfabric/docsync)", pkg, ManifestEnv)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest of %s: %w", pkg, err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("manifest of %s has version %d; this docsync reads version 1", pkg, m.Version)
	}
	return &m, nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

// skipCommand reports whether a command needs no documentation: hidden,
// cobra's help and completion, and everything below a hidden command.
func skipCommand(cmds map[string]Command, path string) bool {
	if path == "" {
		return false
	}
	first := strings.Fields(path)[0]
	if first == "help" || first == "completion" || first == "__complete" || first == "__completeNoDesc" {
		return true
	}
	parts := strings.Fields(path)
	for i := 1; i <= len(parts); i++ {
		if cmds[strings.Join(parts[:i], " ")].Hidden {
			return true
		}
	}
	return false
}

func commandsAndFlags(c config.CLI, m *Manifest, docs []*markdown.Doc) []finding.Finding {
	cmds := map[string]Command{}
	for _, cmd := range m.Commands {
		cmds[cmd.Path] = cmd
	}
	var text strings.Builder
	for _, d := range docs {
		text.WriteString(d.Text())
		text.WriteString("\n")
	}
	all := text.String()
	where := "README.md"
	if len(docs) > 0 {
		where = docs[0].Path
	}
	var out []finding.Finding
	flagSeen := map[string]bool{}
	for _, cmd := range m.Commands {
		if skipCommand(cmds, cmd.Path) {
			continue
		}
		if cmd.Path != "" && !mentions(all, append([]string{cmd.Path}, aliasPaths(cmd)...)) {
			out = append(out, finding.Finding{Rule: "DS-CMD", File: where, Subject: c.Name + " " + cmd.Path,
				Message: fmt.Sprintf("command `%s %s` is not documented (%s)", c.Name, cmd.Path, docNames(docs))})
		}
		for _, f := range cmd.Flags {
			if !f.Local || f.Hidden || f.Name == "help" || f.Name == "version" || flagSeen[f.Name] {
				continue
			}
			flagSeen[f.Name] = true
			if !regexp.MustCompile(`--` + regexp.QuoteMeta(f.Name) + `\b`).MatchString(all) {
				subject := "--" + f.Name
				out = append(out, finding.Finding{Rule: "DS-FLAG", File: where, Subject: subject,
					Message: fmt.Sprintf("flag --%s of `%s` is not documented (%s)", f.Name, strings.TrimSpace(c.Name+" "+cmd.Path), docNames(docs))})
			}
		}
	}
	return out
}

func aliasPaths(cmd Command) []string {
	parts := strings.Fields(cmd.Path)
	var out []string
	for _, a := range cmd.Aliases {
		out = append(out, strings.Join(append(parts[:len(parts)-1:len(parts)-1], a), " "))
	}
	return out
}

func mentions(text string, paths []string) bool {
	for _, p := range paths {
		words := strings.Fields(p)
		for i := range words {
			words[i] = regexp.QuoteMeta(words[i])
		}
		re := regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])` + strings.Join(words, `\s+`) + `(?:$|[^A-Za-z0-9_-])`)
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// shellLangs are the block languages holding command lines; an unlabelled
// block is output.
var shellLangs = map[string]bool{"sh": true, "bash": true, "shell": true, "console": true, "zsh": true, "shell-session": true}

var usageExitRE = regexp.MustCompile(`(?i)\bexit(?:\s+(?:code|status))?:?\s+2\b|^\s*2\s*$`)

// examples checks the command lines of the CLI in shell blocks (DS-EX).
func examples(c config.CLI, m *Manifest, docs []*markdown.Doc) []finding.Finding {
	cmds := map[string]Command{}
	for _, cmd := range m.Commands {
		cmds[cmd.Path] = cmd
	}
	var out []finding.Finding
	for _, d := range docs {
		for _, b := range d.Blocks {
			if !shellLangs[b.Lang] {
				continue
			}
			for _, cl := range commandLines(b) {
				if cl.usageError {
					continue
				}
				for _, args := range cliInvocations(c.Name, cl.text) {
					if msg := validate(c.Name, cmds, args); msg != "" {
						out = append(out, finding.Finding{Rule: "DS-EX", File: d.Path, Line: cl.line,
							Subject: fmt.Sprintf("%s:%d", d.Path, cl.line), Message: msg + ": " + strings.TrimSpace(c.Name+" "+strings.Join(args, " "))})
					}
				}
			}
		}
	}
	return out
}

type commandLine struct {
	line       int
	text       string
	usageError bool
}

// commandLines returns a block's command lines: in a console block the lines
// after a prompt, elsewhere every line; continuations are joined.
func commandLines(b markdown.Block) []commandLine {
	console := b.Lang == "console" || b.Lang == "shell-session"
	if !console {
		for _, l := range b.Lines {
			if strings.HasPrefix(strings.TrimSpace(l), "$ ") {
				console = true
				break
			}
		}
	}
	var out []commandLine
	for i := 0; i < len(b.Lines); i++ {
		l := b.Lines[i]
		t := strings.TrimSpace(l)
		if console {
			if !strings.HasPrefix(t, "$ ") && !strings.HasPrefix(t, "% ") && !strings.HasPrefix(t, "> ") {
				continue
			}
			t = strings.TrimSpace(t[2:])
		}
		start := i
		for strings.HasSuffix(t, "\\") && i+1 < len(b.Lines) {
			i++
			t = strings.TrimSuffix(t, "\\") + " " + strings.TrimSpace(b.Lines[i])
		}
		cl := commandLine{line: b.Start + start, text: t}
		if console {
			// Output up to the next prompt: does it show exit code 2?
			for j := i + 1; j < len(b.Lines); j++ {
				o := strings.TrimSpace(b.Lines[j])
				if strings.HasPrefix(o, "$ ") && !strings.HasPrefix(o, "$ echo $?") {
					break
				}
				if usageExitRE.MatchString(o) || strings.Contains(o, "Usage:") {
					cl.usageError = true
				}
			}
		}
		out = append(out, cl)
	}
	return out
}

// cliInvocations splits a shell line at operators and returns the arguments
// of each part that runs the CLI.
func cliInvocations(name, line string) [][]string {
	words, ok := shellWords(line)
	if !ok {
		return nil
	}
	var out [][]string
	var cur []string
	flush := func() {
		// Skip environment assignments and wrappers.
		for len(cur) > 0 && (strings.Contains(cur[0], "=") && !strings.HasPrefix(cur[0], "-") || cur[0] == "sudo" || cur[0] == "env" || cur[0] == "time" || cur[0] == "exec") {
			cur = cur[1:]
		}
		if len(cur) > 0 && (cur[0] == name || strings.HasSuffix(cur[0], "/"+name) || cur[0] == name+".exe") {
			args := cur[1:]
			// Redirections end the arguments.
			for i, a := range args {
				if strings.HasPrefix(a, ">") || strings.HasPrefix(a, "<") || strings.HasPrefix(a, "2>") || strings.HasPrefix(a, "&>") {
					args = args[:i]
					break
				}
			}
			out = append(out, args)
		}
		cur = nil
	}
	for _, w := range words {
		switch w {
		case "|", "||", "&&", ";", "&", "|&":
			flush()
		default:
			cur = append(cur, w)
		}
	}
	flush()
	return out
}

// shellWords splits like a POSIX shell: quotes, backslash escapes, comments
// and the operators | || && ; & as words of their own. It returns false for
// a line it cannot split (an open quote).
func shellWords(s string) ([]string, bool) {
	var out []string
	var b strings.Builder
	inWord := false
	emit := func() {
		if inWord {
			out = append(out, b.String())
			b.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, false
			}
			b.WriteString(s[i+1 : i+1+j])
			inWord = true
			i += j + 1
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, false
			}
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			b.WriteByte(s[i])
			inWord = true
		case c == '#' && !inWord:
			emit()
			return out, true
		case c == ' ' || c == '\t':
			emit()
		case c == '|' || c == '&' || c == ';':
			emit()
			op := string(c)
			if i+1 < len(s) && (s[i+1] == '|' || s[i+1] == '&') && c != ';' {
				op += string(s[i+1])
				i++
			}
			if op == "&" && len(out) > 0 && strings.HasSuffix(out[len(out)-1], ">") {
				// 2>&1: a redirection, one word.
				j := i + 1
				for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '-') {
					j++
				}
				out[len(out)-1] += op + s[i+1:j]
				i = j - 1
				continue
			}
			out = append(out, op)
		default:
			b.WriteByte(c)
			inWord = true
		}
	}
	emit()
	return out, true
}

// validate resolves args against the command tree and returns what is wrong,
// or "".
func validate(name string, cmds map[string]Command, args []string) string {
	show := func(p string) string { return strings.TrimSpace(name + " " + p) }
	cur := cmds[""]
	path := ""
	set := map[string]bool{}
	help := false
	positional := 0
	flags := func(c Command) map[string]Flag {
		m := map[string]Flag{}
		for _, f := range c.Flags {
			m["--"+f.Name] = f
			if f.Shorthand != "" {
				m["-"+f.Shorthand] = f
			}
		}
		return m
	}
	known := flags(cur)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if placeholder(a) {
			positional++
			continue
		}
		if strings.HasPrefix(a, "--") {
			fname, _, hasValue := strings.Cut(a, "=")
			if fname == "--help" {
				help = true
				continue
			}
			f, ok := known[fname]
			if !ok {
				return fmt.Sprintf("unknown flag %s for `%s`", fname, show(path))
			}
			set[f.Name] = true
			if !f.Bool && !hasValue {
				i++ // the value
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			// -v, -abc (bool shorthands), -nVALUE, -n VALUE, -n=VALUE.
			for k := 1; k < len(a); k++ {
				fname := "-" + a[k:k+1]
				if fname == "-h" {
					help = true
					continue
				}
				f, ok := known[fname]
				if !ok {
					return fmt.Sprintf("unknown flag %s for `%s`", fname, show(path))
				}
				set[f.Name] = true
				if !f.Bool {
					if k == len(a)-1 {
						i++ // the value is the next word
					}
					break // the rest of the word is the value
				}
			}
			continue
		}
		if positional == 0 {
			next := strings.TrimSpace(path + " " + a)
			if sub, ok := cmds[next]; ok {
				cur, path, known = sub, next, flags(sub)
				continue
			}
			if alias := aliasOf(cmds, cur, path, a); alias != "" {
				cur, path, known = cmds[alias], alias, flags(cmds[alias])
				continue
			}
			if a == "help" {
				return ""
			}
			if len(cur.Subcommands) > 0 && !cur.Runnable {
				return fmt.Sprintf("unknown command %q for `%s`", a, show(path))
			}
		}
		positional++
	}
	if help || !cur.Runnable {
		return ""
	}
	for _, f := range cur.Flags {
		if f.Required && !set[f.Name] {
			return fmt.Sprintf("required flag --%s of `%s` is not set", f.Name, show(path))
		}
	}
	return ""
}

func aliasOf(cmds map[string]Command, cur Command, path, a string) string {
	for _, s := range cur.Subcommands {
		full := strings.TrimSpace(path + " " + s)
		for _, al := range cmds[full].Aliases {
			if al == a {
				return full
			}
		}
	}
	return ""
}

// placeholder reports whether a word stands for something the reader fills
// in: <host>, [flags], {id}, ..., $VAR.
func placeholder(a string) bool {
	return strings.HasPrefix(a, "<") || strings.HasPrefix(a, "[") || strings.HasPrefix(a, "{") ||
		a == "..." || a == "…" || strings.HasPrefix(a, "$")
}

// orgExit is the organisation exit-code table (standards/api-design.md).
var orgExit = map[int]string{0: "ok", 1: "findings", 2: "usage", 3: "invalid_input", 4: "transport", 5: "timeout",
	6: "protocol", 7: "partial", 8: "unsupported", 9: "permission", 10: "internal", 13: "safety"}

// orgWords are the words a code's meaning must contain in the table.
var orgWords = map[string][]string{
	"ok": {"success", "ok"}, "findings": {"finding", "problem"}, "usage": {"usage", "flag", "argument"},
	"invalid_input": {"invalid input", "invalid_input", "invalid"},
	"transport":     {"transport", "connect", "network", "i/o"},
	"timeout":       {"timeout", "timed out", "deadline"},
	"protocol":      {"protocol", "refused", "rejected", "exception", "negative"},
	"partial":       {"partial"},
	"unsupported":   {"unsupported", "not supported", "not implemented"},
	"permission":    {"permission", "denied", "forbidden", "unauthori", "not authori"},
	"internal":      {"internal", "unclassified", "unexpected"},
	"safety":        {"safety", "interlock", "live", "not virtual", "non-virtual", "confirm"},
}

// kindAliases are constant names that mean an organisation kind
// (ExitConnection is the transport code).
var kindAliases = map[string]string{
	"success": "ok", "connection": "transport", "connect": "transport", "network": "transport", "io": "transport",
	"rejected": "protocol", "refused": "protocol", "exception": "protocol", "notsupported": "unsupported",
	"notimplemented": "unsupported", "denied": "permission", "forbidden": "permission",
	"failure": "internal", "unexpected": "internal", "invalidinput": "invalid_input", "invalid": "invalid_input",
	"interlock": "safety", "live": "safety", "livetarget": "safety", "unconfirmed": "safety",
}

var exitNameRE = regexp.MustCompile(`^[Ee]xit([A-Z0-9][A-Za-z0-9_]*)$`)

// exitCodes checks the exit-code table against the constants and the
// organisation table (DS-EXIT).
func exitCodes(r *Repo, c config.CLI) []finding.Finding {
	var out []finding.Finding
	consts := map[int]exitConst{}
	if c.ExitCodes != "" {
		consts = exitConsts(r.Dir, c.ExitCodes)
	}
	for _, code := range sortedInts(consts) {
		kind := consts[code].kind
		if a, ok := kindAliases[strings.ReplaceAll(kind, "_", "")]; ok {
			kind = a
		}
		if want, ok := orgExit[code]; ok && kind != want {
			out = append(out, finding.Finding{Rule: "DS-EXIT", File: consts[code].file, Line: consts[code].line, Subject: fmt.Sprintf("exit %d", code),
				Message: fmt.Sprintf("exit code %d is %q in the code; the organisation table makes %d %q", code, kind, code, want)})
		}
	}
	d := r.Doc(c.ExitDocPath())
	if d == nil {
		return append(out, finding.Finding{Rule: "DS-EXIT", File: c.ExitDocPath(), Subject: c.ExitDocPath(),
			Message: c.ExitDocPath() + " is missing: the exit codes are a public contract of " + c.Name})
	}
	doc := map[int]int{}
	for _, t := range d.Tables() {
		if len(t[0].Cells) < 2 || !strings.Contains(strings.ToLower(t[0].Cells[0]), "code") {
			continue
		}
		for _, row := range t[1:] {
			code, err := strconv.Atoi(strings.Trim(row.Cells[0], "` "))
			if err != nil || len(row.Cells) < 2 {
				continue
			}
			doc[code] = row.Line
			meaning := strings.ToLower(strings.Join(row.Cells[1:], " "))
			if want, ok := orgExit[code]; ok && !containsAny(meaning, orgWordsOf(want)) {
				out = append(out, finding.Finding{Rule: "DS-EXIT", File: d.Path, Line: row.Line, Subject: fmt.Sprintf("exit %d", code),
					Message: fmt.Sprintf("exit code %d is documented as %q; the organisation table makes it %q", code, strings.TrimSpace(row.Cells[1]), want)})
			}
		}
	}
	if len(consts) == 0 {
		return out
	}
	for _, code := range sortedInts(consts) {
		if _, ok := doc[code]; !ok {
			out = append(out, finding.Finding{Rule: "DS-EXIT", File: consts[code].file, Line: consts[code].line, Subject: fmt.Sprintf("exit %d", code),
				Message: fmt.Sprintf("exit code %d (%s) is not in %s", code, consts[code].kind, d.Path)})
		}
	}
	for _, code := range sortedInts(doc) {
		if _, ok := consts[code]; !ok {
			out = append(out, finding.Finding{Rule: "DS-EXIT", File: d.Path, Line: doc[code], Subject: fmt.Sprintf("exit %d", code),
				Message: fmt.Sprintf("exit code %d is documented but no Exit… constant in %s has it", code, c.ExitCodes)})
		}
	}
	return out
}

func orgWordsOf(kind string) []string {
	if w, ok := orgWords[kind]; ok {
		return w
	}
	return []string{strings.ReplaceAll(kind, "_", " ")}
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

type exitConst struct {
	kind, file string
	line       int
}

func sortedInts[V any](m map[int]V) []int {
	k := make([]int, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	slices.Sort(k)
	return k
}

// exitConsts reads the Exit… integer constants of a package directory.
func exitConsts(root, dir string) map[int]exitConst {
	out := map[int]exitConst{}
	fset := token.NewFileSet()
	matches, _ := filepath.Glob(filepath.Join(root, dir, "*.go"))
	for _, p := range matches {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				for i, n := range vs.Names {
					m := exitNameRE.FindStringSubmatch(n.Name)
					if m == nil || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.INT {
						continue
					}
					code, err := strconv.Atoi(lit.Value)
					if err != nil {
						continue
					}
					rel, _ := filepath.Rel(root, p)
					out[code] = exitConst{kind: snake(m[1]), file: filepath.ToSlash(rel), line: fset.Position(n.Pos()).Line}
				}
			}
		}
	}
	return out
}

// snake turns InvalidInput into invalid_input and OK into ok.
func snake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, c := range rs {
		if unicode.IsUpper(c) && i > 0 && (unicode.IsLower(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1])) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// jsonFields checks that the JSON document names every field of the JSON
// output types (DS-JSON).
func jsonFields(r *Repo, c config.CLI) []finding.Finding {
	if c.JSONTypes == "" {
		return nil
	}
	fields := jsonTags(r.Dir, c.JSONTypes)
	if len(fields) == 0 {
		return nil
	}
	d := r.Doc(c.JSONDocPath())
	if d == nil {
		return []finding.Finding{{Rule: "DS-JSON", File: c.JSONDocPath(), Subject: c.JSONDocPath(),
			Message: c.JSONDocPath() + " is missing: the JSON output is a public contract of " + c.Name}}
	}
	text := d.Text()
	var out []finding.Finding
	for _, name := range sortedKeys(fields) {
		if strings.Contains(text, "`"+name+"`") || strings.Contains(text, `"`+name+`"`) {
			continue
		}
		out = append(out, finding.Finding{Rule: "DS-JSON", File: fields[name].file, Line: fields[name].line, Subject: name,
			Message: fmt.Sprintf("JSON field %q is not in %s", name, d.Path)})
	}
	return out
}

// jsonTags returns the JSON field names of the structs of a directory, with
// where each is declared.
func jsonTags(root, dir string) map[string]exitConst {
	out := map[string]exitConst{}
	fset := token.NewFileSet()
	matches, _ := filepath.Glob(filepath.Join(root, dir, "*.go"))
	for _, p := range matches {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(root, p)
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fl := range st.Fields.List {
				if fl.Tag == nil {
					continue
				}
				tag, err := strconv.Unquote(fl.Tag.Value)
				if err != nil {
					continue
				}
				name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
				if name == "" || name == "-" {
					continue
				}
				if _, ok := out[name]; !ok {
					out[name] = exitConst{file: filepath.ToSlash(rel), line: fset.Position(fl.Pos()).Line}
				}
			}
			return true
		})
	}
	return out
}
