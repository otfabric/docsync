// SPDX-License-Identifier: MIT

package goapi

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeFiles writes name -> content under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadImportsAndTests(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"m.go": `package m

import (
	"fmt"

	"example.com/m/sub"
	x "github.com/dep/x/v2"
	_ "github.com/dep/blank"
	. "github.com/dep/dot"
	"github.com/dep/v2x"
	"github.com/dep/v"
)

var _ = fmt.Sprint(sub.A, x.B)
`,
		"n.go": `package m

import "github.com/dep/x/v2"
`,
		"sub/sub.go": "package sub\n\nconst A = 1\n",
		"m_test.go": `package m

import (
	"testing"

	"github.com/dep/testonly"
)

type T struct{}

func (T) TestMethod(t *testing.T) {}
func TestA(t *testing.T)          {}
func Test_b(t *testing.T)         {}
func Testify()                    {}
func FuzzB(f *testing.F)          {}
func BenchmarkC(b *testing.B)     {}
func ExampleD()                   {}
func Example()                    {}
func helper()                     {}

var _ = testonly.X
`,
		"ext_test.go": `package m_test

import "testing"

func TestExt(t *testing.T) {}
`,
		"bad_test.go": "package m\n\nfunc TestBroken( {\n",
	})
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	wantImports := map[string][]string{
		"x":        {"github.com/dep/x/v2"}, // the path's v2 element is skipped; the alias is the same name
		"blank":    {"github.com/dep/blank"},
		"dot":      {"github.com/dep/dot"},
		"v2x":      {"github.com/dep/v2x"},
		"v":        {"github.com/dep/v"},
		"testonly": {"github.com/dep/testonly"},
	}
	if !maps.EqualFunc(m.Imports, wantImports, slices.Equal) {
		t.Errorf("Imports = %v, want %v", m.Imports, wantImports)
	}

	wantTests := map[string]map[string]bool{
		"m": {"TestA": true, "Test_b": true, "FuzzB": true, "BenchmarkC": true, "ExampleD": true, "Example": true, "TestExt": true},
	}
	if !maps.EqualFunc(m.Tests, wantTests, maps.Equal) {
		t.Errorf("Tests = %v, want %v", m.Tests, wantTests)
	}

	ix := NewIndex(m)
	for _, tc := range []struct {
		pkg, name string
		want      bool
	}{
		{"m", "TestA", true},
		{"m", "TestExt", true},
		{"m", "helper", false},
		{"m", "TestMethod", false},
		{"other", "TestA", false},
	} {
		if got := ix.IsTest(tc.pkg, tc.name); got != tc.want {
			t.Errorf("IsTest(%q, %q) = %v, want %v", tc.pkg, tc.name, got, tc.want)
		}
	}
	if got := ix.Imports["x"]; !slices.Equal(got, []string{"github.com/dep/x/v2"}) {
		t.Errorf("Index.Imports[x] = %v", got)
	}
}

func TestLoadAlias(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/m\n",
		"m.go":   "package m\n\nimport y \"github.com/dep/x\"\n\nvar _ = y.A\n",
	})
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"x": {"github.com/dep/x"}, "y": {"github.com/dep/x"}}
	if !maps.EqualFunc(m.Imports, want, slices.Equal) {
		t.Errorf("Imports = %v, want %v", m.Imports, want)
	}
}

func TestExportedNames(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.go": `package dep

const C = 1

var V, unexported = 1, 2

func F() {}
func f() {}

type T struct{ Field int }

func (T) M() {}

type I interface{ IM() }
`,
		"a_test.go": "package dep\n\nfunc TestOnly() {}\n\nvar X = 1\n",
		"bad.go":    "package dep\n\nfunc Broken( {\n",
	})
	got := ExportedNames(dir)
	for _, n := range []string{"C", "V", "F", "T", "M", "I", "IM"} {
		if !got[n] {
			t.Errorf("%s is missing from %v", n, got)
		}
	}
	for _, n := range []string{"unexported", "f", "TestOnly", "X", "Broken"} {
		if got[n] {
			t.Errorf("%s is in %v", n, got)
		}
	}
	if got := ExportedNames(filepath.Join(dir, "missing")); len(got) != 0 {
		t.Errorf("a missing directory: %v", got)
	}
}
