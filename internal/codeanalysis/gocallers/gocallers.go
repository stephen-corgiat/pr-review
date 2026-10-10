// Package gocallers finds the exact callers of Go Touched Methods across
// the module that holds them, with go/packages and go/types.
//
// It sits beside the code-analysis interface rather than behind it: the
// Analyzer interface reads file pairs, and callers need the whole module
// at the head. Its input is the worktree (checked out at the head) and the
// head's Go Touched Methods; its output is, per Touched Method Key, the
// callers:
//
//   - every use of the method's *types.Func (the generic origin, for
//     instantiated functions and methods) that is the callee of a call
//   - every other use, a reference (a method or function value), marked Ref
//   - for a concrete method T.M, every call of I.M where T or *T
//     implements the interface I, marked Via "I" (interfaces from any
//     module and the standard library count)
//
// The Pull Request's code is never run. go/packages runs `go list` (and
// the compiler, for the dependencies' export data) with CGO_ENABLED=0, so
// no #cgo directive can run a C compiler; GOFLAGS=-mod=readonly, so go.mod
// is never rewritten (missing modules still download through the proxy);
// GOTOOLCHAIN=local, so a toolchain line can't download a Go toolchain;
// and GOPACKAGESDRIVER=off, so `go list` is the driver. See Env.
//
// Type errors in some packages don't stop the rest. A Touched Method whose
// own package didn't type-check is unavailable with the first error, and
// keeps whatever callers the other packages gave.
package gocallers

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Version is the analysis's version: a cached Result from another version
// is computed again.
const Version = 1

// Method is a Go Touched Method at the head.
type Method struct {
	Key   string // the Touched Method Key
	Path  string // repo-relative
	Name  string
	Owner string // the receiver, "T" or "*T"; "" for a function
	Line  int    // the head-side line of its "func"
}

// Caller is one place that calls (or refers to) a Touched Method.
type Caller struct {
	// Func is the enclosing function: "pkg.Func", "(T).Method" or
	// "(*T).Method", "func literal in pkg.Func", or "pkg (package level)".
	Func string `json:"func"`
	Path string `json:"path"` // repo-relative
	Line int    `json:"line"`
	Test bool   `json:"test,omitempty"` // in a _test.go file
	Ref  bool   `json:"ref,omitempty"`  // a reference, not a call
	// Via is the interface the call goes through ("io.Writer"), "" for
	// a direct call or reference.
	Via string `json:"via,omitempty"`
}

// Callers is what was found for one Touched Method.
type Callers struct {
	Callers []Caller `json:"callers,omitempty"`
	// Unavailable says why the callers couldn't be found (or, with
	// Callers, why they may be incomplete).
	Unavailable string `json:"unavailable,omitempty"`
}

// Counts is the number of callers, of those in tests, and of those
// through an interface.
func (c Callers) Counts() (total, tests, via int) {
	for _, x := range c.Callers {
		total++
		if x.Test {
			tests++
		}
		if x.Via != "" {
			via++
		}
	}
	return
}

// Result is the callers per Touched Method Key.
type Result map[string]Callers

// Options configures Analyze. The zero value is what the tool uses.
type Options struct {
	// Env is the environment `go list` starts from; nil is os.Environ().
	// Env's overrides always apply on top.
	Env []string
	// load is go/packages.Load (tests replace it).
	load func(*packages.Config, ...string) ([]*packages.Package, error)
}

// Analyze finds the callers of methods in the worktree at root.
func Analyze(ctx context.Context, root string, methods []Method) Result {
	return Options{}.Analyze(ctx, root, methods)
}

// Analyze finds the callers of methods in the worktree at root. Every
// method gets an entry; a method whose callers couldn't be found says why.
func (o Options) Analyze(ctx context.Context, root string, methods []Method) Result {
	out := Result{}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	// Group the methods by the module that holds them.
	byModule := map[string][]Method{}
	var modules []string
	for _, m := range methods {
		dir := moduleDir(root, filepath.Join(root, filepath.FromSlash(m.Path)))
		if dir == "" {
			out[m.Key] = Callers{Unavailable: "no go.mod holds " + m.Path}
			continue
		}
		if _, ok := byModule[dir]; !ok {
			modules = append(modules, dir)
		}
		byModule[dir] = append(byModule[dir], m)
	}
	sort.Strings(modules)
	for _, dir := range modules {
		if err := ctx.Err(); err != nil {
			return out
		}
		for k, c := range o.module(ctx, root, dir, byModule[dir]) {
			out[k] = c
		}
	}
	return out
}

// moduleDir is the directory of the nearest go.mod above file, within
// root ("" when there's none).
func moduleDir(root, file string) string {
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if dir == root || !within(root, dir) {
			return ""
		}
	}
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Env is the environment go/packages runs `go list` with for the module
// in dir of the worktree at root: base with the safety overrides on top
// (later entries win). A go.work at the root is used; any other isn't.
func Env(base []string, root, dir string) []string {
	env := append([]string(nil), base...)
	mod := "-mod=readonly"
	work := "off"
	if _, err := os.Stat(filepath.Join(root, "go.work")); err == nil {
		work = filepath.Join(root, "go.work")
	} else if _, err := os.Stat(filepath.Join(dir, "vendor", "modules.txt")); err == nil {
		mod = "-mod=vendor" // the module's own vendor directory: read-only too, and offline
	}
	return append(env,
		"CGO_ENABLED=0",
		"GOFLAGS="+mod,
		"GOTOOLCHAIN=local",
		"GOWORK="+work,
		"GOPACKAGESDRIVER=off",
	)
}

const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
	packages.NeedModule | packages.NeedForTest

// module finds the callers of methods, all in the module at dir.
func (o Options) module(ctx context.Context, root, dir string, methods []Method) Result {
	out := Result{}
	fail := func(reason string) Result {
		for _, m := range methods {
			out[m.Key] = Callers{Unavailable: reason}
		}
		return out
	}
	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	load := o.load
	if load == nil {
		load = packages.Load
	}
	cfg := &packages.Config{
		Context: ctx,
		Mode:    loadMode,
		Dir:     dir,
		Env:     Env(base, root, dir),
		Tests:   true,
		Fset:    token.NewFileSet(),
	}
	pkgs, err := load(cfg, "./...")
	if err != nil {
		if ctx.Err() != nil {
			return out
		}
		return fail(shortReason(root, err.Error()))
	}
	a := &analysis{root: root, fset: cfg.Fset, visible: map[visKey]*types.Package{}}
	for _, p := range pkgs {
		if p.TypesInfo != nil && p.Types != nil && a.inRoot(p) {
			a.pkgs = append(a.pkgs, p)
		}
	}
	if len(a.pkgs) == 0 {
		reason := "no packages loaded"
		for _, p := range pkgs {
			if len(p.Errors) > 0 {
				reason = p.Errors[0].Msg
				break
			}
		}
		return fail(shortReason(root, reason))
	}
	for _, m := range methods {
		t, why := a.target(m)
		if t == nil {
			out[m.Key] = Callers{Unavailable: why}
			continue
		}
		t.unavailable = why
		a.targets = append(a.targets, t)
	}
	a.scan(ctx)
	for _, t := range a.targets {
		cs := t.sorted()
		out[t.m.Key] = Callers{Callers: cs, Unavailable: t.unavailable}
	}
	return out
}

// shortReason is an error's first line, with the worktree's path and go
// list's wrapping dropped.
func shortReason(root, s string) string {
	if _, after, ok := strings.Cut(s, "stderr: "); ok {
		s = after
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "go: ")
	s = strings.ReplaceAll(s, root+string(filepath.Separator), "")
	s = strings.ReplaceAll(s, root, ".")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

type visKey struct {
	from *types.Package
	path string
}

type analysis struct {
	root    string
	fset    *token.FileSet
	pkgs    []*packages.Package // loaded from source, in the worktree
	targets []*target
	visible map[visKey]*types.Package
}

// inRoot: the package's files are in the worktree (not the generated
// test main in the build cache).
func (a *analysis) inRoot(p *packages.Package) bool {
	for _, f := range p.CompiledGoFiles {
		if strings.HasSuffix(f, ".go") && a.rel(f) != "" {
			return true
		}
	}
	return false
}

// rel is file's repo-relative slash path, or "" when it isn't in the
// worktree.
func (a *analysis) rel(file string) string {
	r, err := filepath.Rel(a.root, file)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(r)
}

// target is a Touched Method found in its package.
type target struct {
	m       Method
	pkgPath string // its package's path
	recv    string // the receiver's type name, "" for a function
	name    string
	generic bool // its receiver type (or the function) has type parameters
	home    *types.Func
	homeT   *types.Named // the receiver type in its home package

	objs        map[*types.Package]*types.Func // the method in each variant of its package
	found       map[string]Caller              // by position (and interface)
	unavailable string
}

// target finds m's declaration in the loaded packages. A nil target says
// why it couldn't be found; a target with a reason found it in a package
// that didn't type-check.
func (a *analysis) target(m Method) (*target, string) {
	abs := filepath.Join(a.root, filepath.FromSlash(m.Path))
	isTest := strings.HasSuffix(m.Path, "_test.go")
	var home *packages.Package
	var file *ast.File
	for _, p := range a.pkgs {
		for i, f := range p.CompiledGoFiles {
			if f != abs || i >= len(p.Syntax) {
				continue
			}
			// A non-test file is in the package and its test variant: take
			// the package. A _test.go file is only in a test variant.
			if home == nil || (!isTest && p.ForTest == "" && home.ForTest != "") {
				home, file = p, p.Syntax[i]
			}
		}
	}
	if home == nil {
		for _, p := range a.pkgs {
			for _, f := range p.IgnoredFiles {
				if f == abs {
					return nil, "its file isn't in the build (build constraints or cgo)"
				}
			}
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, m.Path + " isn't in the worktree"
		}
		return nil, "its package didn't load"
	}
	recv := strings.TrimPrefix(m.Owner, "*")
	var decl *ast.FuncDecl
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != m.Name || recvName(fd) != recv {
			continue
		}
		if decl == nil || a.fset.Position(fd.Pos()).Line == m.Line {
			decl = fd
		}
	}
	if decl == nil {
		return nil, "its declaration wasn't found in " + m.Path
	}
	fn, _ := home.TypesInfo.Defs[decl.Name].(*types.Func)
	if fn == nil {
		return nil, "it has no type information"
	}
	t := &target{m: m, pkgPath: home.Types.Path(), recv: recv, name: m.Name, home: fn,
		objs: map[*types.Package]*types.Func{home.Types: fn}, found: map[string]Caller{}}
	sig := fn.Type().(*types.Signature)
	if r := sig.Recv(); r != nil {
		t.homeT = namedOf(r.Type())
		t.generic = t.homeT != nil && t.homeT.TypeParams().Len() > 0
	} else {
		t.generic = sig.TypeParams().Len() > 0
	}
	return t, a.firstError(home)
}

// firstError is a package's first type error (else its first error of any
// kind), short: "" when it type-checked.
func (a *analysis) firstError(p *packages.Package) string {
	if len(p.TypeErrors) > 0 {
		e := p.TypeErrors[0]
		pos := e.Fset.PositionFor(e.Pos, false)
		return fmt.Sprintf("%s:%d: %s", a.rel(pos.Filename), pos.Line, e.Msg)
	}
	if len(p.Errors) > 0 {
		return shortReason(a.root, p.Errors[0].Error())
	}
	return ""
}

func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	x := fd.Recv.List[0].Type
	for {
		switch e := x.(type) {
		case *ast.StarExpr:
			x = e.X
		case *ast.ParenExpr:
			x = e.X
		case *ast.IndexExpr:
			x = e.X
		case *ast.IndexListExpr:
			x = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

func namedOf(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := types.Unalias(t).(*types.Named)
	return n
}

// in is the method as package `from` sees it: test variants of a package
// are type-checked again, with their own objects. nil when from can't
// see the method's package.
func (a *analysis) in(t *target, from *types.Package) *types.Func {
	if fn, ok := t.objs[from]; ok {
		return fn
	}
	var fn *types.Func
	if v := a.variant(from, t.pkgPath); v != nil {
		fn = lookup(v, t.recv, t.name)
	}
	t.objs[from] = fn
	return fn
}

// variant is the package with path that from sees (itself, or one it
// imports, directly or not).
func (a *analysis) variant(from *types.Package, path string) *types.Package {
	k := visKey{from, path}
	if v, ok := a.visible[k]; ok {
		return v
	}
	a.visible[k] = nil // cycles (there are none in Go, but be safe)
	var v *types.Package
	if from.Path() == path {
		v = from
	} else {
		for _, imp := range from.Imports() {
			if v = a.variant(imp, path); v != nil {
				break
			}
		}
	}
	a.visible[k] = v
	return v
}

func lookup(p *types.Package, recv, name string) *types.Func {
	if recv == "" {
		fn, _ := p.Scope().Lookup(name).(*types.Func)
		return fn
	}
	tn, _ := p.Scope().Lookup(recv).(*types.TypeName)
	if tn == nil {
		return nil
	}
	n := namedOf(tn.Type())
	if n == nil {
		return nil
	}
	for i := 0; i < n.NumMethods(); i++ {
		if m := n.Method(i); m.Name() == name {
			return m
		}
	}
	return nil
}

// recvType is the method's receiver type as package `from` sees it (or
// in its home package, when from can't see it).
func (a *analysis) recvType(t *target, from *types.Package) *types.Named {
	if fn := a.in(t, from); fn != nil {
		if r := fn.Type().(*types.Signature).Recv(); r != nil {
			return namedOf(r.Type())
		}
	}
	return t.homeT
}

// scan walks every package's files once, finding uses of the targets.
func (a *analysis) scan(ctx context.Context) {
	byName := map[string][]*target{}
	for _, t := range a.targets {
		byName[t.name] = append(byName[t.name], t)
	}
	if len(byName) == 0 {
		return
	}
	for _, p := range a.pkgs {
		if ctx.Err() != nil {
			return
		}
		for _, f := range p.Syntax {
			path := a.rel(a.fset.PositionFor(f.Pos(), false).Filename)
			if path == "" {
				continue
			}
			a.scanFile(p, f, path, byName)
		}
	}
}

// scanFile finds the targets' uses in one file, tracking the enclosing
// function and which identifiers are the callee of a call.
func (a *analysis) scanFile(p *packages.Package, f *ast.File, path string, byName map[string][]*target) {
	info := p.TypesInfo
	isTest := strings.HasSuffix(path, "_test.go")
	callee := map[*ast.Ident]bool{}
	sels := map[*ast.Ident]*ast.SelectorExpr{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.CallExpr:
			if id := calleeIdent(n.Fun); id != nil {
				callee[id] = true
			}
		case *ast.SelectorExpr:
			sels[n.Sel] = n
		case *ast.Ident:
			fn, _ := info.Uses[n].(*types.Func)
			if fn == nil || byName[fn.Name()] == nil {
				return true
			}
			fn = fn.Origin()
			pos := a.fset.PositionFor(n.Pos(), false)
			c := Caller{Path: path, Line: pos.Line, Test: isTest, Ref: !callee[n]}
			for _, t := range byName[fn.Name()] {
				if obj := a.in(t, p.Types); obj != nil && fn == obj {
					c.Func = enclosing(p.Types.Name(), stack)
					t.add(pos, c)
					continue
				}
				if via := a.through(t, p, fn, sels[n]); via != "" {
					c := c
					c.Func, c.Via = enclosing(p.Types.Name(), stack), via
					t.add(pos, c)
				}
			}
		}
		return true
	})
}

// calleeIdent is the identifier naming a call's function: f, x.f, f[T],
// x.f[T, U], (f).
func calleeIdent(x ast.Expr) *ast.Ident {
	for {
		switch e := x.(type) {
		case *ast.ParenExpr:
			x = e.X
		case *ast.IndexExpr:
			x = e.X
		case *ast.IndexListExpr:
			x = e.X
		case *ast.SelectorExpr:
			return e.Sel
		case *ast.Ident:
			return e
		default:
			return nil
		}
	}
}

// through reports the interface a use of fn (an interface method) goes
// through, when t's receiver type (or a pointer to it) implements it: ""
// otherwise.
func (a *analysis) through(t *target, p *packages.Package, fn *types.Func, sel *ast.SelectorExpr) string {
	if t.recv == "" || t.generic {
		return "" // a function, or a generic type: no interface dispatch to check
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil || !types.IsInterface(sig.Recv().Type()) {
		return ""
	}
	// The interface as the call site names it (io.ReadWriter, not the
	// io.Writer that declares Write), else the one declaring the method.
	iface := sig.Recv().Type()
	if sel != nil {
		if s := p.TypesInfo.Selections[sel]; s != nil {
			r := s.Recv()
			if tp, ok := r.(*types.TypeParam); ok {
				r = tp.Constraint()
			}
			if types.IsInterface(r) {
				iface = r
			}
		}
	}
	it, _ := iface.Underlying().(*types.Interface)
	T := a.recvType(t, p.Types)
	if it == nil || T == nil {
		return ""
	}
	if !types.Implements(T, it) && !types.Implements(types.NewPointer(T), it) {
		return ""
	}
	return types.TypeString(iface, func(p *types.Package) string { return p.Name() })
}

func (t *target) add(pos token.Position, c Caller) {
	k := fmt.Sprintf("%s:%d:%d:%s", c.Path, pos.Line, pos.Column, c.Via)
	if _, ok := t.found[k]; !ok {
		t.found[k] = c
	}
}

func (t *target) sorted() []Caller {
	out := make([]Caller, 0, len(t.found))
	keys := make([]string, 0, len(t.found))
	for k := range t.found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, t.found[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Via != y.Via {
			return x.Via < y.Via // direct first, then by interface
		}
		if x.Test != y.Test {
			return !x.Test
		}
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.Line < y.Line
	})
	return out
}

// enclosing names the function a use sits in, from the AST path to it.
func enclosing(pkg string, stack []ast.Node) string {
	lit := false
	for i := len(stack) - 1; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.FuncLit:
			lit = true
		case *ast.FuncDecl:
			name := funcName(pkg, n)
			if lit {
				return "func literal in " + name
			}
			return name
		}
	}
	if lit {
		return "func literal in " + pkg + " (package level)"
	}
	return pkg + " (package level)"
}

func funcName(pkg string, fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return pkg + "." + fd.Name.Name
	}
	star := ""
	x := fd.Recv.List[0].Type
	if p, ok := x.(*ast.ParenExpr); ok {
		x = p.X
	}
	if _, ok := x.(*ast.StarExpr); ok {
		star = "*"
	}
	return "(" + star + recvName(fd) + ")." + fd.Name.Name
}
