package gocallers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The acceptance test's module (spec seam 3): a type whose Write is called
// directly from another package, from a _test.go file, passed as a method
// value, through a module interface and through io.Writer, and from a
// closure; a generic function and a generic type's method, each used with
// two type arguments; a same-named method on another type that mustn't
// count; and a package with a type error that doesn't stop the others.
//
// A "// want:<id>" comment marks the line each expected caller is on.
var acceptance = map[string]string{
	"go.mod": "module example.com/accept\n\ngo 1.22\n",

	"store/buffer.go": `package store

// Sink takes bytes: a module interface *Buffer satisfies.
type Sink interface {
	Write(p []byte) (int, error)
}

// Buffer keeps what's written.
type Buffer struct{ data []byte }

// Write is the Touched Method.
func (b *Buffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// Flush calls Write on itself.
func (b *Buffer) Flush() {
	b.Write(nil) // want:flush
}

// Discard has a same-named method that mustn't count.
type Discard struct{}

func (Discard) Write(p []byte) (int, error) { return len(p), nil }

// Map is a generic Touched Method.
func Map[T, U any](xs []T, f func(T) U) []U {
	var out []U
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

// Stack is a generic type.
type Stack[T any] struct{ items []T }

// Push is a Touched Method on a generic type.
func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }
`,

	"store/buffer_test.go": `package store

import "testing"

func TestWrite(t *testing.T) {
	var b Buffer
	b.Write([]byte("x")) // want:test
}
`,

	"store/ext_test.go": `package store_test

import (
	"testing"

	"example.com/accept/store"
)

func TestMap(t *testing.T) {
	_ = store.Map([]int{1}, func(i int) bool { return i > 0 }) // want:maptest
}
`,

	"use/use.go": `package use

import (
	"io"
	"strconv"

	"example.com/accept/store"
)

// names is set at package level.
var names = store.Map([]int{1, 2}, strconv.Itoa) // want:pkglevel

func Direct(b *store.Buffer) {
	b.Write([]byte("direct")) // want:direct
}

func Each(f func([]byte) (int, error)) { _, _ = f(nil) }

func Value(b *store.Buffer) {
	Each(b.Write) // want:value
}

func ToSink(s store.Sink) {
	s.Write(nil) // want:sink
}

func ToWriter(w io.Writer) {
	w.Write(nil) // want:writer
}

func Closure(b *store.Buffer) func() {
	return func() {
		b.Write(nil) // want:closure
	}
}

func Other(d store.Discard) {
	d.Write(nil) // a Discard: not a caller of (*Buffer).Write
}

func Generic() {
	_ = store.Map([]string{"a"}, func(s string) int { return len(s) }) // want:map1
	_ = store.Map[int, string]([]int{1}, strconv.Itoa)                  // want:map2
	var a store.Stack[int]
	a.Push(1) // want:push1
	var b store.Stack[string]
	push := b.Push // want:push2
	push("x")
}
`,

	"broken/broken.go": `package broken

import "example.com/accept/store"

// Helper is a Touched Method in a package that doesn't type-check.
func Helper() int {
	return 1
}

func Bad() {
	var n int = "not an int"
	_ = n
}

func Calls(b *store.Buffer) int {
	b.Write(nil) // want:broken
	return Helper() // want:helper
}
`,

	"other/other.go": `package other

import "example.com/accept/broken"

func UsesBroken() int {
	return broken.Helper() // want:helper2
}
`,
}

// writeModule writes files under a temp dir and returns it, with the line
// of each "// want:<id>" marker.
func writeModule(t *testing.T, files map[string]string) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	want := map[string]string{}
	re := regexp.MustCompile(`// want:(\w+)`)
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(src, "\n") {
			if m := re.FindStringSubmatch(l); m != nil {
				want[m[1]] = fmt.Sprintf("%s:%d", name, i+1)
			}
		}
	}
	return dir, want
}

// describe is one line per caller: "Func path:line marks".
func describe(cs []Caller) []string {
	var out []string
	for _, c := range cs {
		s := c.Func + " " + fmt.Sprintf("%s:%d", c.Path, c.Line)
		if c.Test {
			s += " test"
		}
		if c.Ref {
			s += " ref"
		}
		if c.Via != "" {
			s += " via " + c.Via
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func lineOf(t *testing.T, files map[string]string, path, prefix string) int {
	t.Helper()
	for i, l := range strings.Split(files[path], "\n") {
		if strings.HasPrefix(l, prefix) {
			return i + 1
		}
	}
	t.Fatalf("no %q in %s", prefix, path)
	return 0
}

func TestAcceptance(t *testing.T) {
	dir, at := writeModule(t, acceptance)
	ms := []Method{
		{Key: "store/buffer.go::(*Buffer).Write", Path: "store/buffer.go", Name: "Write", Owner: "*Buffer",
			Line: lineOf(t, acceptance, "store/buffer.go", "func (b *Buffer) Write")},
		{Key: "store/buffer.go::Map", Path: "store/buffer.go", Name: "Map",
			Line: lineOf(t, acceptance, "store/buffer.go", "func Map")},
		{Key: "store/buffer.go::(*Stack).Push", Path: "store/buffer.go", Name: "Push", Owner: "*Stack",
			Line: lineOf(t, acceptance, "store/buffer.go", "func (s *Stack[T]) Push")},
		{Key: "broken/broken.go::Helper", Path: "broken/broken.go", Name: "Helper",
			Line: lineOf(t, acceptance, "broken/broken.go", "func Helper")},
	}
	res := Analyze(context.Background(), dir, ms)

	check := func(key string, want []string) {
		t.Helper()
		sort.Strings(want)
		if got := describe(res[key].Callers); !reflect.DeepEqual(got, want) {
			t.Errorf("%s callers:\n got  %q\n want %q", key, got, want)
		}
	}
	check("store/buffer.go::(*Buffer).Write", []string{
		"(*Buffer).Flush " + at["flush"],
		"store.TestWrite " + at["test"] + " test",
		"use.Direct " + at["direct"],
		"use.Value " + at["value"] + " ref",
		"use.ToSink " + at["sink"] + " via store.Sink",
		"use.ToWriter " + at["writer"] + " via io.Writer",
		"func literal in use.Closure " + at["closure"],
		"broken.Calls " + at["broken"],
	})
	check("store/buffer.go::Map", []string{
		"use (package level) " + at["pkglevel"],
		"use.Generic " + at["map1"],
		"use.Generic " + at["map2"],
		"store_test.TestMap " + at["maptest"] + " test",
	})
	check("store/buffer.go::(*Stack).Push", []string{
		"use.Generic " + at["push1"],
		"use.Generic " + at["push2"] + " ref",
	})
	check("broken/broken.go::Helper", []string{
		"broken.Calls " + at["helper"],
		"other.UsesBroken " + at["helper2"],
	})

	for _, k := range []string{"store/buffer.go::(*Buffer).Write", "store/buffer.go::Map", "store/buffer.go::(*Stack).Push"} {
		if u := res[k].Unavailable; u != "" {
			t.Errorf("%s: unavailable %q: the type error in broken mustn't stop the others", k, u)
		}
	}
	// The method in the package that doesn't type-check is unavailable,
	// with the first error, and keeps the callers found.
	if u := res["broken/broken.go::Helper"].Unavailable; !strings.Contains(u, "broken/broken.go:11") || !strings.Contains(u, "cannot use") {
		t.Errorf("broken.Helper: unavailable %q, want the type error at broken/broken.go:11", u)
	}
	n, tests, via := res["store/buffer.go::(*Buffer).Write"].Counts()
	if n != 8 || tests != 1 || via != 2 {
		t.Errorf("Write counts: %d callers, %d in tests, %d via interface; want 8, 1, 2", n, tests, via)
	}
}

// The environment go/packages gets: cgo off, go.mod read-only, the local
// toolchain only, no external driver, and no go.work but the repo's.
func TestEnvironment(t *testing.T) {
	dir, _ := writeModule(t, map[string]string{
		"go.mod": "module example.com/env\n\ngo 1.22\n",
		"a/a.go": "package a\n\nfunc F() {}\n",
	})
	var got []string
	o := Options{
		Env: []string{"GOFLAGS=-mod=mod", "CGO_ENABLED=1", "GOTOOLCHAIN=auto", "GOWORK=/elsewhere/go.work", "GOPACKAGESDRIVER=/evil"},
		load: func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
			got = cfg.Env
			if cfg.Dir != dir || !cfg.Tests || !reflect.DeepEqual(patterns, []string{"./..."}) {
				t.Errorf("Load(dir %s, tests %v, %q)", cfg.Dir, cfg.Tests, patterns)
			}
			return nil, fmt.Errorf("stop")
		},
	}
	res := o.Analyze(context.Background(), dir, []Method{{Key: "a/a.go::F", Path: "a/a.go", Name: "F", Line: 3}})
	if res["a/a.go::F"].Unavailable != "stop" {
		t.Errorf("unavailable %q, want the load error", res["a/a.go::F"].Unavailable)
	}
	want := map[string]string{"CGO_ENABLED": "0", "GOFLAGS": "-mod=readonly", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOPACKAGESDRIVER": "off"}
	if eff := effective(got); !subset(want, eff) {
		t.Errorf("env %v, want %v", eff, want)
	}

	// A go.work at the repo root is used.
	os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.22\n\nuse .\n"), 0o644)
	o.Analyze(context.Background(), dir, []Method{{Key: "a/a.go::F", Path: "a/a.go", Name: "F", Line: 3}})
	if w := effective(got)["GOWORK"]; w != filepath.Join(dir, "go.work") {
		t.Errorf("GOWORK %q, want the root's go.work", w)
	}
}

// effective is the environment as exec sees it: the last value wins.
func effective(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

func subset(want, got map[string]string) bool {
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// A toolchain the module needs that isn't here makes the callers
// unavailable, with the reason (GOTOOLCHAIN=local: nothing downloads).
func TestNewerGoIsUnavailable(t *testing.T) {
	dir, _ := writeModule(t, map[string]string{
		"go.mod": "module example.com/newer\n\ngo 1.999\n\ntoolchain go1.999.0\n",
		"a/a.go": "package a\n\nfunc F() {}\n",
	})
	res := Analyze(context.Background(), dir, []Method{{Key: "a/a.go::F", Path: "a/a.go", Name: "F", Line: 3}})
	u := res["a/a.go::F"].Unavailable
	if !strings.HasPrefix(u, "go.mod requires go >= 1.999 (running go ") || !strings.HasSuffix(u, "; GOTOOLCHAIN=local)") {
		t.Errorf("unavailable %q, want the go version it needs", u)
	}
	if len(res["a/a.go::F"].Callers) > 0 {
		t.Errorf("callers %v from a module that didn't load", res["a/a.go::F"].Callers)
	}
}

// A file that's only built with cgo is left out (CGO_ENABLED=0): its
// methods are unavailable, and its #cgo directives never run.
func TestCgoFileIsLeftOut(t *testing.T) {
	dir, _ := writeModule(t, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.22\n",
		"a/c.go": "package a\n\n// #cgo CFLAGS: -DX=$(touch pwned)\nimport \"C\"\n\nfunc WithC() {}\n",
		"a/a.go": "package a\n\nfunc F() { G() }\n\nfunc G() {}\n",
	})
	res := Analyze(context.Background(), dir, []Method{
		{Key: "a/c.go::WithC", Path: "a/c.go", Name: "WithC", Line: 6},
		{Key: "a/a.go::G", Path: "a/a.go", Name: "G", Line: 5},
	})
	if u := res["a/c.go::WithC"].Unavailable; !strings.Contains(u, "isn't in the build") {
		t.Errorf("WithC: unavailable %q, want left out of the build", u)
	}
	if got := describe(res["a/a.go::G"].Callers); !reflect.DeepEqual(got, []string{"a.F a/a.go:3"}) {
		t.Errorf("G callers %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "pwned")); err == nil {
		t.Error("a #cgo directive ran")
	}
}

// Every module that holds a Touched Method is loaded; a file outside any
// module is unavailable.
func TestSeveralModules(t *testing.T) {
	dir, _ := writeModule(t, map[string]string{
		"one/go.mod":  "module example.com/one\n\ngo 1.22\n",
		"one/a/a.go":  "package a\n\nfunc F() {}\n\nfunc G() { F() }\n",
		"two/go.mod":  "module example.com/two\n\ngo 1.22\n",
		"two/b/b.go":  "package b\n\nfunc H() {}\n",
		"two/b/b2.go": "package b\n\nfunc init() { H() }\n",
		"loose/x.go":  "package loose\n\nfunc X() {}\n",
	})
	res := Analyze(context.Background(), dir, []Method{
		{Key: "one/a/a.go::F", Path: "one/a/a.go", Name: "F", Line: 3},
		{Key: "two/b/b.go::H", Path: "two/b/b.go", Name: "H", Line: 3},
		{Key: "loose/x.go::X", Path: "loose/x.go", Name: "X", Line: 3},
	})
	if got := describe(res["one/a/a.go::F"].Callers); !reflect.DeepEqual(got, []string{"a.G one/a/a.go:5"}) {
		t.Errorf("F callers %q", got)
	}
	if got := describe(res["two/b/b.go::H"].Callers); !reflect.DeepEqual(got, []string{"b.init two/b/b2.go:3"}) {
		t.Errorf("H callers %q", got)
	}
	if u := res["loose/x.go::X"].Unavailable; !strings.Contains(u, "no go.mod") {
		t.Errorf("X: unavailable %q", u)
	}
}

// A method with no callers has an empty, available result.
func TestNoCallers(t *testing.T) {
	dir, _ := writeModule(t, map[string]string{
		"go.mod": "module example.com/none\n\ngo 1.22\n",
		"a/a.go": "package a\n\ntype T struct{}\n\nfunc (T) Lonely() {}\n",
	})
	c := Analyze(context.Background(), dir, []Method{{Key: "a/a.go::(T).Lonely", Path: "a/a.go", Name: "Lonely", Owner: "T", Line: 5}})["a/a.go::(T).Lonely"]
	if len(c.Callers) != 0 || c.Unavailable != "" {
		t.Errorf("got %+v, want none", c)
	}
}
