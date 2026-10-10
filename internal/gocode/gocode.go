// Package gocode finds Touched Methods, the call graph among them, and
// exported-API changes in changed .go files, via go/parser and go/ast only
// (no types, no agent).
package gocode

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

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

// Method is a Touched Method: a *ast.FuncDecl whose line range contains a
// changed line on the same side.
type Method struct {
	Key         string // <path>::<Name> or <path>::(*T).Name / (T).Name
	Path        string
	Name        string // e.g. "ProcessBounce"
	Receiver    string // "" for a function, "T" or "*T" for a method
	Status      Status
	NewStart    int // 1-based inclusive (from the Doc comment, if any); 0 if absent on the new side
	NewEnd      int
	OldStart    int
	OldEnd      int
	NewFuncLine int // the "func" keyword's own line (no Doc comment), new side
	OldFuncLine int
}

// DisplayName is Name, or "T.Name" for a method (no "*").
func (m Method) DisplayName() string {
	if m.Receiver == "" {
		return m.Name
	}
	return strings.TrimPrefix(m.Receiver, "*") + "." + m.Name
}

// HasReceiver reports whether the method has a receiver (vs. a bare function).
func (m Method) HasReceiver() bool { return m.Receiver != "" }

// ReceiverType is the receiver's bare type name, without "*".
func (m Method) ReceiverType() string { return strings.TrimPrefix(m.Receiver, "*") }

type Edge struct {
	Caller, Callee string // Method keys
	Ambiguous      bool   // rule 3, name-matched with more than one candidate
}

type ParseNote struct {
	Path string
	Err  string
}

type ExportedChange struct {
	Kind   string // "func_added" "func_removed" "func_sig_changed" "type_added" "type_removed" "field_added" "field_removed"
	Name   string // e.g. "webhooks.NewBeacon", "(*webhooks.Beacon).ProcessHandshake", "webhooks.Beacon", "bounce.Opt.BeaconEnabled"
	Path   string
	Detail string // old/new signature text for sig_changed
}

// FileSource is one changed .go file's old and new contents (either may be
// empty/absent, e.g. an added or deleted file).
type FileSource struct {
	Path       string
	OldContent string
	OldExists  bool
	NewContent string
	NewExists  bool
	ChangedOld map[int]bool // old-side line numbers that are removed
	ChangedNew map[int]bool // new-side line numbers that are added
}

type parsedFile struct {
	fset  *token.FileSet
	file  *ast.File
	funcs map[string]*ast.FuncDecl // key (without path) -> decl
	err   error
}

func parseSide(src string) *parsedFile {
	if src == "" {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		return &parsedFile{err: err}
	}
	pf := &parsedFile{fset: fset, file: f, funcs: map[string]*ast.FuncDecl{}}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		pf.funcs[funcKeyPart(fd)] = fd
	}
	return pf
}

func funcKeyPart(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := receiverString(fd.Recv.List[0].Type)
	return recv + "." + fd.Name.Name
}

func receiverString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "(*" + identName(t.X) + ")"
	default:
		return "(" + identName(expr) + ")"
	}
}

func identName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // generic type e.g. Foo[T]
		return identName(t.X)
	case *ast.IndexListExpr:
		return identName(t.X)
	default:
		return ""
	}
}

func declRange(fset *token.FileSet, fd *ast.FuncDecl) (start, end, funcLine int) {
	startPos := fd.Pos()
	if fd.Doc != nil {
		startPos = fd.Doc.Pos()
	}
	return fset.Position(startPos).Line, fset.Position(fd.End()).Line, fset.Position(fd.Pos()).Line
}

// Analysis is the result of analysing every changed .go file.
type Analysis struct {
	Methods         []Method
	Edges           []Edge
	ParseNotes      []ParseNote
	ExportedChanges []ExportedChange
}

// MethodByKey indexes Methods by Key for convenience.
func (a *Analysis) MethodByKey() map[string]Method {
	m := make(map[string]Method, len(a.Methods))
	for _, mm := range a.Methods {
		m[mm.Key] = mm
	}
	return m
}

// Callees returns the callee keys for a caller key, in edge order.
func (a *Analysis) Callees(caller string) []string {
	var out []string
	for _, e := range a.Edges {
		if e.Caller == caller {
			out = append(out, e.Callee)
		}
	}
	return out
}

// Callers returns the caller keys for a callee key (the reverse edges).
func (a *Analysis) Callers(callee string) []string {
	var out []string
	for _, e := range a.Edges {
		if e.Callee == callee {
			out = append(out, e.Caller)
		}
	}
	return out
}

// Analyze finds Touched Methods, the call graph, and exported-API changes.
func Analyze(files []FileSource, modulePath string) *Analysis {
	res := &Analysis{}

	type fileParsed struct {
		path     string
		old, new *parsedFile
		src      FileSource
	}
	var parsedFiles []fileParsed

	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		var old, nw *parsedFile
		if f.OldExists {
			old = parseSide(f.OldContent)
		}
		if f.NewExists {
			nw = parseSide(f.NewContent)
		}
		if old != nil && old.err != nil {
			res.ParseNotes = append(res.ParseNotes, ParseNote{Path: f.Path, Err: "old: " + old.err.Error()})
			old = nil
		}
		if nw != nil && nw.err != nil {
			res.ParseNotes = append(res.ParseNotes, ParseNote{Path: f.Path, Err: "new: " + nw.err.Error()})
			nw = nil
		}
		parsedFiles = append(parsedFiles, fileParsed{path: f.Path, old: old, new: nw, src: f})
	}

	// Touched Methods.
	for _, pf := range parsedFiles {
		oldKeys := map[string]bool{}
		if pf.old != nil {
			for k := range pf.old.funcs {
				oldKeys[k] = true
			}
		}
		newKeys := map[string]bool{}
		if pf.new != nil {
			for k := range pf.new.funcs {
				newKeys[k] = true
			}
		}
		allKeys := map[string]bool{}
		for k := range oldKeys {
			allKeys[k] = true
		}
		for k := range newKeys {
			allKeys[k] = true
		}
		var keys []string
		for k := range allKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var newStart, newEnd, oldStart, oldEnd, newFuncLine, oldFuncLine int
			touched := false
			if pf.new != nil {
				if fd, ok := pf.new.funcs[k]; ok {
					newStart, newEnd, newFuncLine = declRange(pf.new.fset, fd)
					for l := newStart; l <= newEnd; l++ {
						if pf.src.ChangedNew[l] {
							touched = true
							break
						}
					}
				}
			}
			if pf.old != nil {
				if fd, ok := pf.old.funcs[k]; ok {
					oldStart, oldEnd, oldFuncLine = declRange(pf.old.fset, fd)
					for l := oldStart; l <= oldEnd; l++ {
						if pf.src.ChangedOld[l] {
							touched = true
							break
						}
					}
				}
			}
			if !touched {
				continue
			}
			var status Status
			switch {
			case !oldKeys[k]:
				status = Added
			case !newKeys[k]:
				status = Removed
			default:
				status = Changed
			}
			name, recv := splitKeyPart(k)
			res.Methods = append(res.Methods, Method{
				Key:      pf.path + "::" + k,
				Path:     pf.path,
				Name:     name,
				Receiver: recv,
				Status:   status,
				NewStart: newStart, NewEnd: newEnd,
				OldStart: oldStart, OldEnd: oldEnd,
				NewFuncLine: newFuncLine, OldFuncLine: oldFuncLine,
			})
		}
	}
	sort.Slice(res.Methods, func(i, j int) bool { return res.Methods[i].Key < res.Methods[j].Key })

	// Index touched methods for the call graph.
	touchedByKey := res.MethodByKey()
	// touched functions (no receiver) grouped by directory+name
	funcsByDirName := map[string][]string{} // "dir\x00Name" -> []key
	// touched methods (with receiver) grouped by name
	methodsByName := map[string][]string{} // Name -> []key
	for _, m := range res.Methods {
		if !m.HasReceiver() {
			dir := path.Dir(m.Path)
			k := dir + "\x00" + m.Name
			funcsByDirName[k] = append(funcsByDirName[k], m.Key)
		} else {
			methodsByName[m.Name] = append(methodsByName[m.Name], m.Key)
		}
	}

	edgeSeen := map[[2]string]bool{}
	addEdge := func(caller, callee string, ambiguous bool) {
		key := [2]string{caller, callee}
		if edgeSeen[key] {
			return
		}
		edgeSeen[key] = true
		res.Edges = append(res.Edges, Edge{Caller: caller, Callee: callee, Ambiguous: ambiguous})
	}

	for _, pf := range parsedFiles {
		imports := map[string]string{} // local name -> import path
		var astFile *ast.File
		if pf.new != nil {
			astFile = pf.new.file
		} else if pf.old != nil {
			astFile = pf.old.file
		}
		if astFile != nil {
			for _, imp := range astFile.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				name := path.Base(p)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				imports[name] = p
			}
		}
		dir := path.Dir(pf.path)

		// Walk each touched method's body, in the same source as its status
		// (new side, or old side if removed).
		for _, m := range res.Methods {
			if m.Path != pf.path {
				continue
			}
			var body *ast.FuncDecl
			var fset *token.FileSet
			key := funcKeyOf(m)
			if m.Status == Removed {
				if pf.old != nil {
					body, fset = pf.old.funcs[key], pf.old.fset
				}
			} else {
				if pf.new != nil {
					body, fset = pf.new.funcs[key], pf.new.fset
				}
			}
			if body == nil || fset == nil {
				continue
			}
			callerRecvIdent := ""
			if body.Recv != nil && len(body.Recv.List) > 0 && len(body.Recv.List[0].Names) > 0 {
				callerRecvIdent = body.Recv.List[0].Names[0].Name
			}
			callerRecvType := m.ReceiverType()

			ast.Inspect(body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					// Rule 1: bare call f(...).
					k := dir + "\x00" + fn.Name
					for _, calleeKey := range funcsByDirName[k] {
						addEdge(m.Key, calleeKey, false)
					}
				case *ast.SelectorExpr:
					if xIdent, ok := fn.X.(*ast.Ident); ok {
						if impPath, isImport := imports[xIdent.Name]; isImport {
							// Rule 2: package call x.F(...).
							if strings.HasPrefix(impPath, modulePath+"/") {
								calleeDir := strings.TrimPrefix(impPath, modulePath+"/")
								k := calleeDir + "\x00" + fn.Sel.Name
								for _, calleeKey := range funcsByDirName[k] {
									addEdge(m.Key, calleeKey, false)
								}
							}
							return true
						}
					}
					// Rule 3: anything else, expr.M(...).
					candidates := methodsByName[fn.Sel.Name]
					var kept []string
					for _, calleeKey := range candidates {
						if calleeKey == m.Key {
							continue // no self edges
						}
						calleeM := touchedByKey[calleeKey]
						if calleeM.ReceiverType() == callerRecvType {
							// Same type as caller's own receiver: keep only
							// when expr is exactly the caller's receiver ident.
							if xIdent, ok := fn.X.(*ast.Ident); ok && callerRecvIdent != "" && xIdent.Name == callerRecvIdent {
								kept = append(kept, calleeKey)
							}
							continue
						}
						kept = append(kept, calleeKey)
					}
					ambiguous := len(kept) > 1
					for _, calleeKey := range kept {
						addEdge(m.Key, calleeKey, ambiguous)
					}
				}
				return true
			})
		}
	}

	// Exported-API changes.
	for _, pf := range parsedFiles {
		if pf.old == nil && pf.new == nil {
			continue
		}
		var pkgFile *ast.File
		if pf.new != nil {
			pkgFile = pf.new.file
		} else {
			pkgFile = pf.old.file
		}
		if pkgFile != nil && pkgFile.Name != nil && pkgFile.Name.Name == "main" {
			continue
		}
		res.ExportedChanges = append(res.ExportedChanges, exportedFuncChanges(pf.path, pf.old, pf.new)...)
		res.ExportedChanges = append(res.ExportedChanges, exportedTypeChanges(pf.path, pf.old, pf.new)...)
	}

	return res
}

func funcKeyOf(m Method) string {
	if m.Receiver == "" {
		return m.Name
	}
	return "(" + m.Receiver + ")." + m.Name
}

func splitKeyPart(k string) (name, receiver string) {
	// k is either "Name" or "(T).Name" / "(*T).Name"
	if !strings.HasPrefix(k, "(") {
		return k, ""
	}
	close := strings.Index(k, ").")
	if close < 0 {
		return k, ""
	}
	recv := k[1:close]
	name = k[close+2:]
	return name, recv
}

func isExported(name string) bool {
	return name != "" && strings.ToUpper(name[:1]) == name[:1] && strings.ToLower(name[:1]) != name[:1]
}

func printNode(fset *token.FileSet, n ast.Node) string {
	var sb strings.Builder
	printer.Fprint(&sb, fset, n)
	return normalizeWS(sb.String())
}

func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func exportedFuncChanges(pathStr string, old, nw *parsedFile) []ExportedChange {
	type sig struct {
		key  string
		recv string
		name string
		text string
	}
	collect := func(pf *parsedFile) map[string]sig {
		out := map[string]sig{}
		if pf == nil {
			return out
		}
		for k, fd := range pf.funcs {
			name, recv := splitKeyPart(k)
			if !isExported(name) {
				continue
			}
			if recv != "" {
				rt := strings.TrimPrefix(recv, "*")
				if !isExported(rt) {
					continue
				}
			}
			var sb strings.Builder
			if fd.Recv != nil {
				printer.Fprint(&sb, pf.fset, fd.Recv)
				sb.WriteString(" ")
			}
			printer.Fprint(&sb, pf.fset, fd.Type)
			out[k] = sig{key: k, recv: recv, name: name, text: normalizeWS(sb.String())}
		}
		return out
	}
	oldSigs := collect(old)
	newSigs := collect(nw)

	dispName := func(recv, name string) string {
		if recv == "" {
			return pkgQualified(pathStr, name)
		}
		return "(" + pkgQualifiedRecv(pathStr, recv) + ")." + name
	}

	var out []ExportedChange
	var keys []string
	seen := map[string]bool{}
	for k := range oldSigs {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range newSigs {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		o, hasOld := oldSigs[k]
		n, hasNew := newSigs[k]
		switch {
		case hasOld && !hasNew:
			out = append(out, ExportedChange{Kind: "func_removed", Name: dispName(o.recv, o.name), Path: pathStr})
		case !hasOld && hasNew:
			out = append(out, ExportedChange{Kind: "func_added", Name: dispName(n.recv, n.name), Path: pathStr})
		case hasOld && hasNew && o.text != n.text:
			out = append(out, ExportedChange{Kind: "func_sig_changed", Name: dispName(n.recv, n.name), Path: pathStr,
				Detail: "was " + o.text + "; now " + n.text})
		}
	}
	return out
}

func pkgQualified(pathStr, name string) string {
	pkg := path.Base(path.Dir(pathStr))
	if pkg == "." || pkg == "" {
		return name
	}
	return pkg + "." + name
}

func pkgQualifiedRecv(pathStr, recv string) string {
	pkg := path.Base(path.Dir(pathStr))
	if pkg == "." || pkg == "" {
		return recv
	}
	if strings.HasPrefix(recv, "*") {
		return "*" + pkg + "." + strings.TrimPrefix(recv, "*")
	}
	return pkg + "." + recv
}

type fieldInfo struct {
	name string
	text string
}

func structFields(pf *parsedFile, st *ast.StructType) []fieldInfo {
	var out []fieldInfo
	for _, f := range st.Fields.List {
		var typeText strings.Builder
		printer.Fprint(&typeText, pf.fset, f.Type)
		tt := normalizeWS(typeText.String())
		if len(f.Names) == 0 {
			// Embedded field; name is the type's identifier.
			out = append(out, fieldInfo{name: identName(f.Type), text: tt})
			continue
		}
		for _, n := range f.Names {
			if !isExported(n.Name) {
				continue
			}
			out = append(out, fieldInfo{name: n.Name, text: tt})
		}
	}
	return out
}

func exportedTypeChanges(pathStr string, old, nw *parsedFile) []ExportedChange {
	collect := func(pf *parsedFile) map[string]*ast.StructType {
		out := map[string]*ast.StructType{}
		if pf == nil {
			return out
		}
		for _, decl := range pf.file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !isExported(ts.Name.Name) {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					out[ts.Name.Name] = st
				}
			}
		}
		return out
	}
	oldTypes := collect(old)
	newTypes := collect(nw)

	var out []ExportedChange
	var names []string
	seen := map[string]bool{}
	for n := range oldTypes {
		names = append(names, n)
		seen[n] = true
	}
	for n := range newTypes {
		if !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		ot, hasOld := oldTypes[name]
		nt, hasNew := newTypes[name]
		qname := pkgQualified(pathStr, name)
		switch {
		case hasOld && !hasNew:
			out = append(out, ExportedChange{Kind: "type_removed", Name: qname, Path: pathStr})
		case !hasOld && hasNew:
			out = append(out, ExportedChange{Kind: "type_added", Name: qname, Path: pathStr})
		case hasOld && hasNew:
			of := fieldMap(structFields(old, ot))
			nf := fieldMap(structFields(nw, nt))
			var fnames []string
			fseen := map[string]bool{}
			for n := range of {
				fnames = append(fnames, n)
				fseen[n] = true
			}
			for n := range nf {
				if !fseen[n] {
					fnames = append(fnames, n)
				}
			}
			sort.Strings(fnames)
			for _, fn := range fnames {
				_, hasOldF := of[fn]
				_, hasNewF := nf[fn]
				if hasOldF && !hasNewF {
					out = append(out, ExportedChange{Kind: "field_removed", Name: qname + "." + fn, Path: pathStr})
				} else if !hasOldF && hasNewF {
					out = append(out, ExportedChange{Kind: "field_added", Name: qname + "." + fn, Path: pathStr})
				}
			}
		}
	}
	return out
}

func fieldMap(fs []fieldInfo) map[string]string {
	m := map[string]string{}
	for _, f := range fs {
		m[f.name] = f.text
	}
	return m
}
