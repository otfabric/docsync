// SPDX-License-Identifier: MIT

// Package goapi reads the exported API of a Go module from its source,
// without building it.
package goapi

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Module is a Go module and its packages.
type Module struct {
	Dir       string
	Path      string
	GoVersion string
	// Toolchain is the toolchain directive without "go" (1.26.8), if any.
	Toolchain string
	// Requires maps a required module path to its version.
	Requires map[string]string
	Packages []*Package
	// Tests maps a package name to the test functions (Test…, Fuzz…,
	// Benchmark…, Example…) its _test.go files declare, external test
	// packages (name_test) under the package's name.
	Tests map[string]map[string]bool
	// Imports maps the name a Go file of the module imports a package of
	// another module under (its last path element or its alias) to the
	// import paths: the dependency packages a document can mean.
	Imports map[string][]string
	// Unparsed are the Go files that do not parse (left out).
	Unparsed []string
}

// Package is one package directory.
type Package struct {
	// Rel is the directory relative to the module, "." for the root.
	Rel        string
	ImportPath string
	Name       string
	Main       bool
	Funcs      map[string]*Func
	Types      map[string]*Type
	Values     map[string]*Value

	methods []*Func // until attachMethods
}

// Pos is a declaration's place, relative to the module.
type Pos struct {
	File      string
	Line      int
	Generated bool
}

// Func is a function or method.
type Func struct {
	Pos
	Name string
	// Recv is the receiver's base type name; empty for a function.
	Recv    string
	Params  []string
	Results []string
}

// Signature renders parameter and result type lists.
func Signature(params, results []string) string {
	s := "(" + strings.Join(params, ", ") + ")"
	switch len(results) {
	case 0:
	case 1:
		s += " " + results[0]
	default:
		s += " (" + strings.Join(results, ", ") + ")"
	}
	return s
}

// Type is a named type.
type Type struct {
	Pos
	Name string
	// Fields are the exported field names of a struct type (embedded
	// types by their name).
	Fields []string
	// Methods are the methods declared on the type and, for an interface,
	// its exported methods.
	Methods map[string]*Func
	// Embedded are the names of embedded types (struct fields and
	// interfaces) whose members the type promotes.
	Embedded []string
	// Interface is true for an interface type.
	Interface bool
}

// IsError reports whether the type has an Error() string method.
func (t *Type) IsError() bool {
	m := t.Methods["Error"]
	return m != nil && len(m.Params) == 0 && len(m.Results) == 1 && m.Results[0] == "string"
}

// Value is a constant or variable.
type Value struct {
	Pos
	Name  string
	Const bool
	// TypeName is the named type of a typed constant (declared, or
	// inherited in a const block), else empty.
	TypeName string
	// Error is true for a variable created with errors.New or fmt.Errorf,
	// or named Err... .
	Error bool
}

// Load reads the module in dir. Nested modules, testdata, vendor and hidden
// directories are left out; of the _test.go files only the test functions
// are read.
func Load(dir string) (*Module, error) {
	m := &Module{Dir: dir, Requires: map[string]string{}, Tests: map[string]map[string]bool{}, Imports: map[string][]string{}}
	if err := m.readGoMod(); err != nil {
		return nil, err
	}
	byDir := map[string]*Package{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			name := e.Name()
			if rel != "." && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			if rel != "." {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		if strings.HasSuffix(p, "_test.go") {
			m.addTests(fset, p)
			return nil
		}
		f := parseFile(fset, p)
		if f == nil {
			m.Unparsed = append(m.Unparsed, rel) // go vet reports it
			return nil
		}
		if ignored(f) {
			return nil
		}
		m.addImports(f)
		d := filepath.ToSlash(filepath.Dir(rel))
		pkg := byDir[d]
		if pkg == nil {
			ip := m.Path
			if d != "." {
				ip += "/" + d
			}
			pkg = &Package{Rel: d, ImportPath: ip, Funcs: map[string]*Func{}, Types: map[string]*Type{}, Values: map[string]*Value{}}
			byDir[d] = pkg
		}
		if pkg.Name == "" || f.Name.Name != "main" && pkg.Main {
			pkg.Name = f.Name.Name
			pkg.Main = f.Name.Name == "main"
		}
		if f.Name.Name != pkg.Name {
			return nil // an extra package in the directory (rare): skip
		}
		pkg.add(fset, rel, f, ast.IsGenerated(f), moduleAliases(f, m.Path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, p := range byDir {
		p.attachMethods()
		m.Packages = append(m.Packages, p)
	}
	slices.SortFunc(m.Packages, func(a, b *Package) int { return strings.Compare(a.Rel, b.Rel) })
	return m, nil
}

// addImports records a file's imports of other modules' packages (paths
// with a dot in the first element: not the standard library).
func (m *Module) addImports(f *ast.File) {
	for _, s := range f.Imports {
		p := strings.Trim(s.Path.Value, `"`)
		if p == m.Path || strings.HasPrefix(p, m.Path+"/") || !strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
			continue
		}
		name := p[strings.LastIndexByte(p, '/')+1:]
		if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
			q := p[:strings.LastIndexByte(p, '/')]
			name = q[strings.LastIndexByte(q, '/')+1:] // example.com/x/v2 is x
		}
		names := []string{name} // the package's name, as documents write it
		if s.Name != nil && s.Name.Name != "_" && s.Name.Name != "." {
			names = append(names, s.Name.Name) // and the file's alias
		}
		for _, n := range names {
			if !slices.Contains(m.Imports[n], p) {
				m.Imports[n] = append(m.Imports[n], p)
			}
		}
	}
}

var testFuncRE = regexp.MustCompile(`^(?:Test|Fuzz|Benchmark|Example)(?:$|[^a-z])`)

// addTests records the test functions of a _test.go file.
func (m *Module) addTests(fset *token.FileSet, p string) {
	f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
	if err != nil || ignored(f) {
		return
	}
	m.addImports(f)
	name := strings.TrimSuffix(f.Name.Name, "_test")
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && testFuncRE.MatchString(fd.Name.Name) {
			if m.Tests[name] == nil {
				m.Tests[name] = map[string]bool{}
			}
			m.Tests[name][fd.Name.Name] = true
		}
	}
}

// parseFile parses a Go file, or returns nil when it does not parse.
func parseFile(fset *token.FileSet, path string) *ast.File {
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil
	}
	return f
}

func ignored(f *ast.File) bool {
	for _, c := range f.Comments {
		if c.Pos() > f.Package {
			break
		}
		for _, l := range c.List {
			if strings.HasPrefix(l.Text, "//go:build") && strings.Contains(l.Text, "ignore") {
				return true
			}
		}
	}
	return false
}

var requireRE = regexp.MustCompile(`^(\S+)\s+(v\S+)`)

func (m *Module) readGoMod() error {
	data, err := os.ReadFile(filepath.Join(m.Dir, "go.mod"))
	if err != nil {
		return err
	}
	inRequire := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if i := strings.Index(l, "//"); i >= 0 {
			l = strings.TrimSpace(l[:i])
		}
		switch {
		case inRequire && l == ")":
			inRequire = false
		case inRequire:
			if r := requireRE.FindStringSubmatch(l); r != nil {
				m.Requires[r[1]] = r[2]
			}
		case strings.HasPrefix(l, "module "):
			m.Path = strings.Trim(strings.TrimSpace(l[len("module "):]), `"`)
		case strings.HasPrefix(l, "go "):
			m.GoVersion = strings.TrimSpace(l[len("go "):])
		case strings.HasPrefix(l, "toolchain go"):
			m.Toolchain = strings.TrimSpace(l[len("toolchain go"):])
		case l == "require (":
			inRequire = true
		case strings.HasPrefix(l, "require "):
			if r := requireRE.FindStringSubmatch(strings.TrimSpace(l[len("require "):])); r != nil {
				m.Requires[r[1]] = r[2]
			}
		}
	}
	if m.Path == "" {
		return fmt.Errorf("%s: no module line", filepath.Join(m.Dir, "go.mod"))
	}
	return nil
}

// moduleAliases returns a regexp matching the qualifiers of the file's
// imports of packages of the module (pubsubsec. for an import of
// module/pubsub/security as pubsubsec), or nil.
func moduleAliases(f *ast.File, modPath string) *regexp.Regexp {
	var names []string
	for _, s := range f.Imports {
		p := strings.Trim(s.Path.Value, `"`)
		if p != modPath && !strings.HasPrefix(p, modPath+"/") {
			continue
		}
		name := p[strings.LastIndexByte(p, '/')+1:]
		if s.Name != nil {
			name = s.Name.Name
		}
		names = append(names, regexp.QuoteMeta(name))
	}
	if len(names) == 0 {
		return nil
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(names, "|") + `)\.`)
}

func (p *Package) add(fset *token.FileSet, rel string, f *ast.File, gen bool, local *regexp.Regexp) {
	unqualify := func(ts []string) []string {
		if local != nil {
			for i, t := range ts {
				ts[i] = local.ReplaceAllString(t, "")
			}
		}
		return ts
	}
	pos := func(n ast.Node) Pos {
		return Pos{File: rel, Line: fset.Position(n.Pos()).Line, Generated: gen}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			fn := &Func{Pos: pos(d), Name: d.Name.Name}
			fn.Params, fn.Results = FieldTypes(d.Type)
			fn.Params, fn.Results = unqualify(fn.Params), unqualify(fn.Results)
			if d.Recv != nil && len(d.Recv.List) == 1 {
				fn.Recv = recvName(d.Recv.List[0].Type)
				p.methods = append(p.methods, fn)
				continue
			}
			p.Funcs[fn.Name] = fn
		case *ast.GenDecl:
			constType := ""
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					t := &Type{Pos: pos(s), Name: s.Name.Name, Methods: map[string]*Func{}}
					switch tt := s.Type.(type) {
					case *ast.StructType:
						for _, fl := range tt.Fields.List {
							if len(fl.Names) == 0 {
								n := recvName(fl.Type)
								if ast.IsExported(n) {
									t.Fields = append(t.Fields, n)
								}
								t.Embedded = append(t.Embedded, n)
							}
							for _, n := range fl.Names {
								if n.IsExported() {
									t.Fields = append(t.Fields, n.Name)
								}
							}
						}
					case *ast.InterfaceType:
						t.Interface = true
						for _, fl := range tt.Methods.List {
							ft, ok := fl.Type.(*ast.FuncType)
							if !ok {
								t.Embedded = append(t.Embedded, recvName(fl.Type))
								continue
							}
							for _, n := range fl.Names {
								if n.IsExported() {
									mf := &Func{Pos: pos(fl), Name: n.Name, Recv: t.Name}
									mf.Params, mf.Results = FieldTypes(ft)
									mf.Params, mf.Results = unqualify(mf.Params), unqualify(mf.Results)
									t.Methods[n.Name] = mf
								}
							}
						}
					}
					p.Types[t.Name] = t
				case *ast.ValueSpec:
					if d.Tok == token.CONST {
						switch {
						case s.Type != nil:
							constType = recvName(s.Type)
						case len(s.Values) > 0:
							constType = "" // a new untyped expression
						}
					}
					for i, n := range s.Names {
						if !n.IsExported() {
							continue
						}
						v := &Value{Pos: pos(n), Name: n.Name, Const: d.Tok == token.CONST}
						if v.Const && ast.IsExported(constType) {
							v.TypeName = constType
						}
						if !v.Const {
							v.Error = strings.HasPrefix(n.Name, "Err") && len(n.Name) > 3 && ast.IsExported(n.Name[3:])
							if i < len(s.Values) && isErrorCall(s.Values[i]) {
								v.Error = true
							}
						}
						p.Values[v.Name] = v
					}
				}
			}
		}
	}
}

func isErrorCall(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := s.X.(*ast.Ident)
	return ok && (x.Name == "errors" && s.Sel.Name == "New" || x.Name == "fmt" && s.Sel.Name == "Errorf")
}

func (p *Package) attachMethods() {
	for _, fn := range p.methods {
		t := p.Types[fn.Recv]
		if t == nil {
			continue // a method of an unexported type
		}
		t.Methods[fn.Name] = fn
	}
	p.methods = nil
}

func recvName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.SelectorExpr:
			return t.Sel.Name
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// FieldTypes returns a function type's parameter and result types, one per
// name.
func FieldTypes(ft *ast.FuncType) (params, results []string) {
	expand := func(fl *ast.FieldList) []string {
		if fl == nil {
			return nil
		}
		var out []string
		for _, f := range fl.List {
			s := types.ExprString(f.Type)
			n := max(len(f.Names), 1)
			for range n {
				out = append(out, s)
			}
		}
		return out
	}
	return expand(ft.Params), expand(ft.Results)
}

// Public reports whether the package's identifiers need reference entries:
// not a main package and not in a directory skip matches.
func (p *Package) Public(skip func(rel string) bool) bool {
	return !p.Main && !skip(p.Rel)
}

// Index answers lookups by name across the module.
type Index struct {
	// PkgNames are the package names of the module's non-main packages.
	PkgNames map[string][]*Package
	// Members maps an exported name to true when some package, type or
	// interface of the module declares it (function, method, field, type,
	// constant or variable).
	Members map[string]bool
	// Types maps a type name to every type of that name.
	Types map[string][]*Type
	// Tests are the test functions of the _test.go files by package name
	// (Module.Tests).
	Tests map[string]map[string]bool
	// Imports are the dependency packages the module imports, by name
	// (Module.Imports).
	Imports map[string][]string
}

// IsTest reports whether a _test.go file of a package named pkg declares
// the test function name (Test…, Fuzz…, Benchmark…, Example…).
func (ix *Index) IsTest(pkg, name string) bool { return ix.Tests[pkg][name] }

// NewIndex indexes the module.
func NewIndex(m *Module) *Index {
	ix := &Index{PkgNames: map[string][]*Package{}, Members: map[string]bool{}, Types: map[string][]*Type{}, Tests: m.Tests, Imports: m.Imports}
	for _, p := range m.Packages {
		if !p.Main {
			ix.PkgNames[p.Name] = append(ix.PkgNames[p.Name], p)
		}
		for n := range p.Funcs {
			ix.Members[n] = true
		}
		for n := range p.Values {
			ix.Members[n] = true
		}
		for n, t := range p.Types {
			ix.Members[n] = true
			ix.Types[n] = append(ix.Types[n], t)
			for mn := range t.Methods {
				ix.Members[mn] = true
			}
			for _, f := range t.Fields {
				ix.Members[f] = true
			}
		}
	}
	return ix
}

// ExportedNames returns the exported top-level names (and the methods and
// fields of exported types) of the package in dir, a dependency's: its
// non-test Go files, whatever their build constraints.
func ExportedNames(dir string) map[string]bool {
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		p := &Package{Funcs: map[string]*Func{}, Types: map[string]*Type{}, Values: map[string]*Value{}}
		p.add(fset, f, af, false, nil)
		p.attachMethods()
		for n := range p.Funcs {
			out[n] = true
		}
		for n := range p.Values {
			out[n] = true
		}
		for n, t := range p.Types {
			out[n] = true
			for mn := range t.Methods {
				out[mn] = true
			}
		}
	}
	return out
}

// Has reports whether one of the packages declares name at top level.
func Has(pkgs []*Package, name string) bool {
	for _, p := range pkgs {
		if p.Funcs[name] != nil || p.Types[name] != nil || p.Values[name] != nil {
			return true
		}
	}
	return false
}
