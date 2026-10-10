package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// Tree-sitter pass: TypeScript, TSX and Ruby files get method blocks, J/K
// and ]/[ stops, from the code analysis's sample files.

const samples = "../codeanalysis/treesitter/testdata/"

// newSampleRepo opens -repo mode on a repo whose base and head hold the
// code analysis's sample files.
func newSampleRepo(t *testing.T) *Model {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "samples")
	files := map[string]string{"web/calc.ts": "calc.%s.ts", "web/panel.tsx": "panel.%s.tsx", "lib/invoice.rb": "invoice.%s.rb"}
	put := func(side string) {
		for dst, src := range files {
			b, err := os.ReadFile(samples + strings.Replace(src, "%s", side, 1))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(dst)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, dst), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	testrepo.Git(t, dir, "init", "-q", "-b", "main")
	put("base")
	testrepo.Git(t, dir, "add", "-A")
	testrepo.Git(t, dir, "commit", "-q", "-m", "base")
	put("head")
	testrepo.Git(t, dir, "commit", "-q", "-am", "head")
	eng, _, err := app.Repo(dir, "HEAD^", "HEAD", nil)
	if err != nil {
		t.Fatalf("open (-repo mode): %v", err)
	}
	t.Cleanup(eng.Close)
	return New(eng, true)
}

// walk presses key until the cursor leaves path or stops moving, and
// returns each stop: a method key's part after "::", or "L<line>".
func walk(t *testing.T, m *Model, path, key string) []string {
	t.Helper()
	var stops []string
	last := -1
	for i := 0; i < 40; i++ {
		run(t, m, 140, 45, key, 20*time.Millisecond)
		if m.filePath != path || m.centreCursor == last {
			break
		}
		last = m.centreCursor
		l, meth := stopHere(m)
		if meth != "" {
			stops = append(stops, meth[strings.Index(meth, "::")+2:])
		} else {
			stops = append(stops, "L"+strconv.Itoa(l))
		}
	}
	return stops
}

// openAtTop opens path with the centre pane focused, cursor at the top.
func openAtTop(t *testing.T, m *Model, path string, w, h int) string {
	t.Helper()
	m.openFile(path)
	m.focus = FocusCentre
	m.dirty = true
	return run(t, m, w, h, "g", 20*time.Millisecond)
}

func sameStops(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%s:\n got  %v\n want %v", what, got, want)
	}
}

// 55. TypeScript and TSX files get method blocks with their call graph;
// J/K walk the Touched Methods, ]/[ stop on changes and added declarations.
func TestScenario55_TypeScriptMethods(t *testing.T) {
	m := newSampleRepo(t)
	run(t, m, 140, 45, "", 50*time.Millisecond)
	for _, p := range []string{"web/calc.ts", "web/panel.tsx"} {
		if m.sess.Files[p].ParseErr != "" {
			t.Errorf("%s: parse note %q", p, m.sess.Files[p].ParseErr)
		}
	}

	openAtTop(t, m, "web/calc.ts", 140, 45)
	sameStops(t, "J in calc.ts", walk(t, m, "web/calc.ts", "J"),
		[]string{"add", "double", "(Calc).constructor", "(Calc).step", "(Calc).reset", "(Calc).onClick", "logged"})
	sameStops(t, "K back up calc.ts", walk(t, m, "web/calc.ts", "K"),
		[]string{"(Calc).onClick", "(Calc).reset", "(Calc).step", "(Calc).constructor", "double", "add"})

	m.jumpToMethod("web/calc.ts::(Calc).step")
	out := run(t, m, 140, 45, "", 20*time.Millisecond)
	mustContain(t, out, "── Calc.step", "step's header rule")
	mustContain(t, out, "↑ callers  onClick", "step's callers")
	mustContain(t, out, "↓ calls    add · double", "step's calls")

	// ] lands on a method block when an added line starts a declaration
	// (double, the constructor, onClick); the rest are runs of changes.
	openAtTop(t, m, "web/calc.ts", 140, 45)
	stops := walk(t, m, "web/calc.ts", "]")
	for _, want := range []string{"double", "(Calc).constructor", "(Calc).onClick", "L36"} {
		if !contains(stops, want) {
			t.Errorf("] in calc.ts: no stop at %s, got %v", want, stops)
		}
	}

	// TSX: the nested handler belongs to Panel; JSX isn't a call (a
	// known gap), so Panel has no edge to Button.
	openAtTop(t, m, "web/panel.tsx", 140, 45)
	sameStops(t, "J in panel.tsx", walk(t, m, "web/panel.tsx", "J"), []string{"Button", "Panel", "track"})
	m.jumpToMethod("web/panel.tsx::Panel")
	out = run(t, m, 140, 45, "", 20*time.Millisecond)
	mustContain(t, out, "── Panel", "Panel's header rule")
	mustContain(t, out, "↓ calls    track", "Panel calls track from its onClick handler")
	mustNotContain(t, out, "Button ·", "JSX isn't a call")
}

// 56. Ruby files too: class methods show as Owner.name, and a class's
// first method starts at its def (the known gap).
func TestScenario56_RubyMethods(t *testing.T) {
	m := newSampleRepo(t)
	openAtTop(t, m, "lib/invoice.rb", 140, 45)
	sameStops(t, "J in invoice.rb", walk(t, m, "lib/invoice.rb", "J"),
		[]string{"(Invoice).total", "(Invoice).price", "(Invoice).tax", "(Invoice).self.build", "(Invoice).self.empty", "(Invoice).helper"})

	m.jumpToMethod("lib/invoice.rb::(Invoice).self.build")
	out := run(t, m, 140, 45, "", 20*time.Millisecond)
	mustContain(t, out, "── Invoice.build", "build's header rule")
	mustContain(t, out, "↑ callers  empty", "build's callers")
	mustContain(t, out, "↓ calls    total", "build's calls")

	openAtTop(t, m, "lib/invoice.rb", 140, 45)
	stops := walk(t, m, "lib/invoice.rb", "]")
	for _, want := range []string{"L4", "(Invoice).tax", "L28", "(Invoice).self.empty"} {
		if !contains(stops, want) {
			t.Errorf("] in invoice.rb: no stop at %s, got %v", want, stops)
		}
	}
	back := walk(t, m, "lib/invoice.rb", "[")
	if len(back) == 0 || back[len(back)-1] != stops[0] {
		t.Errorf("[ should walk back to the first stop %s, got %v", stops[0], back)
	}
}

func TestTreeSitter_FrameFit(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		m := newSampleRepo(t)
		for _, p := range []string{"web/calc.ts", "web/panel.tsx", "lib/invoice.rb"} {
			openAtTop(t, m, p, sz.w, sz.h)
			run(t, m, sz.w, sz.h, "J J ] ]", 20*time.Millisecond)
		}
		m.centreView = ViewBriefing
		m.dirty = true
		run(t, m, sz.w, sz.h, "ctrl+d ctrl+d", 20*time.Millisecond)
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
