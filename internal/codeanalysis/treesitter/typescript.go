package treesitter

import (
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
)

// TypeScript and TSX: the TypeScript tags.scm with the JavaScript one
// appended (as tree-sitter-typescript's tree-sitter.json says), plus
// extra.scm for constructors and class fields holding a function.
//
// Known gaps (written down as expected results in the tests):
//   - components wrapped in a call (React.memo(() => …), forwardRef),
//     anonymous default exports and overload signatures aren't Touched
//     Methods: the first two aren't tagged as definitions, and signatures
//     have no body (the implementation is found, at its own lines)
//   - JSX elements (<Foo />) aren't calls
//   - decorators (@logged()) are calls, made by the method under them
func init() {
	queries := []string{"typescript/tags.scm", "javascript/tags.scm", "typescript/extra.scm"}
	languages = append(languages,
		tsLanguage("typescript", []string{".ts", ".mts", ".cts"}, typescript.Language, queries),
		tsLanguage("tsx", []string{".tsx"}, tsx.Language, queries),
	)
}

func tsLanguage(name string, exts []string, grammar func() *sitter.Language, queries []string) *language {
	return &language{
		name: name, family: "typescript", exts: exts, grammar: grammar, queries: queries,
		skipDef: set("function_signature", "method_signature", "abstract_method_signature"),
		funcs: set("function_declaration", "function_expression", "function", "arrow_function",
			"method_definition", "generator_function_declaration", "generator_function"),
		leading:   set("comment", "decorator"),
		widen:     tsWiden,
		owner:     tsOwner,
		call:      tsCall,
		declStops: tsDeclStops,
	}
}

// tsWiden widens a const arrow function to its lexical_declaration (when
// it's the only declarator), an assignment to its statement, and anything
// exported to its export_statement.
func tsWiden(n *sitter.Node, l *sitter.Language) *sitter.Node {
	switch n.Type(l) {
	case "variable_declarator":
		if p := n.Parent(); p != nil && (p.Type(l) == "lexical_declaration" || p.Type(l) == "variable_declaration") && countType(p, l, "variable_declarator") == 1 {
			n = p
		}
	case "assignment_expression":
		if p := n.Parent(); p != nil && p.Type(l) == "expression_statement" {
			n = p
		}
	}
	if p := n.Parent(); p != nil && p.Type(l) == "export_statement" {
		n = p
	}
	return n
}

// tsOwner is the class a method is in, or the variable an object literal
// with the function is assigned to (const api = { get: () => … }).
func tsOwner(n *sitter.Node, l *sitter.Language, src []byte) (string, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Type(l) {
		case "class_declaration", "abstract_class_declaration", "class":
			if name := p.ChildByFieldName("name", l); name != nil {
				return name.Text(src), false
			}
			return declaratorName(p, l, src), false
		case "object":
			if n.Type(l) == "pair" && n.Parent() == p {
				return declaratorName(p, l, src), false
			}
			return "", false
		}
	}
	return "", false
}

// declaratorName is the variable n is the value of (const X = n), or "".
func declaratorName(n *sitter.Node, l *sitter.Language, src []byte) string {
	if p := n.Parent(); p != nil && p.Type(l) == "variable_declarator" {
		if name := p.ChildByFieldName("name", l); name != nil {
			return name.Text(src)
		}
	}
	return ""
}

// tsCall reads f(), this.f() and x.f() from the callee's name node.
func tsCall(name *sitter.Node, l *sitter.Language, src []byte) (callKind, bool) {
	p := name.Parent()
	if p == nil {
		return 0, false
	}
	switch p.Type(l) {
	case "call_expression":
		return callBare, true
	case "member_expression":
		if obj := p.ChildByFieldName("object", l); obj != nil && obj.Type(l) == "this" {
			return callSelf, true
		}
		return callOther, true
	}
	return 0, false
}

var tsTopDecls = set("function_declaration", "generator_function_declaration", "function_signature",
	"class_declaration", "abstract_class_declaration", "lexical_declaration", "variable_declaration",
	"interface_declaration", "type_alias_declaration", "enum_declaration", "internal_module", "module",
	"ambient_declaration", "export_statement")

// tsDeclStops are the top-level declarations and the members of top-level
// classes (as Go's methods are top-level).
func tsDeclStops(root *sitter.Node, l *sitter.Language) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < root.NamedChildCount(); i++ {
		c := root.NamedChild(i)
		if !tsTopDecls[c.Type(l)] {
			continue
		}
		out = append(out, c)
		cls := c
		if c.Type(l) == "export_statement" {
			cls = c.ChildByFieldName("declaration", l)
		}
		if cls == nil || (cls.Type(l) != "class_declaration" && cls.Type(l) != "abstract_class_declaration") {
			continue
		}
		body := cls.ChildByFieldName("body", l)
		if body == nil {
			continue
		}
		for j := 0; j < body.NamedChildCount(); j++ {
			if m := body.NamedChild(j); m.Type(l) != "comment" && m.Type(l) != "decorator" {
				out = append(out, m)
			}
		}
	}
	return out
}

func countType(n *sitter.Node, l *sitter.Language, typ string) int {
	k := 0
	for i := 0; i < n.NamedChildCount(); i++ {
		if n.NamedChild(i).Type(l) == typ {
			k++
		}
	}
	return k
}

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
