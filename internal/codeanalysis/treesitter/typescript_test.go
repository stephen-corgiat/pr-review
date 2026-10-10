package treesitter

import (
	"os"
	"reflect"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/catest"
)

// analyze runs the Analyzer on testdata/<name>.base<ext> and .head<ext>,
// as the file <name><ext>.
func analyze(t *testing.T, name, ext string) *codeanalysis.Result {
	t.Helper()
	base, err := os.ReadFile("testdata/" + name + ".base" + ext)
	if err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile("testdata/" + name + ".head" + ext)
	if err != nil {
		t.Fatal(err)
	}
	r := codeanalysis.Run([]codeanalysis.FilePair{catest.Pair(name+ext, string(base), string(head))}, New())
	if len(r.ParseNotes) > 0 {
		t.Errorf("parse notes: %+v", r.ParseNotes)
	}
	return r
}

func TestTypeScript(t *testing.T) {
	r := analyze(t, "calc", ".ts")

	// Ranges include leading comments and decorators, and an exported
	// const arrow function is widened to its export statement.
	catest.Methods(t, r, []string{
		"calc.ts::add changed new 3-8 decl 5 old 3-6 decl 4",
		"calc.ts::double added new 14-15 decl 15",
		"calc.ts::(Calc).constructor changed new 18-20 decl 18 old 13-13 decl 13",
		"calc.ts::(Calc).step changed new 22-25 decl 23 old 15-18 decl 16",
		"calc.ts::(Calc).reset changed new 27-30 decl 28 old 20-23 decl 21",
		"calc.ts::(Calc).onClick added new 32-32 decl 32",
		"calc.ts::logged changed new 45-47 decl 45 old 35-37 decl 35",
		// Known gaps, expected:
		//  - the added overload signature (line 36) touches no method:
		//    signatures aren't Touched Methods, and `over`'s
		//    implementation (37-39) didn't change
		//  - the anonymous default export changed (line 42) but isn't found
	})
	catest.Edges(t, r, []string{
		"calc.ts::double -> calc.ts::add",
		"calc.ts::(Calc).step -> calc.ts::add",
		"calc.ts::(Calc).step -> calc.ts::double",
		"calc.ts::(Calc).constructor -> calc.ts::(Calc).reset", // this.reset()
		"calc.ts::(Calc).onClick -> calc.ts::(Calc).step",
		// Known gap, expected: the decorator @logged() is a call made by
		// the method under it.
		"calc.ts::(Calc).reset -> calc.ts::logged",
	})
	// Top-level declarations and class members, from their leading
	// comments; not the import. 35 and 36 are the overload signatures.
	if got, want := r.DeclLines["calc.ts"], []int{3, 10, 14, 17, 18, 22, 27, 32, 35, 36, 37, 41, 45}; !reflect.DeepEqual(got, want) {
		t.Errorf("decl stops = %v, want %v", got, want)
	}
	if len(r.ExportedChanges) != 0 {
		t.Errorf("Contract Changes are Go-only, got %+v", r.ExportedChanges)
	}
}

func TestTSX(t *testing.T) {
	r := analyze(t, "panel", ".tsx")
	catest.Methods(t, r, []string{
		"panel.tsx::Button changed new 3-6 decl 4 old 3-6 decl 4",
		"panel.tsx::Panel changed new 8-15 decl 8 old 8-15 decl 8",
		"panel.tsx::track changed new 19-21 decl 19 old 19-21 decl 19",
		// Known gap, expected: Memo (React.memo(() => …), line 17)
		// changed but isn't found.
	})
	catest.Edges(t, r, []string{
		// The nested onClick handler is part of Panel, not a Touched
		// Method of its own.
		"panel.tsx::Panel -> panel.tsx::track",
		// Known gap, expected: no Panel -> Button; <Button /> is JSX,
		// not a call.
	})
	if got, want := r.DeclLines["panel.tsx"], []int{3, 8, 17, 19}; !reflect.DeepEqual(got, want) {
		t.Errorf("decl stops = %v, want %v", got, want)
	}
}

// Edges join TypeScript and TSX files; a bare call prefers its own file.
func TestTypeScriptEdgesAcrossFiles(t *testing.T) {
	files := []codeanalysis.FilePair{
		catest.Pair("util.ts",
			"export function fmt(n: number) {\n  return String(n);\n}\n",
			"export function fmt(n: number) {\n  return n.toFixed(2);\n}\n"),
		catest.Pair("view.tsx",
			"import { fmt } from './util';\nexport function View({ n }: { n: number }) {\n  return <b>{n}</b>;\n}\n",
			"import { fmt } from './util';\nexport function View({ n }: { n: number }) {\n  return <b>{fmt(n)}</b>;\n}\n"),
		catest.Pair("other.ts",
			"function fmt(s: string) {\n  return s;\n}\nexport function shout(s: string) {\n  return s;\n}\n",
			"function fmt(s: string) {\n  return s + '!';\n}\nexport function shout(s: string) {\n  return fmt(s);\n}\n"),
	}
	r := codeanalysis.Run(files, New())
	catest.Edges(t, r, []string{
		"view.tsx::View -> other.ts::fmt (ambiguous)",
		"view.tsx::View -> util.ts::fmt (ambiguous)",
		"other.ts::shout -> other.ts::fmt",
	})
}

func TestReadsTypeScriptByExtension(t *testing.T) {
	a := New()
	for p, want := range map[string]bool{
		"a.ts": true, "a.mts": true, "a.cts": true, "a.tsx": true, "A.TSX": true,
		"a.go": false, "a.js": false, "a.jsx": false, "a.d": false,
	} {
		if a.Reads(p) != want {
			t.Errorf("Reads(%q) = %v, want %v", p, !want, want)
		}
	}
}
