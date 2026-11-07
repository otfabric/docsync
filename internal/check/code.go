// SPDX-License-Identifier: MIT

package check

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/otfabric/docsync/internal/finding"
	"github.com/otfabric/docsync/internal/goapi"
	"github.com/otfabric/docsync/internal/markdown"
)

// goBlock is one Go code block prepared for checking.
type goBlock struct {
	doc  *markdown.Doc
	blk  markdown.Block
	file *ast.File
	fset *token.FileSet
	// lines maps a line of the prepared source to the document line.
	lines []int
	// signature is true for a block of declarations without bodies: a
	// listing of the API, compared with the code instead of compiled.
	signature bool
	// body is true for statements wrapped in a function: what they return
	// is unknown.
	body bool
	// parseOnly is true for a fragment that parses only as the fields of
	// a composite literal: its types are unknown.
	parseOnly bool
	imports   map[string]string // name -> import path
}

var (
	ellipsisLineRE = regexp.MustCompile(`^\s*(\.\.\.|…)\s*$`)
	ellipsisRE     = regexp.MustCompile(`([{(\[])\s*(\.\.\.|…)\s*([})\]])`)
	ellipsisArgRE  = regexp.MustCompile(`,\s*(\.\.\.|…)\s*([)}\]])`)
	// x := ..., Field: ..., return ...: an elided value.
	// elided stands for an elided value: undefined, so it has no type and
	// raises no error beyond its own, which is ignored.
	elided          = "elided_"
	ellipsisValueRE = regexp.MustCompile(`(:=|[^=!<>]=|:|\breturn)\s*(\.\.\.|…)(\s*[,)}\]]|\s*$)`)
	importLineRE    = regexp.MustCompile(`^import\s+(?:([A-Za-z_.][A-Za-z0-9_]*)\s+)?"([^"]+)"\s*$`)
)

// prepare parses one block: as a whole file, as declarations, or as the body
// of a function. It returns an error when none of them parses.
func prepare(d *markdown.Doc, b markdown.Block) (*goBlock, error) {
	var src []string
	var origin []int
	var imports []string
	var importOrigin []int
	inImport := false
	full := strings.HasPrefix(firstCode(b.Lines), "package ")
	for i, l := range b.Lines {
		n := b.Start + i
		t := strings.TrimSpace(l)
		switch {
		case ellipsisLineRE.MatchString(l):
			l = ""
		case full:
		case inImport:
			imports, importOrigin = append(imports, l), append(importOrigin, n)
			if t == ")" {
				inImport = false
			}
			continue
		case t == "import (":
			inImport = true
			imports, importOrigin = append(imports, l), append(importOrigin, n)
			continue
		case importLineRE.MatchString(t):
			imports, importOrigin = append(imports, l), append(importOrigin, n)
			continue
		}
		l = ellipsisRE.ReplaceAllString(l, "$1$3")
		l = ellipsisArgRE.ReplaceAllString(l, "$2")
		l = ellipsisValueRE.ReplaceAllString(l, "$1 "+elided+"$3")
		src, origin = append(src, l), append(origin, n)
	}
	try := func(lines []string, lineOrigin []int) (*goBlock, error) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, d.Path, strings.Join(lines, "\n"), parser.ParseComments|parser.AllErrors)
		if err != nil {
			return nil, err
		}
		return &goBlock{doc: d, blk: b, file: f, fset: fset, lines: lineOrigin}, nil
	}
	if full {
		return try(src, origin)
	}
	head := append([]string{"package p"}, imports...)
	headOrigin := append([]int{0}, importOrigin...)
	wrap := func(open string, lines []string, lineOrigin []int) ([]string, []int) {
		l := append(append(slices.Clone(head), open), lines...)
		o := append(append(slices.Clone(headOrigin), 0), lineOrigin...)
		return append(l, "}"), append(o, 0)
	}
	// Declarations.
	g, errDecl := try(append(slices.Clone(head), src...), append(slices.Clone(headOrigin), origin...))
	if errDecl == nil {
		g.signature = isSignatureBlock(g.file)
		return g, nil
	}
	// Declarations, then statements (a type and the code that uses it).
	if decl, declO, stmt, stmtO := splitDeclsStatements(src, origin); len(decl) > 0 && len(stmt) > 0 {
		l, o := wrap("func _() {", stmt, stmtO)
		l = append(append(slices.Clone(head), decl...), l[len(head):]...)
		o = append(append(slices.Clone(headOrigin), declO...), o[len(headOrigin):]...)
		if g, err := try(l, o); err == nil {
			g.body = true
			return g, nil
		}
	}
	// Statements.
	l, o := wrap("func _() {", src, origin)
	g, errBody := try(l, o)
	if errBody == nil {
		g.body = true
		return g, nil
	}
	// Method specifications: the methods of an interface.
	l, o = wrap("type _ interface {", src, origin)
	if g, err := try(l, o); err == nil {
		g.signature = true
		return g, nil
	}
	// Fields of a composite literal (Config{ ... } shown in part).
	l, o = wrap("var _ = struct{}{", src, origin)
	if g, err := try(l, o); err == nil {
		g.parseOnly = true
		return g, nil
	}
	if declStart(firstCode(src)) {
		return nil, errDecl
	}
	return nil, errBody
}

func declStart(l string) bool {
	for _, k := range []string{"func ", "type ", "var ", "const ", "import "} {
		if strings.HasPrefix(l, k) {
			return true
		}
	}
	return false
}

// splitDeclsStatements separates the top-level declarations of a fragment
// from its statements: a line at depth 0 that starts with func or type opens
// a declaration, any other code line at depth 0 a statement.
func splitDeclsStatements(src []string, origin []int) (decl []string, declO []int, stmt []string, stmtO []int) {
	depth := 0
	inDecl := true
	for i, l := range src {
		t := strings.TrimSpace(l)
		if depth == 0 && t != "" && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "}") && !strings.HasPrefix(t, ")") {
			inDecl = strings.HasPrefix(t, "func ") || strings.HasPrefix(t, "type ") ||
				inDecl && (strings.HasPrefix(t, "var ") || strings.HasPrefix(t, "const "))
		}
		if inDecl {
			decl, declO = append(decl, l), append(declO, origin[i])
		} else {
			stmt, stmtO = append(stmt, l), append(stmtO, origin[i])
		}
		depth += strings.Count(l, "{") + strings.Count(l, "(") - strings.Count(l, "}") - strings.Count(l, ")")
		depth = max(depth, 0)
	}
	return decl, declO, stmt, stmtO
}

// placeholderName reports whether an identifier stands for one the reader
// picks: ErrXxx, X, XRequest, MyStruct.
func placeholderName(n string) bool {
	n = strings.TrimPrefix(n, "Err")
	return strings.Contains(n, "Xxx") || strings.Contains(n, "XXX") || n == "X" || n == "T" || n == "" || placeholderRE.MatchString(n)
}

var placeholderRE = regexp.MustCompile(`^(?:My[A-Z]|Your[A-Z]|Foo|Bar|X(?:Request|Response|Result|Options?|Handler|Event|Error|Config)$)`)

func firstCode(lines []string) string {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "//") {
			return t
		}
	}
	return ""
}

// isSignatureBlock reports whether every function of the file is declared
// without a body (an API listing).
func isSignatureBlock(f *ast.File) bool {
	funcs := 0
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			funcs++
			if fd.Body != nil {
				return false
			}
		}
	}
	if funcs > 0 {
		return true
	}
	// Only type, const and var declarations: a listing too when it has a
	// type declaration (type Config struct { ... }).
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			return true
		}
	}
	return false
}

func (g *goBlock) line(p token.Pos) int {
	n := g.fset.Position(p).Line
	if n >= 1 && n <= len(g.lines) && g.lines[n-1] > 0 {
		return g.lines[n-1]
	}
	return g.blk.Start
}

// Code checks the Go code blocks of the documents (DS-CODE, DS-SIG).
func Code(r *Repo) []finding.Finding {
	if r.Module == nil {
		return nil
	}
	var out []finding.Finding
	var compile []*goBlock
	for _, d := range r.Docs {
		for _, b := range d.Blocks {
			if b.Lang != "go" && b.Lang != "golang" {
				continue
			}
			g, err := prepare(d, b)
			if err != nil {
				out = append(out, finding.Finding{Rule: "DS-CODE", File: d.Path, Line: b.Start, Subject: fmt.Sprintf("%s:%d", d.Path, b.Start),
					Message: "Go block does not parse: " + firstError(err) + " (fix it, or mark a non-Go block with another language)"})
				continue
			}
			if g.parseOnly {
				continue
			}
			if g.signature {
				if r.Config.HasAPI() {
					out = append(out, signatures(r, g)...)
				}
				continue
			}
			compile = append(compile, g)
		}
	}
	if len(compile) == 0 {
		return out
	}
	tc, err := newTypeChecker(r, compile)
	if err != nil {
		return append(out, finding.Finding{Rule: "DS-CONF", File: "go.mod", Subject: "typecheck",
			Message: "cannot load the module to type-check Go blocks: " + err.Error()})
	}
	defer tc.close()
	for _, g := range compile {
		out = append(out, tc.check(g)...)
	}
	return out
}

func firstError(err error) string {
	var el interface{ Unwrap() []error }
	if errors.As(err, &el) && len(el.Unwrap()) > 0 {
		err = el.Unwrap()[0]
	}
	s := err.Error()
	if i := strings.Index(s, ": "); i >= 0 && strings.Contains(s[:i], ":") {
		s = s[i+2:]
	}
	return s
}

// typeChecker type-checks blocks against export data that go list builds in
// a scratch module requiring the checked one.
type typeChecker struct {
	r       *Repo
	dir     string
	exports map[string]string
	std     map[string][]string
	goVer   string
}

func newTypeChecker(r *Repo, blocks []*goBlock) (*typeChecker, error) {
	tc := &typeChecker{r: r, exports: map[string]string{}, goVer: r.Module.GoVersion}
	var err error
	if tc.std, err = stdPackages(r.GoBin); err != nil {
		return nil, err
	}
	need := map[string]bool{}
	for _, g := range blocks {
		g.imports = tc.resolveImports(g)
		for _, p := range g.imports {
			need[p] = true
		}
	}
	if len(need) == 0 {
		return tc, nil
	}
	if tc.dir, err = os.MkdirTemp("", "docsync-"); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(r.Dir)
	if err != nil {
		return nil, err
	}
	gomod, err := os.ReadFile(filepath.Join(r.Dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	var mod bytes.Buffer
	fmt.Fprintf(&mod, "module docsync.check\n\ngo %s\n\nrequire %s v0.0.0\n\nreplace %s => %s\n", tc.goVer, r.Module.Path, r.Module.Path, abs)
	// Keep the checked module's dependency versions.
	for p, v := range r.Module.Requires {
		fmt.Fprintf(&mod, "require %s %s\n", p, v)
	}
	for _, l := range strings.Split(string(gomod), "\n") {
		if !strings.HasPrefix(l, "replace ") {
			continue
		}
		// A local replacement is relative to the checked module.
		if left, right, ok := strings.Cut(l, "=>"); ok {
			if target := strings.TrimSpace(right); strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") {
				l = left + "=> " + filepath.Join(abs, target)
			}
		}
		mod.WriteString(l + "\n")
	}
	if err := os.WriteFile(filepath.Join(tc.dir, "go.mod"), mod.Bytes(), 0o600); err != nil {
		return nil, err
	}
	if sum, err := os.ReadFile(filepath.Join(r.Dir, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(tc.dir, "go.sum"), sum, 0o600); err != nil {
			return nil, err
		}
	}
	paths := sortedKeys(need)
	args := append([]string{"list", "-e", "-export", "-deps", "-json=ImportPath,Export,Error"}, paths...)
	cmd := exec.Command(r.GoBin, args...)
	cmd.Dir = tc.dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outb, err := cmd.Output()
	if err != nil && len(outb) == 0 {
		return nil, fmt.Errorf("go list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(outb))
	for {
		var p struct {
			ImportPath, Export string
			Error              *struct{ Err string }
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if p.Export != "" && p.Error == nil {
			tc.exports[p.ImportPath] = p.Export
		}
	}
	return tc, nil
}

func (tc *typeChecker) close() {
	if tc.dir != "" {
		_ = os.RemoveAll(tc.dir)
	}
}

// stdPreferred settles standard package names that several paths share.
var stdPreferred = map[string]string{
	"rand": "math/rand", "template": "text/template", "scanner": "text/scanner", "pprof": "runtime/pprof",
	"trace": "runtime/trace", "constraint": "go/build/constraint", "metrics": "runtime/metrics",
}

func stdPackages(goBin string) (map[string][]string, error) {
	out, err := exec.Command(goBin, "list", "std").Output()
	if err != nil {
		return nil, fmt.Errorf("go list std: %w", err)
	}
	m := map[string][]string{}
	for _, p := range strings.Fields(string(out)) {
		if strings.Contains(p, "internal") || strings.HasPrefix(p, "vendor/") || strings.HasPrefix(p, "cmd/") {
			continue
		}
		m[path.Base(p)] = append(m[path.Base(p)], p)
	}
	return m, nil
}

// resolveImports returns the block's imports: those it states, and for each
// unresolved selector base the module or standard package of that name.
func (tc *typeChecker) resolveImports(g *goBlock) map[string]string {
	imp := map[string]string{}
	for _, s := range g.file.Imports {
		p := strings.Trim(s.Path.Value, `"`)
		name := path.Base(p)
		if s.Name != nil {
			name = s.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		imp[name] = p
	}
	unresolved := map[string]bool{}
	for _, id := range g.file.Unresolved {
		unresolved[id.Name] = true
	}
	ast.Inspect(g.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || !unresolved[x.Name] || imp[x.Name] != "" {
			return true
		}
		if pkgs := tc.r.Index.PkgNames[x.Name]; len(pkgs) > 0 {
			best := pkgs[0]
			for _, p := range pkgs[1:] {
				if len(p.ImportPath) < len(best.ImportPath) {
					best = p
				}
			}
			imp[x.Name] = best.ImportPath
			return true
		}
		if p, ok := stdPreferred[x.Name]; ok {
			imp[x.Name] = p
		} else if ps := tc.std[x.Name]; len(ps) == 1 {
			imp[x.Name] = ps[0]
		}
		return true
	})
	return imp
}

var ignoredErrRE = regexp.MustCompile(`declared and not used|imported and not used|" imported as \w+ and not used|missing return|missing function body|is not used|defined and not used|no new variables on left side|redeclared in this block|other declaration of|must have no arguments and no return values|func init must have no|cannot use _ as value`)

func (tc *typeChecker) check(g *goBlock) []finding.Finding {
	for _, p := range g.imports {
		if tc.exports[p] == "" {
			return nil // a package go list could not build (not a dependency of the module)
		}
	}
	// Add the inferred imports to the file.
	var specs []ast.Spec
	have := map[string]bool{}
	for _, s := range g.file.Imports {
		have[strings.Trim(s.Path.Value, `"`)] = true
	}
	for _, name := range sortedKeys(g.imports) {
		p := g.imports[name]
		if have[p] {
			continue
		}
		spec := &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: `"` + p + `"`}}
		if path.Base(p) != name {
			spec.Name = ast.NewIdent(name)
		}
		specs = append(specs, spec)
	}
	if len(specs) > 0 {
		g.file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: specs}}, g.file.Decls...)
	}
	var errs []types.Error
	conf := types.Config{
		GoVersion: "go" + tc.goVer,
		Importer: importer.ForCompiler(g.fset, "gc", func(p string) (io.ReadCloser, error) {
			f := tc.exports[p]
			if f == "" {
				return nil, fmt.Errorf("no export data for %s", p)
			}
			return os.Open(f)
		}),
		Error: func(err error) {
			var te types.Error
			if errors.As(err, &te) {
				errs = append(errs, te)
			}
		},
	}
	_, _ = conf.Check("p", g.fset, []*ast.File{g.file}, nil)
	var out []finding.Finding
	seen := map[string]bool{}
	for _, e := range errs {
		if e.Soft && strings.Contains(e.Msg, "not used") || ignoredErrRE.MatchString(e.Msg) {
			continue
		}
		if g.body && (strings.HasPrefix(e.Msg, "too many return values") || strings.HasPrefix(e.Msg, "not enough return values")) {
			continue
		}
		if name, ok := strings.CutPrefix(e.Msg, "undefined: "); ok && !strings.Contains(name, ".") &&
			(!ast.IsExported(name) || placeholderName(name) || tc.r.Index.Members[name]) {
			continue // a local of the example, a placeholder, or the package's own name written unqualified
		}
		line := g.line(e.Pos)
		k := fmt.Sprint(line, e.Msg)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, finding.Finding{Rule: "DS-CODE", File: g.doc.Path, Line: line, Subject: fmt.Sprintf("%s:%d", g.doc.Path, line),
			Message: "Go block does not compile: " + e.Msg})
	}
	return out
}

// signatures compares an API listing with the code (DS-SIG).
func signatures(r *Repo, g *goBlock) []finding.Finding {
	ix := r.Index
	qual := pkgQualifierRE(ix)
	norm := func(ts []string) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			t = strings.ReplaceAll(t, "interface{}", "any")
			if qual != nil {
				t = qual.ReplaceAllString(t, "")
			}
			out[i] = t
		}
		return out
	}
	var out []finding.Finding
	add := func(p token.Pos, subject, msg string) {
		out = append(out, finding.Finding{Rule: "DS-SIG", File: g.doc.Path, Line: g.line(p), Subject: subject, Message: msg})
	}
	for _, d := range g.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() || placeholderName(d.Name.Name) {
				continue
			}
			name := d.Name.Name
			recv := ""
			if d.Recv != nil && len(d.Recv.List) == 1 {
				recv = recvBase(d.Recv.List[0].Type)
			}
			if placeholderName(recv) {
				continue
			}
			params, results := goapi.FieldTypes(d.Type)
			want := goapi.Signature(norm(params), norm(results))
			var cands []*goapi.Func
			if recv != "" {
				ts := ix.Types[recv]
				if len(ts) == 0 {
					add(d.Pos(), recv+"."+name, fmt.Sprintf("func (%s) %s: the module has no type %s", recv, name, recv))
					continue
				}
				for _, t := range ts {
					if m := t.Methods[name]; m != nil {
						cands = append(cands, m)
					}
				}
				if len(cands) == 0 {
					add(d.Pos(), recv+"."+name, fmt.Sprintf("%s.%s: type %s has no method %s", recv, name, recv, name))
					continue
				}
			} else {
				cands = funcsNamed(r.Module, name)
				if len(cands) == 0 {
					add(d.Pos(), name, fmt.Sprintf("func %s: the module has no function or method %s", name, name))
					continue
				}
			}
			ok := false
			for _, c := range cands {
				if sameTypes(norm(params), norm(c.Params)) && sameTypes(norm(results), norm(c.Results)) {
					ok = true
					break
				}
			}
			if !ok {
				c := cands[0]
				subject := name
				if recv != "" {
					subject = recv + "." + name
				}
				add(d.Pos(), subject, fmt.Sprintf("%s: the document says %s%s, the code (%s:%d) says %s%s",
					subject, name, want, c.File, c.Line, name, goapi.Signature(norm(c.Params), norm(c.Results))))
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if it, ok := s.Type.(*ast.InterfaceType); ok && s.Name.Name == "_" {
						out = append(out, methodSpecs(r, g, it, norm)...)
						continue
					}
					if !s.Name.IsExported() || placeholderName(s.Name.Name) {
						continue
					}
					ts := ix.Types[s.Name.Name]
					if len(ts) == 0 {
						add(s.Pos(), s.Name.Name, "type "+s.Name.Name+": the module has no such type")
						continue
					}
					if st, ok := s.Type.(*ast.StructType); ok {
						for _, f := range st.Fields.List {
							for _, n := range f.Names {
								if n.IsExported() && !hasMember(ix, ts, n.Name) {
									add(n.Pos(), s.Name.Name+"."+n.Name, fmt.Sprintf("%s.%s: type %s has no field %s", s.Name.Name, n.Name, s.Name.Name, n.Name))
								}
							}
						}
					}
					if it, ok := s.Type.(*ast.InterfaceType); ok {
						for _, f := range it.Methods.List {
							for _, n := range f.Names {
								if n.IsExported() && !hasMember(ix, ts, n.Name) {
									add(n.Pos(), s.Name.Name+"."+n.Name, fmt.Sprintf("%s.%s: interface %s has no method %s", s.Name.Name, n.Name, s.Name.Name, n.Name))
								}
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() && !ix.Members[n.Name] {
							add(n.Pos(), n.Name, n.Name+": the module declares no such constant or variable")
						}
					}
				}
			}
		}
	}
	return out
}

var builtinTypes = map[string]bool{"bool": true, "byte": true, "complex64": true, "complex128": true, "error": true,
	"float32": true, "float64": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true, "rune": true,
	"string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true, "any": true}

// sameTypes compares the type lists of a document and of the code. A document
// that writes a parameter's name only (Close(ctx) error) matches any type.
func sameTypes(doc, code []string) bool {
	if len(doc) != len(code) {
		return false
	}
	for i := range doc {
		if doc[i] == code[i] || token.IsIdentifier(doc[i]) && !ast.IsExported(doc[i]) && !builtinTypes[doc[i]] {
			continue
		}
		return false
	}
	return true
}

func recvBase(e ast.Expr) string {
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

// methodSpecs checks a listing of method specifications against the methods
// of that name in the module.
func methodSpecs(r *Repo, g *goBlock, it *ast.InterfaceType, norm func([]string) []string) []finding.Finding {
	var out []finding.Finding
	for _, f := range it.Methods.List {
		ft, ok := f.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, n := range f.Names {
			if !n.IsExported() || placeholderName(n.Name) {
				continue
			}
			cands := funcsNamed(r.Module, n.Name)
			params, results := goapi.FieldTypes(ft)
			ok := false
			for _, c := range cands {
				if c.Recv != "" && sameTypes(norm(params), norm(c.Params)) && sameTypes(norm(results), norm(c.Results)) {
					ok = true
					break
				}
			}
			if ok {
				continue
			}
			msg := fmt.Sprintf("method %s: the module has no method %s", n.Name, n.Name)
			for _, c := range cands {
				if c.Recv != "" {
					msg = fmt.Sprintf("%s: the document says %s%s, the code (%s:%d) says %s%s", n.Name, n.Name,
						goapi.Signature(norm(params), norm(results)), c.File, c.Line, n.Name, goapi.Signature(norm(c.Params), norm(c.Results)))
					break
				}
			}
			out = append(out, finding.Finding{Rule: "DS-SIG", File: g.doc.Path, Line: g.line(n.Pos()), Subject: n.Name, Message: msg})
		}
	}
	return out
}

func funcsNamed(m *goapi.Module, name string) []*goapi.Func {
	var out []*goapi.Func
	for _, p := range m.Packages {
		if f := p.Funcs[name]; f != nil {
			out = append(out, f)
		}
	}
	for _, p := range m.Packages {
		for _, t := range p.Types {
			if f := t.Methods[name]; f != nil {
				out = append(out, f)
			}
		}
	}
	return out
}

func pkgQualifierRE(ix *goapi.Index) *regexp.Regexp {
	var names []string
	for n := range ix.PkgNames {
		names = append(names, regexp.QuoteMeta(n))
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	return regexp.MustCompile(`\b(?:` + strings.Join(names, "|") + `)\.`)
}
