// Package golang puts the Go code analysis (internal/gocode, go/parser)
// behind the code-analysis interface, and adds Go's declaration stops.
package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
	"github.com/stephen-corgiat/pr-review/internal/gocode"
)

// Analyzer reads .go files. ModulePath (go.mod's module line) resolves
// package calls between the module's own packages.
type Analyzer struct {
	ModulePath string
}

func New(modulePath string) Analyzer { return Analyzer{ModulePath: modulePath} }

func (Analyzer) Reads(path string) bool { return strings.HasSuffix(path, ".go") }

// Analyze finds the Touched Methods, call edges between them, top-level
// declaration lines and exported signature changes in each Go file pair,
// using go/parser. It skips files that aren't .go. A side that fails to
// parse adds a ParseNote to the Result rather than an error.
func (a Analyzer) Analyze(files []codeanalysis.FilePair) *codeanalysis.Result {
	var src []gocode.FileSource
	out := &codeanalysis.Result{DeclLines: map[string][]int{}}
	for _, f := range files {
		src = append(src, gocode.FileSource{
			Path: f.Path, OldContent: f.Old, OldExists: f.OldExists,
			NewContent: f.New, NewExists: f.NewExists,
			ChangedOld: f.ChangedOld, ChangedNew: f.ChangedNew,
		})
		if f.NewExists {
			if ls := topLevelDeclLines(f.New); len(ls) > 0 {
				out.DeclLines[f.Path] = ls
			}
		}
	}
	an := gocode.Analyze(src, a.ModulePath)
	for _, m := range an.Methods {
		out.Methods = append(out.Methods, codeanalysis.Method{
			Key: m.Key, Path: m.Path, Name: m.Name, Owner: m.Receiver,
			Status:   status(m.Status),
			NewStart: m.NewStart, NewEnd: m.NewEnd, OldStart: m.OldStart, OldEnd: m.OldEnd,
			NewDeclLine: m.NewFuncLine, OldDeclLine: m.OldFuncLine,
		})
	}
	for _, e := range an.Edges {
		out.Edges = append(out.Edges, codeanalysis.Edge{Caller: e.Caller, Callee: e.Callee, Ambiguous: e.Ambiguous})
	}
	for _, c := range an.ExportedChanges {
		out.ExportedChanges = append(out.ExportedChanges, codeanalysis.ExportedChange{Kind: c.Kind, Name: c.Name, Path: c.Path, Detail: c.Detail})
	}
	for _, n := range an.ParseNotes {
		out.ParseNotes = append(out.ParseNotes, codeanalysis.ParseNote{Path: n.Path, Err: n.Err})
	}
	return out
}

func status(s gocode.Status) codeanalysis.Status {
	switch s {
	case gocode.Added:
		return codeanalysis.Added
	case gocode.Removed:
		return codeanalysis.Removed
	default:
		return codeanalysis.Changed
	}
}

// topLevelDeclLines is the first line (its doc comment's, if any) of every
// top-level declaration: func, method, type, const/var block. A file that
// doesn't parse gives none.
func topLevelDeclLines(src string) []int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		return nil
	}
	var out []int
	for _, d := range f.Decls {
		pos := d.Pos()
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				pos = d.Doc.Pos()
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			if d.Doc != nil {
				pos = d.Doc.Pos()
			}
		}
		out = append(out, fset.Position(pos).Line)
	}
	return out
}
