// Package codeanalysis is the code-analysis interface. Given each changed
// file at the base and the head, an Analyzer finds:
//
//   - the Touched Methods: functions and methods whose range (leading
//     comments included) holds a changed line, with the type, class or
//     module that owns them
//   - the call edges between Touched Methods, matched by name
//   - the declaration stops for ]/[ (new side)
//   - exported-API changes (Go only: the Contract Changes)
//
// Each language lives behind an Analyzer in a sub-package: golang
// (go/parser) and treesitter (TypeScript, TSX and Ruby). Run hands every
// file to the first Analyzer that reads it. Files none of them read get no
// analysis, only the plain diff.
package codeanalysis

import "sort"

// Status is what the Pull Request did to a Touched Method.
type Status int

const (
	Added Status = iota
	Removed
	Changed
)

func (s Status) String() string {
	switch s {
	case Added:
		return "added"
	case Removed:
		return "removed"
	default:
		return "changed"
	}
}

// FilePair is one changed file at the base (Old) and the head (New).
// Either side may be missing (an added or deleted file).
type FilePair struct {
	Path       string
	Old, New   string
	OldExists  bool
	NewExists  bool
	ChangedOld map[int]bool // base-side line numbers the diff removes
	ChangedNew map[int]bool // head-side line numbers the diff adds
}

// Method is a Touched Method.
type Method struct {
	// Key is unique within a Result: "<path>::<Name>" for a function,
	// "<path>::(<Owner>).<Name>" for a method. Languages may add to the
	// part after "::" to keep it unique (Ruby's "self." for a class
	// method).
	Key  string
	Path string
	Name string
	// Owner is the type, class or module the method belongs to: Go's
	// receiver ("T" or "*T"), a TypeScript class, a Ruby class or module.
	// "" for a plain function.
	Owner  string
	Status Status
	// Ranges are 1-based and inclusive, leading comments included; 0 when
	// the method doesn't exist on that side.
	NewStart, NewEnd int
	OldStart, OldEnd int
	// DeclLine is the declaration's own first line, without its leading
	// comments (Go's "func" line); 0 when absent on that side.
	NewDeclLine, OldDeclLine int
}

// DisplayName is Name, or "Owner.Name" for a method (Go's "*" dropped).
func (m Method) DisplayName() string {
	if m.Owner == "" {
		return m.Name
	}
	o := m.Owner
	if o[0] == '*' {
		o = o[1:]
	}
	return o + "." + m.Name
}

// Edge is a call from one Touched Method to another, by Method Key.
type Edge struct {
	Caller, Callee string
	// Ambiguous: matched by name with more than one candidate, so every
	// candidate got an edge.
	Ambiguous bool
}

// ExportedChange is a change to a package's exported API (Go only).
type ExportedChange struct {
	Kind   string // "func_added" "func_removed" "func_sig_changed" "type_added" "type_removed" "field_added" "field_removed"
	Name   string // e.g. "webhooks.NewBeacon", "(*webhooks.Beacon).ProcessBounce", "bounce.Opt.BeaconEnabled"
	Path   string
	Detail string // old and new signature text for func_sig_changed
}

// ParseNote says a file side didn't parse cleanly.
type ParseNote struct {
	Path string
	Err  string
}

// Result is everything found in a set of changed files.
type Result struct {
	Methods []Method // sorted by Key
	Edges   []Edge
	// DeclLines is, per path, the head-side first line (leading comments
	// included) of each declaration ]/[ stops on, ascending.
	DeclLines       map[string][]int
	ExportedChanges []ExportedChange
	ParseNotes      []ParseNote
}

// Analyzer analyses the changed files of the languages it reads.
type Analyzer interface {
	// Reads reports whether the Analyzer reads this path (by extension).
	Reads(path string) bool
	// Analyze is given only paths it Reads. Edges only join Methods from
	// the same call to Analyze.
	Analyze(files []FilePair) *Result
}

// Reads reports whether any of the analyzers reads path.
func Reads(path string, analyzers ...Analyzer) bool {
	return pick(path, analyzers) >= 0
}

func pick(path string, analyzers []Analyzer) int {
	for i, a := range analyzers {
		if a.Reads(path) {
			return i
		}
	}
	return -1
}

// Run gives each file to the first analyzer that reads it and merges the
// results. Files no analyzer reads are skipped.
func Run(files []FilePair, analyzers ...Analyzer) *Result {
	groups := make([][]FilePair, len(analyzers))
	for _, f := range files {
		if i := pick(f.Path, analyzers); i >= 0 {
			groups[i] = append(groups[i], f)
		}
	}
	out := &Result{DeclLines: map[string][]int{}}
	for i, a := range analyzers {
		if len(groups[i]) == 0 {
			continue
		}
		r := a.Analyze(groups[i])
		out.Methods = append(out.Methods, r.Methods...)
		out.Edges = append(out.Edges, r.Edges...)
		out.ExportedChanges = append(out.ExportedChanges, r.ExportedChanges...)
		out.ParseNotes = append(out.ParseNotes, r.ParseNotes...)
		for p, ls := range r.DeclLines {
			out.DeclLines[p] = ls
		}
	}
	sort.Slice(out.Methods, func(i, j int) bool { return out.Methods[i].Key < out.Methods[j].Key })
	return out
}
