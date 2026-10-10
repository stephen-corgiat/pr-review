package treesitter

import (
	"reflect"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/catest"
)

func TestRuby(t *testing.T) {
	r := analyze(t, "invoice", ".rb")

	catest.Methods(t, r, []string{
		// Known gap, expected: total is the class's first method, so its
		// comment (line 8) belongs to the class node and the range
		// starts at the def (9).
		"invoice.rb::(Invoice).total changed new 9-13 decl 9 old 7-10 decl 7",
		"invoice.rb::(Invoice).price changed new 15-19 decl 17 old 12-15 decl 13",
		"invoice.rb::(Invoice).tax changed new 21-21 decl 21 old 17-17 decl 17", // endless def
		"invoice.rb::(Invoice).self.build changed new 23-26 decl 23 old 19-21 decl 19",
		"invoice.rb::(Invoice).self.empty added new 29-31 decl 29", // inside class << self
		"invoice.rb::(Invoice).helper changed new 36-38 decl 36 old 25-27 decl 25",
	})
	catest.Edges(t, r, []string{
		"invoice.rb::(Invoice).total -> invoice.rb::(Invoice).price", // price(i), in a block
		"invoice.rb::(Invoice).total -> invoice.rb::(Invoice).tax",
		"invoice.rb::(Invoice).self.build -> invoice.rb::(Invoice).total", // inv.total(attrs)
		"invoice.rb::(Invoice).self.empty -> invoice.rb::(Invoice).self.build",
		// Known gap, expected: no total -> helper; `helper` has no
		// parentheses or receiver, so it reads as a local variable (as
		// does build's bare `new`).
	})
	// Modules, classes, constants and methods, nested; from leading
	// comments except a class's first member (the gap above).
	if got, want := r.DeclLines["invoice.rb"], []int{3, 4, 6, 9, 15, 21, 23, 28, 29, 36}; !reflect.DeepEqual(got, want) {
		t.Errorf("decl stops = %v, want %v", got, want)
	}
	if len(r.ExportedChanges) != 0 {
		t.Errorf("Contract Changes are Go-only, got %+v", r.ExportedChanges)
	}
}

// Edges never join Ruby to TypeScript, even by the same name.
func TestRubyEdgesStayInRuby(t *testing.T) {
	files := []codeanalysis.FilePair{
		catest.Pair("a.rb", "def run\n  1\nend\n", "def run\n  go_on()\nend\n"),
		catest.Pair("b.ts", "function go_on() {\n  return 1;\n}\n", "function go_on() {\n  return 2;\n}\n"),
		catest.Pair("c.rb", "def go_on\n  1\nend\n", "def go_on\n  2\nend\n"),
	}
	r := codeanalysis.Run(files, New())
	catest.Edges(t, r, []string{"a.rb::run -> c.rb::go_on"})
}

func TestReadsRubyByExtension(t *testing.T) {
	a := New()
	for p, want := range map[string]bool{"a.rb": true, "lib/tasks/db.rake": true, "Gemfile": false, "a.erb": false} {
		if a.Reads(p) != want {
			t.Errorf("Reads(%q) = %v, want %v", p, !want, want)
		}
	}
}

// A file with a syntax error keeps what tree-sitter recovered, with a note.
func TestSyntaxErrorKeepsWhatParsed(t *testing.T) {
	head := "def ok\n  2\nend\n\ndef broken(\n  3\n"
	r := codeanalysis.Run([]codeanalysis.FilePair{catest.Pair("x.rb", "def ok\n  1\nend\n", head)}, New())
	if len(r.ParseNotes) != 1 || r.ParseNotes[0].Path != "x.rb" {
		t.Fatalf("parse notes = %+v, want one for x.rb", r.ParseNotes)
	}
	found := false
	for _, m := range r.Methods {
		if m.Key == "x.rb::ok" {
			found = true
		}
	}
	if !found {
		t.Errorf("ok not found in a file with a later syntax error: %+v", r.Methods)
	}
}
