// SPDX-License-Identifier: MIT

package check

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/docsync/internal/config"
	"github.com/otfabric/docsync/internal/goapi"
	"github.com/otfabric/docsync/internal/repofs"
)

// TestConfiguredDirsDeclareSomething: an exit_codes or json_types directory
// that declares nothing (a mistyped path) is a DS-CONF finding, not a check
// that is quietly off.
func TestConfiguredDirsDeclareSomething(t *testing.T) {
	dir := t.TempDir()
	for rel, s := range map[string]string{
		"internal/errs/errs.go":   "package errs\n\nconst ExitOK = 0\n",
		"internal/out/out.go":     "package out\n\ntype R struct {\n\tA int `json:\"a\"`\n}\n",
		"internal/empty/empty.go": "package empty\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fsys, err := repofs.Open(dir, repofs.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	r := &Repo{Dir: dir, FS: fsys}
	both := []string{"x exit_codes", "x json_types"}
	for _, tc := range []struct {
		c    config.CLI
		want []string
	}{
		{config.CLI{Name: "x", ExitCodes: "internal/errs", JSONTypes: "internal/out"}, nil},
		{config.CLI{Name: "x"}, nil},
		{config.CLI{Name: "x", ExitCodes: "internal/empty", JSONTypes: "internal/empty"}, both},
		{config.CLI{Name: "x", ExitCodes: "internal/nope", JSONTypes: "internal/nope"}, both},
		{config.CLI{Name: "x", ExitCodes: "internal/out", JSONTypes: "internal/errs"}, both},
	} {
		var got []string
		for _, f := range append(exitCodes(r, tc.c), jsonFields(r, tc.c)...) {
			if f.Rule == "DS-CONF" {
				if f.File != config.File || !strings.Contains(f.Message, "remove the key") {
					t.Errorf("%+v: %+v", tc.c, f)
				}
				got = append(got, f.Subject)
			}
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%+v: DS-CONF subjects %q, want %q", tc.c, got, tc.want)
		}
	}
	if len(fsys.Refused()) != 0 {
		t.Errorf("refused: %v", fsys.Refused())
	}
}

func TestShellWords(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
		ok   bool
	}{
		{`a "b c" 'd e' f\ g`, []string{"a", "b c", "d e", "f g"}, true},
		{`a | b && c; d || e & f`, []string{"a", "|", "b", "&&", "c", ";", "d", "||", "e", "&", "f"}, true},
		{`a 2>&1 # rest`, []string{"a", "2>&1"}, true},
		{`a "\"q\""`, []string{"a", `"q"`}, true},
		{`a "open`, nil, false},
		{`a 'open`, nil, false},
	} {
		got, ok := shellWords(tc.in)
		if ok != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("shellWords(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCLIInvocations(t *testing.T) {
	got := cliInvocations("x", `sudo FOO=1 ./bin/x read -u a > out | x.exe write && env x help 2>/dev/null; other x`)
	want := [][]string{{"read", "-u", "a"}, {"write"}, {"help"}}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("%d: got %q, want %q", i, got[i], want[i])
		}
	}
	if cliInvocations("x", `x "open`) != nil {
		t.Error("an open quote splits")
	}
}

func TestValidate(t *testing.T) {
	cmds := map[string]Command{
		"":    {Runnable: false, Subcommands: []string{"get"}, Flags: []Flag{{Name: "verbose", Shorthand: "v", Bool: true, Local: true}}},
		"get": {Runnable: true, Aliases: []string{"g"}, Flags: []Flag{{Name: "url", Shorthand: "u", Required: true}, {Name: "verbose", Shorthand: "v", Bool: true}, {Name: "n", Shorthand: "n"}}},
	}
	for args, want := range map[string]string{
		"get -u x":             "",
		"g --url=x -v":         "",
		"get -vu x":            "",
		"get --help":           "",
		"get -n3 -u x":         "",
		"get <url> -u x -- -z": "",
		"":                     "",
		"help get":             "",
		"get":                  "required flag --url of `x get` is not set",
		"get -z":               "unknown flag -z for `x get`",
		"nope":                 "unknown command \"nope\" for `x`",
	} {
		var a []string
		if args != "" {
			a, _ = shellWords(args)
		}
		if got := validate("x", cmds, a); got != want {
			t.Errorf("validate(%q) = %q, want %q", args, got, want)
		}
	}
}

func TestSnake(t *testing.T) {
	for in, want := range map[string]string{"OK": "ok", "InvalidInput": "invalid_input", "Usage": "usage", "IOError": "io_error", "Code42": "code42"} {
		if got := snake(in); got != want {
			t.Errorf("snake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameTypes(t *testing.T) {
	if !sameTypes([]string{"ctx", "uint16"}, []string{"context.Context", "uint16"}) {
		t.Error("a name-only parameter matches any type")
	}
	if sameTypes([]string{"string"}, []string{"context.Context"}) || sameTypes([]string{"a"}, nil) || sameTypes([]string{"Config"}, []string{"*Config"}) {
		t.Error("different types match")
	}
}

func TestPlaceholderName(t *testing.T) {
	for _, n := range []string{"ErrXxx", "ErrX", "X", "XRequest", "MyStruct", "FooBar"} {
		if !placeholderName(n) {
			t.Errorf("%s is a placeholder", n)
		}
	}
	for _, n := range []string{"ErrClosed", "XMLDecoder", "Client"} {
		if placeholderName(n) {
			t.Errorf("%s is not a placeholder", n)
		}
	}
}

func TestSplitDeclsStatements(t *testing.T) {
	src := []string{"type T struct{", "\tA int", "}", "", "func (T) M() {}", "t := T{}", "t.M()", "func F() {}"}
	decl, _, stmt, _ := splitDeclsStatements(src, make([]int, len(src)))
	if len(decl) != 6 || len(stmt) != 2 {
		t.Fatalf("decl %q stmt %q", decl, stmt)
	}
}

func TestNameListings(t *testing.T) {
	got := nameListings([]string{"var (", "\tErrClosed // closed", "", "\tErrLost", ")", "x := 1", "const (", "\tA = 1", ")"})
	want := []string{"var _ = []any{", "\tErrClosed, // closed", "", "\tErrLost, ", "}", "x := 1", "const (", "\tA = 1", ")"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", got)
	}
}

func TestBestPackage(t *testing.T) {
	pkg := func(path string, names ...string) *goapi.Package {
		p := &goapi.Package{ImportPath: path, Funcs: map[string]*goapi.Func{}}
		for _, n := range names {
			p.Funcs[n] = &goapi.Func{Name: n}
		}
		return p
	}
	for _, tc := range []struct {
		name  string
		pkgs  []*goapi.Package
		names []string
		want  string // import path, "" for none
	}{
		{"none", nil, []string{"New"}, ""},
		{"more names win over a shorter path", []*goapi.Package{pkg("m/errors"), pkg("m/a/b/errors", "New")}, []string{"New"}, "m/a/b/errors"},
		{"fewer names lose", []*goapi.Package{pkg("m/a/b/errors", "New"), pkg("m/errors")}, []string{"New"}, "m/a/b/errors"},
		{"public over internal", []*goapi.Package{pkg("m/internal/errors", "New"), pkg("m/x/errors", "New")}, []string{"New"}, "m/x/errors"},
		{"internal after public loses", []*goapi.Package{pkg("m/x/errors", "New"), pkg("m/internal/errors", "New")}, []string{"New"}, "m/x/errors"},
		{"a directory named internal", []*goapi.Package{pkg("m/x/internal", "New"), pkg("m/long/path/internal2", "New")}, []string{"New"}, "m/long/path/internal2"},
		{"shorter path on a tie", []*goapi.Package{pkg("m/a/b/errors", "New"), pkg("m/errors", "New")}, []string{"New"}, "m/errors"},
		{"longer path on a tie loses", []*goapi.Package{pkg("m/errors"), pkg("m/a/errors")}, nil, "m/errors"},
	} {
		got := ""
		if p := bestPackage(tc.pkgs, tc.names); p != nil {
			got = p.ImportPath
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHasAny(t *testing.T) {
	p := &goapi.Package{Funcs: map[string]*goapi.Func{"New": {Name: "New"}}}
	for _, tc := range []struct {
		names []string
		want  bool
	}{
		{nil, false},
		{[]string{"Wrap"}, false},
		{[]string{"Wrap", "New"}, true},
	} {
		if got := hasAny(p, tc.names); got != tc.want {
			t.Errorf("hasAny(%q) = %v, want %v", tc.names, got, tc.want)
		}
	}
}

func TestIsInternal(t *testing.T) {
	for path, want := range map[string]bool{
		"m/internal":       true,
		"m/internal/wire":  true,
		"m/x/internal/y":   true,
		"m/internalwire":   false,
		"m/transport/wire": false,
		"internal":         false,
	} {
		if got := isInternal(path); got != want {
			t.Errorf("isInternal(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestStripSpans(t *testing.T) {
	for in, want := range map[string]string{
		"no code":             "no code",
		"a `ErrX` b":          "a        b",
		"a ``x ` y`` b":       "a " + strings.Repeat(" ", len("``x ` y``")) + " b",
		"open ` ErrX":         "open ` ErrX",
		"`a` and ``open ErrX": "    and ``open ErrX",
		"``` ErrX":            "``` ErrX",
		"`a``b` c":            "       c",
	} {
		if got := stripSpans(in); got != want {
			t.Errorf("stripSpans(%q) = %q, want %q", in, got, want)
		}
	}
}
