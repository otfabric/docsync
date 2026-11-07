// SPDX-License-Identifier: MIT

package check

import (
	"slices"
	"strings"
	"testing"
)

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
