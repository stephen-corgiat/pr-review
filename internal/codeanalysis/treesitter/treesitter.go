// Package treesitter is the code analysis for TypeScript, TSX and Ruby,
// on tree-sitter through the pure-Go gotreesitter binding. It is the only
// package that imports gotreesitter, so switching binding (to
// smacker/go-tree-sitter, say) touches only this package.
//
// Each grammar's own tags.scm (queries/) finds definitions and calls. The
// code then walks the tree for what queries can't say: leading comments,
// the owning class or module, and the full range of a definition (the
// export statement or const declaration around it).
//
// The grammars come from gotreesitter's standalone grammars/<name>
// packages, so the binary embeds only these three grammars with a plain
// go build: no build tags.
package treesitter

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	sitter "github.com/odvcencio/gotreesitter"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
)

// parseTimeoutMicros bounds one file's parse. A parse that stops early
// keeps what it found and gets a ParseNote.
const parseTimeoutMicros = 5_000_000

// callKind is how a call names its callee.
type callKind int

const (
	callBare  callKind = iota // f(): no receiver
	callSelf                  // this.f(), self.f: the caller's own object
	callOther                 // x.f(): another receiver
)

// language is one grammar and the rules that read Touched Methods from
// its trees.
type language struct {
	name    string
	family  string // edges only join methods of one family (TypeScript and TSX are one)
	exts    []string
	grammar func() *sitter.Language
	queries []string // tags query files under queries/, in order

	// skipDef: definition node types that aren't Touched Methods (e.g.
	// bodiless signatures).
	skipDef map[string]bool
	// funcs: node types with a body. A definition inside one is part of
	// the outer one, not a Touched Method of its own (as Go's closures).
	funcs map[string]bool
	// leading: sibling node types that belong to the definition after them
	// (comments, decorators).
	leading map[string]bool
	// widen is the node a definition's range starts from (e.g. the export
	// statement around it); nil keeps the node.
	widen func(n *sitter.Node, l *sitter.Language) *sitter.Node
	// owner is the class or module that owns a definition, and whether
	// it's a class (singleton) method.
	owner func(n *sitter.Node, l *sitter.Language, src []byte) (name string, singleton bool)
	// call reads a call from its callee's name node; ok false skips it.
	call func(name *sitter.Node, l *sitter.Language, src []byte) (kind callKind, ok bool)
	// bareIsSelf: a bare call may be a method of the caller's own class
	// (Ruby's implicit self).
	bareIsSelf bool
	// declStops are the nodes ]/[ stops on.
	declStops func(root *sitter.Node, l *sitter.Language) []*sitter.Node

	once  sync.Once
	lang  *sitter.Language
	query *sitter.Query
	err   error
}

// languages are registered by the per-language files.
var languages []*language

func forPath(p string) *language {
	ext := strings.ToLower(path.Ext(p))
	for _, lg := range languages {
		for _, e := range lg.exts {
			if e == ext {
				return lg
			}
		}
	}
	return nil
}

func (lg *language) load() error {
	lg.once.Do(func() {
		lg.lang = lg.grammar()
		src, err := tagsQuery(lg.queries...)
		if err != nil {
			lg.err = err
			return
		}
		lg.query, lg.err = sitter.NewQuery(src, lg.lang)
		if lg.err != nil {
			lg.err = fmt.Errorf("%s tags query: %w", lg.name, lg.err)
		}
	})
	return lg.err
}

// Analyzer reads TypeScript (.ts, .mts, .cts), TSX (.tsx) and Ruby (.rb,
// .rake) files.
type Analyzer struct{}

func New() Analyzer { return Analyzer{} }

func (Analyzer) Reads(p string) bool { return forPath(p) != nil }

// def is one definition found in one side of a file.
type def struct {
	part      string // the Method Key after "<path>::"
	name      string
	owner     string
	start     int // 1-based, leading comments included
	end       int
	decl      int    // the definition's own first line
	from, to  uint32 // byte range, leading comments included
	calls     []call
	singleton bool
}

type call struct {
	name string
	kind callKind
}

// side is one side (base or head) of a file, parsed.
type side struct {
	defs  map[string]*def // by part
	decls []int
	note  string // the parse wasn't clean
}

func (lg *language) parse(src string, withDecls bool) *side {
	s := &side{defs: map[string]*def{}}
	b := []byte(src)
	p := sitter.NewParser(lg.lang)
	p.SetTimeoutMicros(parseTimeoutMicros)
	tree, err := p.Parse(b)
	if err != nil || tree == nil {
		s.note = fmt.Sprintf("tree-sitter couldn't parse it: %v", err)
		return s
	}
	defer tree.Release()
	root := tree.RootNode()
	if r := tree.ParseStopReason(); r != sitter.ParseStopNone && r != sitter.ParseStopAccepted {
		s.note = fmt.Sprintf("tree-sitter stopped early (%s)", r)
	} else if root.HasError() {
		s.note = fmt.Sprintf("syntax error near line %d (tree-sitter read past it)", firstErrorLine(root))
	}

	l := lg.lang
	var defs []*def
	seen := map[[2]uint32]bool{}
	var refs []*sitter.Node // callee name nodes
	seenRef := map[uint32]bool{}
	cur := lg.query.Exec(root, l, b)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		var defNode, nameNode, refNode *sitter.Node
		for _, c := range m.Captures {
			switch c.Name {
			case "definition.function", "definition.method":
				defNode = c.Node
			case "reference.call":
				refNode = c.Node
			case "name":
				nameNode = c.Node
			}
		}
		if nameNode == nil {
			continue
		}
		if refNode != nil {
			// Two patterns can capture one call (Ruby's).
			if !seenRef[nameNode.StartByte()] {
				seenRef[nameNode.StartByte()] = true
				refs = append(refs, nameNode)
			}
			continue
		}
		if defNode == nil || lg.skipDef[defNode.Type(l)] || lg.nested(defNode) {
			continue
		}
		key := [2]uint32{defNode.StartByte(), defNode.EndByte()}
		if seen[key] {
			continue
		}
		seen[key] = true
		whole := defNode
		if lg.widen != nil {
			whole = lg.widen(defNode, l)
		}
		first := lg.leadingStart(whole)
		d := &def{
			name:  nameNode.Text(b),
			start: int(first.StartPoint().Row) + 1,
			end:   endLine(whole),
			decl:  int(whole.StartPoint().Row) + 1,
			from:  first.StartByte(),
			to:    whole.EndByte(),
		}
		if lg.owner != nil {
			d.owner, d.singleton = lg.owner(defNode, l, b)
		}
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].from < defs[j].from })
	for _, d := range defs {
		part := d.name
		switch {
		case d.owner != "" && d.singleton:
			part = "(" + d.owner + ").self." + d.name
		case d.owner != "":
			part = "(" + d.owner + ")." + d.name
		case d.singleton:
			part = "self." + d.name
		}
		// A name defined twice (a reopened class, say) keeps both.
		if s.defs[part] != nil {
			for n := 2; ; n++ {
				if p := fmt.Sprintf("%s~%d", part, n); s.defs[p] == nil {
					part = p
					break
				}
			}
		}
		d.part = part
		s.defs[part] = d
	}

	// Each call belongs to the innermost definition around it.
	for _, r := range refs {
		kind, ok := lg.call(r, l, b)
		if !ok {
			continue
		}
		at := r.StartByte()
		var in *def
		for _, d := range defs {
			if d.from <= at && at < d.to && (in == nil || d.to-d.from < in.to-in.from) {
				in = d
			}
		}
		if in != nil {
			in.calls = append(in.calls, call{name: r.Text(b), kind: kind})
		}
	}

	if withDecls && lg.declStops != nil {
		seenLine := map[int]bool{}
		for _, n := range lg.declStops(root, l) {
			ln := int(lg.leadingStart(n).StartPoint().Row) + 1
			if !seenLine[ln] {
				seenLine[ln] = true
				s.decls = append(s.decls, ln)
			}
		}
		sort.Ints(s.decls)
	}
	return s
}

// nested reports whether n sits inside another function's body.
func (lg *language) nested(n *sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if lg.funcs[p.Type(lg.lang)] {
			return true
		}
	}
	return false
}

// leadingStart is the first of the comments (and decorators) directly
// above n: each on its own line, with no blank line before n.
func (lg *language) leadingStart(n *sitter.Node) *sitter.Node {
	first := n
	for p := n.PrevSibling(); p != nil; p = p.PrevSibling() {
		if !lg.leading[p.Type(lg.lang)] {
			break
		}
		if p.EndPoint().Row+1 < first.StartPoint().Row {
			break // a blank line between
		}
		if pp := p.PrevSibling(); pp != nil && pp.EndPoint().Row == p.StartPoint().Row {
			break // it trails the code before it
		}
		first = p
	}
	return first
}

// endLine is n's last line, 1-based. A node that ends at column 0 ended
// with the line before (its trailing newline).
func endLine(n *sitter.Node) int {
	e := n.EndPoint()
	if e.Column == 0 && e.Row > n.StartPoint().Row {
		return int(e.Row)
	}
	return int(e.Row) + 1
}

func firstErrorLine(root *sitter.Node) int {
	var walk func(n *sitter.Node) int
	walk = func(n *sitter.Node) int {
		if n.IsError() || n.IsMissing() {
			return int(n.StartPoint().Row) + 1
		}
		for i := 0; i < n.ChildCount(); i++ {
			if c := n.Child(i); c != nil && (c.HasError() || c.IsError() || c.IsMissing()) {
				if ln := walk(c); ln > 0 {
					return ln
				}
			}
		}
		return 0
	}
	if ln := walk(root); ln > 0 {
		return ln
	}
	return int(root.StartPoint().Row) + 1
}

// touched is a Touched Method with what the edges need.
type touched struct {
	m      codeanalysis.Method
	family string
	calls  []call
}

func (Analyzer) Analyze(files []codeanalysis.FilePair) *codeanalysis.Result {
	out := &codeanalysis.Result{DeclLines: map[string][]int{}}
	var all []*touched
	for _, f := range files {
		lg := forPath(f.Path)
		if lg == nil {
			continue
		}
		if err := lg.load(); err != nil {
			out.ParseNotes = append(out.ParseNotes, codeanalysis.ParseNote{Path: f.Path, Err: err.Error()})
			continue
		}
		var old, nw *side
		if f.OldExists {
			old = lg.parse(f.Old, false)
			if old.note != "" {
				out.ParseNotes = append(out.ParseNotes, codeanalysis.ParseNote{Path: f.Path, Err: "old: " + old.note})
			}
		}
		if f.NewExists {
			nw = lg.parse(f.New, true)
			if nw.note != "" {
				out.ParseNotes = append(out.ParseNotes, codeanalysis.ParseNote{Path: f.Path, Err: "new: " + nw.note})
			}
			if len(nw.decls) > 0 {
				out.DeclLines[f.Path] = nw.decls
			}
		}
		all = append(all, touchedIn(f, lg, old, nw)...)
	}
	for _, t := range all {
		out.Methods = append(out.Methods, t.m)
	}
	sort.Slice(out.Methods, func(i, j int) bool { return out.Methods[i].Key < out.Methods[j].Key })
	out.Edges = edges(all)
	return out
}

// touchedIn finds the definitions on either side whose range holds a
// changed line on that side.
func touchedIn(f codeanalysis.FilePair, lg *language, old, nw *side) []*touched {
	parts := map[string]bool{}
	for _, s := range []*side{old, nw} {
		if s == nil {
			continue
		}
		for p := range s.defs {
			parts[p] = true
		}
	}
	var keys []string
	for p := range parts {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	var out []*touched
	for _, p := range keys {
		var od, nd *def
		if old != nil {
			od = old.defs[p]
		}
		if nw != nil {
			nd = nw.defs[p]
		}
		if !hits(nd, f.ChangedNew) && !hits(od, f.ChangedOld) {
			continue
		}
		t := &touched{family: lg.family}
		m := codeanalysis.Method{Key: f.Path + "::" + p, Path: f.Path}
		switch {
		case od == nil:
			m.Status = codeanalysis.Added
		case nd == nil:
			m.Status = codeanalysis.Removed
		default:
			m.Status = codeanalysis.Changed
		}
		if nd != nil {
			m.Name, m.Owner = nd.name, nd.owner
			m.NewStart, m.NewEnd, m.NewDeclLine = nd.start, nd.end, nd.decl
			t.calls = nd.calls
		}
		if od != nil {
			m.Name, m.Owner = od.name, od.owner
			m.OldStart, m.OldEnd, m.OldDeclLine = od.start, od.end, od.decl
			if nd == nil {
				t.calls = od.calls
			}
		}
		t.m = m
		out = append(out, t)
	}
	return out
}

func hits(d *def, changed map[int]bool) bool {
	if d == nil {
		return false
	}
	for l := d.start; l <= d.end; l++ {
		if changed[l] {
			return true
		}
	}
	return false
}

// edges matches each Touched Method's calls to the Touched Methods of its
// family by name:
//   - a bare call f(): the functions (no owner) named f, those in the
//     caller's file if there are any; in Ruby, as a self call
//   - a self call (this.f(), self.f): the methods named f of the caller's
//     own class in its file; failing that, every method named f
//   - any other call x.f(): every method or function named f
//
// Never the caller itself. More than one candidate makes every edge
// Ambiguous.
func edges(all []*touched) []codeanalysis.Edge {
	byName := map[string][]*touched{}
	for _, t := range all {
		k := t.family + "\x00" + t.m.Name
		byName[k] = append(byName[k], t)
	}
	var out []codeanalysis.Edge
	seen := map[[2]string]bool{}
	langOf := func(t *touched) *language { return forPath(t.m.Path) }
	for _, caller := range all {
		lg := langOf(caller)
		for _, c := range caller.calls {
			var cands []*touched
			for _, t := range byName[caller.family+"\x00"+c.name] {
				if t != caller {
					cands = append(cands, t)
				}
			}
			kind := c.kind
			if kind == callBare && lg != nil && lg.bareIsSelf {
				kind = callSelf
			}
			var kept []*touched
			switch kind {
			case callBare:
				for _, t := range cands {
					if t.m.Owner == "" {
						kept = append(kept, t)
					}
				}
				kept = preferFile(kept, caller.m.Path)
			case callSelf:
				for _, t := range cands {
					if t.m.Owner == caller.m.Owner && t.m.Path == caller.m.Path {
						kept = append(kept, t)
					}
				}
				if len(kept) == 0 {
					for _, t := range cands {
						if t.m.Owner != "" || (lg != nil && lg.bareIsSelf) {
							kept = append(kept, t)
						}
					}
				}
			default:
				kept = cands
			}
			for _, t := range kept {
				k := [2]string{caller.m.Key, t.m.Key}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, codeanalysis.Edge{Caller: caller.m.Key, Callee: t.m.Key, Ambiguous: len(kept) > 1})
			}
		}
	}
	return out
}

func preferFile(ts []*touched, p string) []*touched {
	var same []*touched
	for _, t := range ts {
		if t.m.Path == p {
			same = append(same, t)
		}
	}
	if len(same) > 0 {
		return same
	}
	return ts
}
